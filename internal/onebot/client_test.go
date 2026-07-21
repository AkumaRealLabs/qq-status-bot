package onebot

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientUsesBearerAuthAndArrayMessage(t *testing.T) {
	var sent struct {
		GroupID string `json:"group_id"`
		Message []struct {
			Type string `json:"type"`
			Data struct {
				Text string `json:"text"`
				File string `json:"file"`
			} `json:"data"`
		} `json:"message"`
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q", got)
		}
		switch r.URL.Path {
		case "/get_login_info":
			_, _ = w.Write([]byte(`{"status":"ok","retcode":0,"data":{"user_id":42,"nickname":"bot"}}`))
		case "/send_group_msg":
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"status":"ok","retcode":0,"data":{"message_id":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := Client{HTTP: ts.Client()}
	info, err := client.GetLoginInfo(t.Context(), ts.URL, "test-token")
	if err != nil || info.Nickname != "bot" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if err := client.SendGroupMessage(t.Context(), ts.URL, "test-token", "100", "状态"); err != nil {
		t.Fatal(err)
	}
	if sent.GroupID != "100" || len(sent.Message) != 1 || sent.Message[0].Type != "text" || sent.Message[0].Data.Text != "状态" {
		t.Fatalf("payload = %+v", sent)
	}
	image := []byte{0x89, 0x50, 0x4e, 0x47}
	if err := client.SendGroupImage(t.Context(), ts.URL, "test-token", "100", image); err != nil {
		t.Fatal(err)
	}
	if sent.GroupID != "100" || len(sent.Message) != 1 || sent.Message[0].Type != "image" || sent.Message[0].Data.File != "base64://"+base64.StdEncoding.EncodeToString(image) {
		t.Fatalf("image payload = %+v", sent)
	}
	if err := client.SendGroupImage(t.Context(), ts.URL, "test-token", "100", nil); err == nil {
		t.Fatal("expected empty image error")
	}
	if !bytes.Equal(image, []byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatal("image mutated")
	}
}

func TestEventRequiresExactMentionCommand(t *testing.T) {
	var event Event
	if err := json.Unmarshal([]byte(`{"self_id":42,"post_type":"message","message_type":"group","sub_type":"normal","message_id":1,"group_id":100,"user_id":8,"message":[{"type":"at","data":{"qq":"42"}},{"type":"text","data":{"text":" status "}}]}`), &event); err != nil {
		t.Fatal(err)
	}
	if !event.IsStatusCommand() {
		t.Fatal("expected status command")
	}
	event.Message = append([]MessageSegment{{Type: "text"}}, event.Message...)
	event.Message[0].Data.Text = "\u200b"
	if !event.IsStatusCommand() {
		t.Fatal("expected leading zero-width separator match")
	}
	event.Message = event.Message[1:]
	event.Message[1].Data.Text = "status now"
	if event.IsStatusCommand() {
		t.Fatal("unexpected prefix command match")
	}
	event.Message[1].Data.Text = "状"
	event.Message = append(event.Message, MessageSegment{Type: "text"})
	event.Message[2].Data.Text = "态"
	if !event.IsStatusCommand() {
		t.Fatal("expected split exact command match")
	}
	event.Message[2].Type = "image"
	if event.IsStatusCommand() {
		t.Fatal("unexpected non-text command segment")
	}
}

func TestEventAcceptsStatusCommandSpacingAndSignedMessageID(t *testing.T) {
	for _, text := range []string{"状态", " 状态", "  状态 ", "\u3000状态", "status", " status "} {
		body, err := json.Marshal(map[string]any{
			"self_id":      42,
			"post_type":    "message",
			"message_type": "group",
			"sub_type":     "normal",
			"message_id":   -2073968570,
			"group_id":     100,
			"user_id":      8,
			"message": []any{
				map[string]any{"type": "at", "data": map[string]any{"qq": "42"}},
				map[string]any{"type": "text", "data": map[string]any{"text": text}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var event Event
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		if !event.IsStatusCommand() || event.MessageIDString() != "-2073968570" {
			t.Fatalf("text=%q command=%v message_id=%q", text, event.IsStatusCommand(), event.MessageIDString())
		}
	}
}

func TestEventAcceptsStrictRawCQFallback(t *testing.T) {
	var event Event
	if err := json.Unmarshal([]byte(`{"self_id":42,"post_type":"message","message_type":"group","sub_type":"normal","message_id":1,"group_id":100,"user_id":8,"raw_message":"[CQ:at,qq=42,name=bot] \u200b状态","message":null}`), &event); err != nil {
		t.Fatal(err)
	}
	if !event.IsStatusCommand() {
		t.Fatal("expected strict raw CQ fallback match")
	}
	event.RawMessage = "[CQ:at,qq=99,name=other] 状态"
	if event.IsStatusCommand() {
		t.Fatal("unexpected raw CQ mention match")
	}
	event.RawMessage = "[CQ:at,qq=42,name=bot] status now"
	if event.IsStatusCommand() {
		t.Fatal("unexpected raw CQ prefix command match")
	}
}
