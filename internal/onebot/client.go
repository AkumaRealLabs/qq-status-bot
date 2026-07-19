// Package onebot 封装 OneBot 11 的 HTTP 边界，避免协议细节进入 app/httpapi。
package onebot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const maxResponseBytes = 1 << 20

var positiveIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

type Client struct {
	HTTP *http.Client
}

type LoginInfo struct {
	UserID   json.RawMessage `json:"user_id"`
	Nickname string          `json:"nickname"`
}

type MessageSegment struct {
	Type string `json:"type"`
	Data struct {
		Text string `json:"text,omitempty"`
		QQ   string `json:"qq,omitempty"`
	} `json:"data"`
}

// Event 仅包含群命令处理需要的 OneBot 11 字段。
// ID 保留为 RawMessage，兼容上游把数值编码为 JSON number 或 string。
type Event struct {
	Time        int64            `json:"time"`
	SelfID      json.RawMessage  `json:"self_id"`
	PostType    string           `json:"post_type"`
	MessageType string           `json:"message_type"`
	SubType     string           `json:"sub_type"`
	MessageID   json.RawMessage  `json:"message_id"`
	GroupID     json.RawMessage  `json:"group_id"`
	UserID      json.RawMessage  `json:"user_id"`
	RawMessage  string           `json:"raw_message"`
	Message     []MessageSegment `json:"message"`
}

func (e Event) IsNormalGroupMessage() bool {
	return e.PostType == "message" && e.MessageType == "group" && e.SubType == "normal"
}

func (e Event) GroupIDString() string   { return rawID(e.GroupID) }
func (e Event) UserIDString() string    { return rawID(e.UserID) }
func (e Event) MessageIDString() string { return rawID(e.MessageID) }
func (e Event) SelfIDString() string    { return rawID(e.SelfID) }

// IsStatusCommand 仅接受数组消息段中的 @机器人 + 精确命令。
// 部分 OneBot 实现会把命令拆成多个连续 text 段，合并后仍须恰好为状态命令。
func (e Event) IsStatusCommand() bool {
	if !e.IsNormalGroupMessage() {
		return false
	}
	return e.isArrayStatusCommand() || e.isRawStatusCommand()
}

func (e Event) isArrayStatusCommand() bool {
	if len(e.Message) < 2 {
		return false
	}
	mentionIndex := 0
	for mentionIndex < len(e.Message) && isBlankTextSegment(e.Message[mentionIndex]) {
		mentionIndex++
	}
	if mentionIndex >= len(e.Message)-1 {
		return false
	}
	mention := e.Message[mentionIndex]
	if mention.Type != "at" || rawIDString(mention.Data.QQ) != e.SelfIDString() {
		return false
	}
	var command strings.Builder
	for _, segment := range e.Message[mentionIndex+1:] {
		if segment.Type != "text" {
			return false
		}
		command.WriteString(segment.Data.Text)
	}
	text := normalizeStatusCommand(command.String())
	return text == "状态" || text == "status"
}

func isBlankTextSegment(segment MessageSegment) bool {
	return segment.Type == "text" && normalizeStatusCommand(segment.Data.Text) == ""
}

// normalizeStatusCommand 仅清理 QQ/OneBot 可能插入的空白与零宽分隔符，命令内容仍须完全匹配。
func normalizeStatusCommand(value string) string {
	value = strings.NewReplacer("\u200b", "", "\ufeff", "", "\u2060", "").Replace(value)
	return strings.Join(strings.Fields(value), "")
}

// isRawStatusCommand 兼容 LLBot 在数组段异常时仍会提供的 CQ 原始消息。
// 只接受首段为指向机器人的 CQ at，且余下内容规范化后为精确状态命令。
func (e Event) isRawStatusCommand() bool {
	raw := strings.TrimSpace(e.RawMessage)
	const prefix = "[CQ:at,"
	if !strings.HasPrefix(raw, prefix) {
		return false
	}
	end := strings.IndexByte(raw, ']')
	if end < len(prefix) {
		return false
	}
	target := cqAttribute(raw[len(prefix):end], "qq")
	if rawIDString(target) == "" || rawIDString(target) != e.SelfIDString() {
		return false
	}
	text := normalizeStatusCommand(raw[end+1:])
	return text == "状态" || text == "status"
}

func cqAttribute(attributes, key string) string {
	for _, attribute := range strings.Split(attributes, ",") {
		name, value, found := strings.Cut(attribute, "=")
		if found && name == key {
			return value
		}
	}
	return ""
}

func rawID(raw json.RawMessage) string {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return ""
	}
	var quoted string
	if json.Unmarshal(raw, &quoted) == nil {
		value = quoted
	}
	return rawIDString(value)
}

func rawIDString(value string) string {
	value = strings.TrimSpace(value)
	if !positiveIDPattern.MatchString(value) {
		return ""
	}
	return value
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

// SendGroupImage 使用 OneBot 11 的 base64 图片段发送 PNG，不落地到 LLBot 文件系统。
func (c *Client) SendGroupImage(ctx context.Context, baseURL, token, groupID string, png []byte) error {
	if len(png) == 0 {
		return errors.New("onebot image is empty")
	}
	payload := struct {
		GroupID string `json:"group_id"`
		Message []struct {
			Type string `json:"type"`
			Data struct {
				File string `json:"file"`
			} `json:"data"`
		} `json:"message"`
	}{GroupID: groupID}
	segment := struct {
		Type string `json:"type"`
		Data struct {
			File string `json:"file"`
		} `json:"data"`
	}{Type: "image"}
	segment.Data.File = "base64://" + base64.StdEncoding.EncodeToString(png)
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
