package qqbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestSendGroupTextUsesProactivePayloadOnly(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "3600"})
	}))
	defer tokenServer.Close()
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/groups/group/messages" {
			t.Fatalf("主动消息路径错误: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer apiServer.Close()
	client := &Client{AppID: "app", AppSecret: "secret", APIBaseURL: apiServer.URL, TokenURL: tokenServer.URL, HTTP: apiServer.Client()}
	if err := client.SendGroupText(context.Background(), "group", "hello"); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 || payload["content"] != "hello" || payload["msg_type"] != float64(0) {
		t.Fatalf("主动消息载荷错误: %#v", payload)
	}
	for _, forbidden := range []string{"msg_id", "event_id", "msg_seq"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("主动消息不应包含 %s: %#v", forbidden, payload)
		}
	}
}

func TestSendGroupTextWithKeyboardUsesProactivePayload(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "3600"})
	}))
	defer tokenServer.Close()
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/groups/group/messages" {
			t.Fatalf("主动键盘消息路径错误: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer apiServer.Close()
	client := &Client{AppID: "app", AppSecret: "secret", APIBaseURL: apiServer.URL, TokenURL: tokenServer.URL, HTTP: apiServer.Client()}
	if err := client.SendGroupTextWithKeyboard(t.Context(), "group", "hello", testKeyboard()); err != nil {
		t.Fatal(err)
	}
	if payload["content"] != "hello" || payload["msg_type"] != float64(0) || payload["keyboard"] == nil {
		t.Fatalf("主动键盘消息载荷错误: %#v", payload)
	}
	for _, forbidden := range []string{"msg_id", "event_id", "msg_seq"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("主动键盘消息不应包含 %s: %#v", forbidden, payload)
		}
	}
}

func TestSendGroupImageUsesProactivePayloadOnly(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "3600"})
	}))
	defer tokenServer.Close()
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/groups/group/upload_prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"upload_id": "upload", "block_size": "8", "parts": []any{}})
		case "/v2/groups/group/files":
			_ = json.NewEncoder(w).Encode(map[string]string{"file_info": "file-info"})
		case "/v2/groups/group/messages":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()
	client := &Client{AppID: "app", AppSecret: "secret", APIBaseURL: apiServer.URL, TokenURL: tokenServer.URL, HTTP: apiServer.Client()}
	if err := client.SendGroupImage(context.Background(), "group", []byte("\x89PNG\r\n\x1a\n")); err != nil {
		t.Fatal(err)
	}
	if payload["msg_type"] != float64(7) {
		t.Fatalf("主动图片消息类型错误: %#v", payload)
	}
	media, ok := payload["media"].(map[string]any)
	if !ok || media["file_info"] != "file-info" {
		t.Fatalf("主动图片媒体载荷错误: %#v", payload)
	}
	for _, forbidden := range []string{"msg_id", "msg_seq", "event_id"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("主动图片不应包含 %s: %#v", forbidden, payload)
		}
	}
}

func TestReplyGroupTextWithKeyboardUsesEventID(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "3600"})
	}))
	defer tokenServer.Close()
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/groups/group/messages" {
			t.Fatalf("互动回复请求错误: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer apiServer.Close()
	client := &Client{AppID: "app", AppSecret: "secret", APIBaseURL: apiServer.URL, TokenURL: tokenServer.URL, HTTP: apiServer.Client()}
	if err := client.ReplyGroupTextWithKeyboard(t.Context(), "group", "", "event-1", "请选择", 1, testKeyboard()); err != nil {
		t.Fatal(err)
	}
	if payload["event_id"] != "event-1" || payload["content"] != "请选择" || payload["msg_type"] != float64(0) {
		t.Fatalf("互动回复载荷错误: %#v", payload)
	}
	for _, forbidden := range []string{"msg_id", "msg_seq"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("互动回复不应包含 %s: %#v", forbidden, payload)
		}
	}
	keyboard, ok := payload["keyboard"].(map[string]any)
	if !ok || keyboard["content"] == nil {
		t.Fatalf("互动回复缺少键盘: %#v", payload)
	}
}

func TestReplyGroupImageWithKeyboardUsesEventID(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "3600"})
	}))
	defer tokenServer.Close()
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/groups/group/upload_prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"upload_id": "upload", "block_size": "8", "parts": []any{}})
		case "/v2/groups/group/files":
			_ = json.NewEncoder(w).Encode(map[string]string{"file_info": "file-info"})
		case "/v2/groups/group/messages":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()
	client := &Client{AppID: "app", AppSecret: "secret", APIBaseURL: apiServer.URL, TokenURL: tokenServer.URL, HTTP: apiServer.Client()}
	if err := client.ReplyGroupImageWithKeyboard(t.Context(), "group", "", "event-1", []byte("\x89PNG\r\n\x1a\n"), testKeyboard()); err != nil {
		t.Fatal(err)
	}
	if payload["event_id"] != "event-1" || payload["msg_type"] != float64(7) || payload["keyboard"] == nil {
		t.Fatalf("互动图片载荷错误: %#v", payload)
	}
	for _, forbidden := range []string{"msg_id", "msg_seq"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("互动图片不应包含 %s: %#v", forbidden, payload)
		}
	}
}

func TestReplyGroupImageWithTargetUsesEventID(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "3600"})
	}))
	defer tokenServer.Close()
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/groups/group/upload_prepare":
			_ = json.NewEncoder(w).Encode(map[string]any{"upload_id": "upload", "block_size": "8", "parts": []any{}})
		case "/v2/groups/group/files":
			_ = json.NewEncoder(w).Encode(map[string]string{"file_info": "file-info"})
		case "/v2/groups/group/messages":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()
	client := &Client{AppID: "app", AppSecret: "secret", APIBaseURL: apiServer.URL, TokenURL: tokenServer.URL, HTTP: apiServer.Client()}
	if err := client.ReplyGroupImageWithTarget(t.Context(), "group", "", "event-1", []byte("\x89PNG\r\n\x1a\n")); err != nil {
		t.Fatal(err)
	}
	if payload["event_id"] != "event-1" || payload["msg_type"] != float64(7) || payload["keyboard"] != nil {
		t.Fatalf("互动图片目标载荷错误: %#v", payload)
	}
}

func TestInteractiveReplyRequiresExactlyOneReference(t *testing.T) {
	client := &Client{}
	for _, ids := range [][2]string{{}, {"message", "event"}} {
		if err := client.ReplyGroupTextWithKeyboard(t.Context(), "group", ids[0], ids[1], "content", 1, testKeyboard()); err == nil {
			t.Fatalf("应拒绝引用组合 msg_id=%q event_id=%q", ids[0], ids[1])
		}
	}
}

func TestRespondInteractionUsesPutAndResultCode(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "3600"})
	}))
	defer tokenServer.Close()
	var payload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/interactions/interaction-1" {
			t.Fatalf("互动确认请求错误: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer apiServer.Close()
	client := &Client{AppID: "app", AppSecret: "secret", APIBaseURL: apiServer.URL, TokenURL: tokenServer.URL, HTTP: apiServer.Client()}
	if err := client.RespondInteraction(t.Context(), "interaction-1", 2); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload["code"] != float64(2) {
		t.Fatalf("互动确认载荷错误: %#v", payload)
	}
}

func testKeyboard() Keyboard {
	return Keyboard{Content: &KeyboardContent{Rows: []KeyboardRow{{Buttons: []KeyboardButton{{
		ID: "status", RenderData: ButtonRenderData{Label: "查看状态", Style: 1},
		Action: KeyboardButtonAction{
			Type: ButtonActionCallback, Permission: ButtonPermission{Type: ButtonPermissionAll}, Data: "qq-status-bot:status",
		},
	}}}}}}
}

func TestAPIErrorPreservesPermissionCode(t *testing.T) {
	err := apiResponseError(http.StatusForbidden, []byte(`{"code":40034102,"trace_id":"trace"}`))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 40034102 || !strings.Contains(err.Error(), "40034102") {
		t.Fatalf("错误码未保留: %v", err)
	}
}

func TestUploadPartsUsesOneBasedIndexesAndPartSize(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "expires_in": "3600"})
	}))
	defer tokenServer.Close()

	type finishRequest struct {
		PartIndex int    `json:"part_index"`
		BlockSize string `json:"block_size"`
	}
	var mu sync.Mutex
	partBodies := make(map[string][]byte)
	var finishes []finishRequest
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			partBodies[r.URL.Path] = body
			mu.Unlock()
		case r.Method == http.MethodPost && r.URL.Path == "/v2/groups/group/upload_part_finish":
			var request finishRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			mu.Lock()
			finishes = append(finishes, request)
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()

	client := &Client{
		AppID: "app", AppSecret: "secret", APIBaseURL: apiServer.URL,
		TokenURL: tokenServer.URL, HTTP: apiServer.Client(),
	}
	prepared := uploadPrepareResponse{
		UploadID: "upload", BlockSize: "4",
		Parts: []uploadPart{
			{Index: 2, PresignedURL: apiServer.URL + "/part/2", BlockSize: "2"},
			{Index: 1, PresignedURL: apiServer.URL + "/part/1"},
		},
	}
	if err := client.uploadParts(t.Context(), "group", prepared, []byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(partBodies["/part/1"], []byte("abcd")) || !bytes.Equal(partBodies["/part/2"], []byte("ef")) {
		t.Fatalf("分片内容错误: %#v", partBodies)
	}
	if len(finishes) != 2 || finishes[0].PartIndex != 1 || finishes[0].BlockSize != "4" || finishes[1].PartIndex != 2 || finishes[1].BlockSize != "2" {
		t.Fatalf("分片确认请求错误: %+v", finishes)
	}
}

func TestUploadPartsRejectsInvalidIndexes(t *testing.T) {
	client := &Client{}
	for _, index := range []int{0, 3} {
		prepared := uploadPrepareResponse{
			BlockSize: "4",
			Parts:     []uploadPart{{Index: index, PresignedURL: "https://upload.example/part"}},
		}
		if err := client.uploadParts(t.Context(), "group", prepared, []byte("abcdef")); err == nil {
			t.Fatalf("应拒绝分片序号 %d", index)
		}
	}
}
