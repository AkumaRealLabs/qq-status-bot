// Package onebot 封装 OneBot 11 的 HTTP 边界，避免协议细节进入 app/httpapi。
package onebot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBytes = 1 << 20

type Client struct {
	HTTP *http.Client
}

type LoginInfo struct {
	UserID   json.RawMessage `json:"user_id"`
	Nickname string          `json:"nickname"`
}

type apiResponse struct {
	Status  string          `json:"status"`
	RetCode int             `json:"retcode"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) GetLoginInfo(ctx context.Context, baseURL, token string) (LoginInfo, error) {
	var info LoginInfo
	data, err := c.call(ctx, http.MethodGet, baseURL, token, "/get_login_info", nil)
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return info, errors.New("invalid onebot login response")
	}
	return info, nil
}

// SendGroupMessage 使用数组消息格式，避免依赖 OneBot 实现对字符串格式的兼容差异。
func (c *Client) SendGroupMessage(ctx context.Context, baseURL, token, groupID, text string) error {
	payload := struct {
		GroupID string `json:"group_id"`
		Message []struct {
			Type string `json:"type"`
			Data struct {
				Text string `json:"text"`
			} `json:"data"`
		} `json:"message"`
	}{GroupID: groupID}
	segment := struct {
		Type string `json:"type"`
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	}{Type: "text"}
	segment.Data.Text = text
	payload.Message = append(payload.Message, segment)
	_, err := c.call(ctx, http.MethodPost, baseURL, token, "/send_group_msg", payload)
	return err
}

func (c *Client) call(ctx context.Context, method, baseURL, token, path string, payload any) (json.RawMessage, error) {
	endpoint, err := oneBotURL(baseURL, path)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("onebot request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("onebot HTTP status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(bodyBytes) > maxResponseBytes {
		return nil, errors.New("invalid onebot response")
	}
	var result apiResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, errors.New("invalid onebot response")
	}
	if result.Status != "ok" || result.RetCode != 0 {
		return nil, errors.New("onebot api returned an error")
	}
	return result.Data, nil
}

func oneBotURL(baseURL, path string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("invalid onebot base URL")
	}
	return baseURL + path, nil
}
