package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-upstream-monitor/internal/app"
	"ai-upstream-monitor/internal/onebot"
	"ai-upstream-monitor/internal/store"
)

type oneBotHTTPFake struct {
	sent int
}

func (f *oneBotHTTPFake) GetLoginInfo(_ context.Context, _, _ string) (onebot.LoginInfo, error) {
	return onebot.LoginInfo{}, nil
}

func (f *oneBotHTTPFake) SendGroupMessage(_ context.Context, _, _, _, _ string) error {
	f.sent++
	return nil
}

func TestOneBotRoutesAuthenticateWebhookAndAdmin(t *testing.T) {
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "onebot-http.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := app.New(st)
	fake := &oneBotHTTPFake{}
	svc.OneBotClient = fake
	cfg, err := st.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cfg.OneBotEnabled = true
	cfg.OneBotBaseURL = "http://llbot:3000"
	cfg.OneBotHTTPToken = "http-token"
	cfg.OneBotWebhookToken = "webhook-token"
	cfg.OneBotGroupIDs = []string{"100"}
	if _, err := svc.UpdateSettings(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&Server{App: svc}).Routes())
	defer ts.Close()
	body := `{"self_id":42,"post_type":"message","message_type":"group","sub_type":"normal","message_id":1,"group_id":100,"user_id":8,"message":[{"type":"at","data":{"qq":"42"}},{"type":"text","data":{"text":"状态"}}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/onebot/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", "sha1=wrong")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || fake.sent != 0 {
		t.Fatalf("unauthorized status=%d sent=%d", resp.StatusCode, fake.sent)
	}
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/onebot/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", oneBotHTTPTestSignature("webhook-token", []byte(body)))
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || fake.sent != 0 {
		t.Fatalf("authorized status=%d sent=%d", resp.StatusCode, fake.sent)
	}

	user, err := st.CreateUser(t.Context(), "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateSession(t.Context(), user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/onebot/status", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var status struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || status.Status != "online" {
		t.Fatalf("status=%d body=%+v", resp.StatusCode, status)
	}
}

func oneBotHTTPTestSignature(token string, payload []byte) string {
	mac := hmac.New(sha1.New, []byte(token))
	_, _ = mac.Write(payload)
	return "sha1=" + fmt.Sprintf("%x", mac.Sum(nil))
}
