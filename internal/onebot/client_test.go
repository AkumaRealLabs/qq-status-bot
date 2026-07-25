package onebot

import (
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
	if err := client.SendGroupMessage(t.Context(), ts.URL, "test-token", "100", "成本同步失败"); err != nil {
		t.Fatal(err)
	}
	if sent.GroupID != "100" || len(sent.Message) != 1 || sent.Message[0].Type != "text" || sent.Message[0].Data.Text != "成本同步失败" {
		t.Fatalf("payload = %+v", sent)
	}
}
