package qqbot

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	HeaderSignature = "X-Signature-Ed25519"
	HeaderTimestamp = "X-Signature-Timestamp"

	OpDispatch   = 0
	OpHeartbeat  = 1
	OpCallbackOK = 12
	OpValidation = 13

	EventGroupAtMessage = "GROUP_AT_MESSAGE_CREATE"
	EventGroupMessage   = "GROUP_MESSAGE_CREATE"
)

type Payload struct {
	ID   string          `json:"id,omitempty"`
	Op   int             `json:"op"`
	Data json.RawMessage `json:"d"`
	Seq  uint64          `json:"s,omitempty"`
	Type string          `json:"t,omitempty"`
}

type ValidationRequest struct {
	PlainToken string `json:"plain_token"`
	EventTS    string `json:"event_ts"`
}

func VerifyWebhook(secret, timestamp, signature string, body []byte) bool {
	publicKey, _, err := keyPair(secret)
	if err != nil || strings.TrimSpace(timestamp) == "" {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	message := append([]byte(timestamp), body...)
	return ed25519.Verify(publicKey, message, sig)
}

func ValidationResponse(secret string, request ValidationRequest) ([]byte, error) {
	if request.PlainToken == "" || request.EventTS == "" {
		return nil, errors.New("无效的回调验证请求")
	}
	_, privateKey, err := keyPair(secret)
	if err != nil {
		return nil, err
	}
	message := []byte(request.EventTS + request.PlainToken)
	return json.Marshal(struct {
		PlainToken string `json:"plain_token"`
		Signature  string `json:"signature"`
	}{
		PlainToken: request.PlainToken,
		Signature:  hex.EncodeToString(ed25519.Sign(privateKey, message)),
	})
}

func CallbackACK(success bool) []byte {
	data := 0
	if !success {
		data = 1
	}
	body, _ := json.Marshal(struct {
		Op   int `json:"op"`
		Data int `json:"d"`
	}{Op: OpCallbackOK, Data: data})
	return body
}

func HeartbeatACK(seq uint64) []byte {
	body, _ := json.Marshal(struct {
		Op   int    `json:"op"`
		Data uint64 `json:"d"`
	}{Op: 11, Data: seq})
	return body
}

func keyPair(secret string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	seed := strings.TrimSpace(secret)
	if seed == "" {
		return nil, nil, errors.New("机器人密钥为空")
	}
	for len(seed) < ed25519.SeedSize {
		seed += seed
	}
	return ed25519.GenerateKey(bytes.NewReader([]byte(seed[:ed25519.SeedSize])))
}
