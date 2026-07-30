package qqbot

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestVerifyWebhookAndValidationResponse(t *testing.T) {
	secret := "test-secret"
	timestamp := "1725442341"
	body := []byte(`{"op":0,"d":{},"t":"GROUP_AT_MESSAGE_CREATE"}`)
	_, privateKey, err := keyPair(secret)
	if err != nil {
		t.Fatal(err)
	}
	signature := hex.EncodeToString(ed25519.Sign(privateKey, append([]byte(timestamp), body...)))
	if !VerifyWebhook(secret, timestamp, signature, body) {
		t.Fatal("有效签名未通过")
	}
	if VerifyWebhook(secret, timestamp, signature, append(body, ' ')) {
		t.Fatal("被修改的请求体不应通过")
	}

	response, err := ValidationResponse(secret, ValidationRequest{PlainToken: "plain", EventTS: timestamp})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		PlainToken string `json:"plain_token"`
		Signature  string `json:"signature"`
	}
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatal(err)
	}
	expected := hex.EncodeToString(ed25519.Sign(privateKey, []byte(timestamp+"plain")))
	if decoded.PlainToken != "plain" || decoded.Signature != expected {
		t.Fatalf("验证响应错误: %+v", decoded)
	}
}
