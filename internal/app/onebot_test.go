package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/onebot"
	"ai-upstream-monitor/internal/store"
)

type fakeOneBotClient struct {
	loginErr error
	sent     []fakeOneBotMessage
	images   []fakeOneBotImage
}

type fakeOneBotMessage struct {
	baseURL string
	token   string
	groupID string
	text    string
}

type fakeOneBotImage struct {
	baseURL string
	token   string
	groupID string
	png     []byte
}

func (f *fakeOneBotClient) GetLoginInfo(_ context.Context, _, _ string) (onebot.LoginInfo, error) {
	return onebot.LoginInfo{Nickname: "bot"}, f.loginErr
}

func (f *fakeOneBotClient) SendGroupMessage(_ context.Context, baseURL, token, groupID, text string) error {
	f.sent = append(f.sent, fakeOneBotMessage{baseURL: baseURL, token: token, groupID: groupID, text: text})
	return nil
}

func (f *fakeOneBotClient) SendGroupImage(_ context.Context, baseURL, token, groupID string, png []byte) error {
	f.images = append(f.images, fakeOneBotImage{baseURL: baseURL, token: token, groupID: groupID, png: append([]byte(nil), png...)})
	return nil
}

type fakeOneBotStatusImageRenderer struct {
	images [][]byte
	err    error
}

func (f fakeOneBotStatusImageRenderer) Render(domain.PublicMonitorStatus) ([][]byte, error) {
	return f.images, f.err
}

func oneBotTestService(t *testing.T) (*Service, *fakeOneBotClient, *store.Store) {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "onebot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	fake := &fakeOneBotClient{}
	svc.OneBotClient = fake
	cfg, err := st.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.OneBotEnabled = true
	cfg.OneBotBaseURL = "http://llbot:3000/"
	cfg.OneBotHTTPToken = "http-token"
	cfg.OneBotWebhookToken = "webhook-token"
	cfg.OneBotGroupIDs = []string{"100", "100", " 200 "}
	if _, err := svc.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	return svc, fake, st
}

func oneBotEvent(t *testing.T, messageID, groupID, userID string) onebot.Event {
	t.Helper()
	var event onebot.Event
	body := `{"self_id":42,"post_type":"message","message_type":"group","sub_type":"normal","message_id":` + messageID + `,"group_id":` + groupID + `,"user_id":` + userID + `,"message":[{"type":"at","data":{"qq":"42"}},{"type":"text","data":{"text":"状态"}}]}`
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func TestOneBotEventFiltersSecretsAndRateLimits(t *testing.T) {
	svc, fake, st := oneBotTestService(t)
	public, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "公开异常", BaseURL: "https://public.example.test", APIKey: "sk-public", PublicEnabled: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	private, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "私有异常", BaseURL: "https://private.example.test", APIKey: "sk-private", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", public.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusFailed, Input: "private input", Output: "private output", Error: "private raw error"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", private.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusFailed, Error: "private raw error"}); err != nil {
		t.Fatal(err)
	}

	event := oneBotEvent(t, "1", "100", "8")
	if err := svc.HandleOneBotEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("messages = %+v", fake.sent)
	}
	message := fake.sent[0]
	if message.baseURL != "http://llbot:3000" || message.token != "http-token" || message.groupID != "100" {
		t.Fatalf("send metadata = %+v", message)
	}
	for _, hidden := range []string{"私有异常", "sk-public", "private input", "private output", "private raw error", "public.example.test"} {
		if strings.Contains(message.text, hidden) {
			t.Fatalf("reply leaked %q: %s", hidden, message.text)
		}
	}
	if !strings.Contains(message.text, "公开异常") || !strings.Contains(message.text, "异常 1") {
		t.Fatalf("reply = %s", message.text)
	}

	if err := svc.HandleOneBotEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleOneBotEvent(t.Context(), oneBotEvent(t, "2", "100", "8")); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("dedupe/cooldown messages = %d", len(fake.sent))
	}
	if err := svc.HandleOneBotEvent(t.Context(), oneBotEvent(t, "3", "100", "9")); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 2 {
		t.Fatalf("different user messages = %d", len(fake.sent))
	}
	if err := svc.HandleOneBotEvent(t.Context(), oneBotEvent(t, "4", "999", "10")); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 2 {
		t.Fatalf("non-whitelist messages = %d", len(fake.sent))
	}
	payload := []byte(`{"message":"payload"}`)
	if err := svc.AuthorizeOneBotEvent(t.Context(), testOneBotSignature("webhook-token", payload), payload); err != nil {
		t.Fatalf("valid signature error = %v", err)
	}
	if err := svc.AuthorizeOneBotEvent(t.Context(), "sha1=bad", payload); !errors.Is(err, ErrOneBotUnauthorized) {
		t.Fatalf("error = %v", err)
	}
}

func testOneBotSignature(token string, payload []byte) string {
	mac := hmac.New(sha1.New, []byte(token))
	_, _ = mac.Write(payload)
	return "sha1=" + fmt.Sprintf("%x", mac.Sum(nil))
}

func TestOneBotStatusAndMessageSplitting(t *testing.T) {
	svc, fake, st := oneBotTestService(t)
	status, err := svc.OneBotStatus(t.Context())
	if err != nil || status.Status != "online" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	fake.loginErr = errors.New("token=must-not-leak")
	status, err = svc.OneBotStatus(t.Context())
	if err != nil || status.Status != "error" || status.Error != "无法连接 OneBot 服务" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	cfg, err := st.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.OneBotEnabled = false
	if _, err := st.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	status, err = svc.OneBotStatus(t.Context())
	if err != nil || status.Status != "disabled" {
		t.Fatalf("disabled status=%+v err=%v", status, err)
	}
	cfg.OneBotEnabled = true
	cfg.OneBotBaseURL = ""
	cfg.OneBotGroupIDs = nil
	if _, err := st.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	status, err = svc.OneBotStatus(t.Context())
	if err != nil || status.Status != "unconfigured" {
		t.Fatalf("unconfigured status=%+v err=%v", status, err)
	}
	parts := splitOneBotMessage(strings.Repeat("状", 1901), 1900)
	if len(parts) != 2 || len([]rune(parts[0])) != 1900 || len([]rune(parts[1])) != 1 {
		t.Fatalf("parts=%d len=%d/%d", len(parts), len([]rune(parts[0])), len([]rune(parts[1])))
	}
}

func TestOneBotEventSendsRenderedPublicStatusImage(t *testing.T) {
	svc, fake, st := oneBotTestService(t)
	public, err := st.CreateCard(t.Context(), domain.ModelCard{Name: "公开卡片", PublicEnabled: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProbe(t.Context(), "", public.ID, domain.ProbeModel, monitor.ProbeResult{Status: monitor.StatusOperational, Latency: 123 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	svc.OneBotStatusImageRenderer = fakeOneBotStatusImageRenderer{images: [][]byte{{0x89, 0x50, 0x4e, 0x47}}}
	if err := svc.HandleOneBotEvent(t.Context(), oneBotEvent(t, "1", "100", "8")); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 0 || len(fake.images) != 1 {
		t.Fatalf("text=%d images=%d", len(fake.sent), len(fake.images))
	}
	image := fake.images[0]
	if image.baseURL != "http://llbot:3000" || image.token != "http-token" || image.groupID != "100" || !bytes.Equal(image.png, []byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatalf("image = %+v", image)
	}
}
