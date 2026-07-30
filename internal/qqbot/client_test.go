package qqbot

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

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
