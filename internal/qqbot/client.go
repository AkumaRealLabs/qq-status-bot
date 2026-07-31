package qqbot

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxAPIResponseBytes = 1 << 20
	maxImageBytes       = 20 << 20
	md5PrefixBytes      = 10002432
)

type Client struct {
	AppID       string
	AppSecret   string
	Credentials func() (string, string)
	APIBaseURL  string
	TokenURL    string
	HTTP        *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
	tokenAppID  string
	rateMu      sync.Mutex
	nextAccount time.Time
	nextGroup   map[string]time.Time
}

// APIError 保留 QQ HTTP 状态和结构化错误码，供调用方区分权限错误与临时错误。
type APIError struct {
	HTTPStatus int
	Code       int
	TraceID    string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("QQ API 错误：HTTP %d，错误码 %d，trace_id=%s", e.HTTPStatus, e.Code, e.TraceID)
	}
	return fmt.Sprintf("QQ API 错误：HTTP %d", e.HTTPStatus)
}

type tokenResponse struct {
	Code        int             `json:"code"`
	Message     string          `json:"message"`
	AccessToken string          `json:"access_token"`
	ExpiresIn   json.RawMessage `json:"expires_in"`
}

type uploadPrepareResponse struct {
	UploadID  string       `json:"upload_id"`
	BlockSize string       `json:"block_size"`
	Parts     []uploadPart `json:"parts"`
}

type uploadPart struct {
	Index        int    `json:"index"`
	PresignedURL string `json:"presigned_url"`
	BlockSize    string `json:"block_size"`
}

func (c *Client) ReplyGroupImage(ctx context.Context, groupOpenID, messageID string, image []byte) error {
	if len(image) == 0 || len(image) > maxImageBytes {
		return errors.New("截图大小不符合 QQ 图片限制")
	}
	if len(image) < 8 || !bytes.Equal(image[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return errors.New("截图不是有效的 PNG 图片")
	}

	fileInfo, err := c.uploadGroupImage(ctx, groupOpenID, image)
	if err != nil {
		return err
	}
	payload := struct {
		MessageType int    `json:"msg_type"`
		MessageID   string `json:"msg_id"`
		MessageSeq  int    `json:"msg_seq"`
		Media       struct {
			FileInfo string `json:"file_info"`
		} `json:"media"`
	}{MessageType: 7, MessageID: messageID, MessageSeq: 1}
	payload.Media.FileInfo = fileInfo
	return c.post(ctx, groupPath(groupOpenID, "/messages"), payload, nil)
}

func (c *Client) ReplyGroupText(ctx context.Context, groupOpenID, messageID, content string, messageSeq int) error {
	payload := struct {
		Content     string `json:"content"`
		MessageType int    `json:"msg_type"`
		MessageID   string `json:"msg_id"`
		MessageSeq  int    `json:"msg_seq"`
	}{Content: content, MessageType: 0, MessageID: messageID, MessageSeq: messageSeq}
	return c.post(ctx, groupPath(groupOpenID, "/messages"), payload, nil)
}

// SendGroupText 发送主动群消息；该接口不需要被动回复字段，因此只提交内容和消息类型。
func (c *Client) SendGroupText(ctx context.Context, groupOpenID, content string) error {
	if strings.TrimSpace(groupOpenID) == "" {
		return errors.New("告警群 OpenID 不能为空")
	}
	if strings.TrimSpace(content) == "" {
		return errors.New("主动消息内容不能为空")
	}
	if err := c.waitActiveRate(ctx, groupOpenID); err != nil {
		return err
	}
	payload := struct {
		Content     string `json:"content"`
		MessageType int    `json:"msg_type"`
	}{Content: content, MessageType: 0}
	return c.post(ctx, groupPath(groupOpenID, "/messages"), payload, nil)
}

func (c *Client) waitActiveRate(ctx context.Context, groupOpenID string) error {
	c.rateMu.Lock()
	if c.nextGroup == nil {
		c.nextGroup = make(map[string]time.Time)
	}
	now := time.Now()
	ready := now
	if c.nextAccount.After(ready) {
		ready = c.nextAccount
	}
	if groupReady := c.nextGroup[groupOpenID]; groupReady.After(ready) {
		ready = groupReady
	}
	c.nextAccount = ready.Add(2 * time.Second)
	c.nextGroup[groupOpenID] = ready.Add(3 * time.Second)
	wait := time.Until(ready)
	c.rateMu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) uploadGroupImage(ctx context.Context, groupOpenID string, image []byte) (string, error) {
	fullMD5 := md5.Sum(image)
	fullSHA1 := sha1.Sum(image)
	prefixEnd := min(len(image), md5PrefixBytes)
	prefixMD5 := md5.Sum(image[:prefixEnd])
	preparePayload := struct {
		FileType int    `json:"file_type"`
		FileSize string `json:"file_size"`
		FileName string `json:"file_name"`
		MD5      string `json:"md5"`
		SHA1     string `json:"sha1"`
		MD510M   string `json:"md5_10m"`
	}{
		FileType: 1,
		FileSize: strconv.Itoa(len(image)),
		FileName: "ggapi-status.png",
		MD5:      hex.EncodeToString(fullMD5[:]),
		SHA1:     hex.EncodeToString(fullSHA1[:]),
		MD510M:   hex.EncodeToString(prefixMD5[:]),
	}
	var prepared uploadPrepareResponse
	if err := c.post(ctx, groupPath(groupOpenID, "/upload_prepare"), preparePayload, &prepared); err != nil {
		return "", fmt.Errorf("准备上传 QQ 图片: %w", err)
	}
	if prepared.UploadID == "" {
		return "", errors.New("准备上传 QQ 图片: 缺少 upload_id")
	}
	if err := c.uploadParts(ctx, groupOpenID, prepared, image); err != nil {
		return "", err
	}
	mergePayload := struct {
		FileType int    `json:"file_type"`
		Send     bool   `json:"srv_send_msg"`
		FileName string `json:"file_name"`
		UploadID string `json:"upload_id"`
	}{FileType: 1, Send: false, FileName: "ggapi-status.png", UploadID: prepared.UploadID}
	var merged struct {
		FileInfo string `json:"file_info"`
	}
	if err := c.post(ctx, groupPath(groupOpenID, "/files"), mergePayload, &merged); err != nil {
		return "", fmt.Errorf("合并 QQ 图片: %w", err)
	}
	if merged.FileInfo == "" {
		return "", errors.New("合并 QQ 图片: 缺少 file_info")
	}
	return merged.FileInfo, nil
}

func (c *Client) uploadParts(ctx context.Context, groupOpenID string, prepared uploadPrepareResponse, image []byte) error {
	if len(prepared.Parts) == 0 {
		return nil
	}
	blockSize, err := strconv.Atoi(prepared.BlockSize)
	if err != nil || blockSize <= 0 {
		return errors.New("准备上传 QQ 图片: 无效的分片大小")
	}
	sort.Slice(prepared.Parts, func(i, j int) bool { return prepared.Parts[i].Index < prepared.Parts[j].Index })
	for _, part := range prepared.Parts {
		if part.Index <= 0 || part.Index-1 > (len(image)-1)/blockSize {
			return errors.New("准备上传 QQ 图片: 无效的分片序号")
		}
		partSize := blockSize
		if strings.TrimSpace(part.BlockSize) != "" {
			partSize, err = strconv.Atoi(part.BlockSize)
			if err != nil || partSize <= 0 {
				return errors.New("准备上传 QQ 图片: 无效的分片大小")
			}
		}
		start := (part.Index - 1) * blockSize
		end := start + min(partSize, len(image)-start)
		chunk := image[start:end]
		if err := c.putPart(ctx, part.PresignedURL, chunk); err != nil {
			return fmt.Errorf("上传 QQ 图片分片 %d: %w", part.Index, err)
		}
		chunkMD5 := md5.Sum(chunk)
		finishPayload := struct {
			UploadID  string `json:"upload_id"`
			PartIndex int    `json:"part_index"`
			BlockSize string `json:"block_size"`
			MD5       string `json:"md5"`
		}{
			UploadID:  prepared.UploadID,
			PartIndex: part.Index,
			BlockSize: strconv.Itoa(len(chunk)),
			MD5:       hex.EncodeToString(chunkMD5[:]),
		}
		if err := c.post(ctx, groupPath(groupOpenID, "/upload_part_finish"), finishPayload, nil); err != nil {
			return fmt.Errorf("确认 QQ 图片分片 %d: %w", part.Index, err)
		}
	}
	return nil
}

func (c *Client) putPart(ctx context.Context, rawURL string, chunk []byte) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("无效的分片上传地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, bytes.NewReader(chunk))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAPIResponseBytes))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("HTTP 状态码 %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, payload, out any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.token(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.APIBaseURL, "/")+path, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "QQBot "+token)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return fmt.Errorf("QQ API 请求失败: %w", err)
		}
		body, readErr := readResponse(resp)
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.clearToken()
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return apiResponseError(resp.StatusCode, body)
		}
		if out != nil && len(body) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return errors.New("QQ API 返回了无效 JSON")
			}
		}
		return nil
	}
	return errors.New("QQ API 鉴权失败")
}

func (c *Client) token(ctx context.Context) (string, error) {
	appID, appSecret := c.credentials()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && c.tokenAppID == appID && time.Until(c.tokenExpiry) > time.Minute {
		return c.accessToken, nil
	}
	payload, _ := json.Marshal(map[string]string{"appId": appID, "clientSecret": appSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("获取 QQ Access Token: %w", err)
	}
	body, err := readResponse(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", apiResponseError(resp.StatusCode, body)
	}
	var result tokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", errors.New("QQ Access Token 返回了无效 JSON")
	}
	if result.Code != 0 || result.AccessToken == "" {
		return "", fmt.Errorf("获取 QQ Access Token 失败，错误码 %d", result.Code)
	}
	expiresIn, err := parseExpiresIn(result.ExpiresIn)
	if err != nil || expiresIn <= 0 {
		return "", errors.New("QQ Access Token 有效期无效")
	}
	c.accessToken = result.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	c.tokenAppID = appID
	return c.accessToken, nil
}

func (c *Client) clearToken() {
	c.mu.Lock()
	c.accessToken = ""
	c.tokenExpiry = time.Time{}
	c.tokenAppID = ""
	c.mu.Unlock()
}

func (c *Client) credentials() (string, string) {
	if c.Credentials != nil {
		return c.Credentials()
	}
	return c.AppID, c.AppSecret
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func parseExpiresIn(raw json.RawMessage) (int64, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strconv.ParseInt(text, 10, 64)
	}
	var number int64
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, err
	}
	return number, nil
}

func readResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxAPIResponseBytes {
		return nil, errors.New("QQ API 响应过大")
	}
	return body, nil
}

func apiResponseError(status int, body []byte) error {
	var result struct {
		Code    int    `json:"err_code"`
		CodeAlt int    `json:"code"`
		TraceID string `json:"trace_id"`
	}
	_ = json.Unmarshal(body, &result)
	if result.Code == 0 {
		result.Code = result.CodeAlt
	}
	return &APIError{HTTPStatus: status, Code: result.Code, TraceID: result.TraceID}
}

func groupPath(groupOpenID, suffix string) string {
	return "/v2/groups/" + url.PathEscape(strings.TrimSpace(groupOpenID)) + suffix
}
