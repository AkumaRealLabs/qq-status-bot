package axonhub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("AxonHub 管理员认证失败")

type Channel struct {
	ID             string
	Name           string
	Type           string
	Status         string
	OrderingWeight int
	Tags           []string
	Models         []string
}

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func (c Client) SignIn(ctx context.Context, email, password string) (string, time.Time, error) {
	body, err := json.Marshal(map[string]string{"email": strings.TrimSpace(email), "password": password})
	if err != nil {
		return "", time.Time{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/admin/auth/signin", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", time.Time{}, safeError(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", time.Time{}, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("AxonHub 登录 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || strings.TrimSpace(payload.Token) == "" {
		return "", time.Time{}, errors.New("AxonHub 登录响应格式错误")
	}
	return payload.Token, jwtExpiry(payload.Token), nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Now().Add(6 * time.Hour)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Now().Add(6 * time.Hour)
	}
	var claims struct {
		ExpiresAt int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.ExpiresAt <= 0 {
		return time.Now().Add(6 * time.Hour)
	}
	return time.Unix(claims.ExpiresAt, 0).UTC()
}

const channelFields = `id name type status orderingWeight tags allModelEntries { requestModel }`

func (c Client) Channels(ctx context.Context) ([]Channel, error) {
	var data struct {
		Channels []channelPayload `json:"allChannelSummarys"`
	}
	err := c.graphql(ctx, `query AUMChannels { allChannelSummarys(includeArchived: true) { `+channelFields+` } }`, nil, &data)
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(data.Channels))
	for _, item := range data.Channels {
		out = append(out, item.channel())
	}
	return out, nil
}

func (c Client) UpdateFields(ctx context.Context, id string, tags []string, weight int) (Channel, error) {
	var data struct {
		Channel channelPayload `json:"updateChannel"`
	}
	input := map[string]any{"tags": tags, "orderingWeight": weight}
	err := c.graphql(ctx, `mutation AUMUpdateChannel($id: ID!, $input: UpdateChannelInput!) { updateChannel(id: $id, input: $input) { `+channelFields+` } }`, map[string]any{"id": id, "input": input}, &data)
	return data.Channel.channel(), err
}

type channelPayload struct {
	ID             json.RawMessage `json:"id"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Status         string          `json:"status"`
	OrderingWeight int             `json:"orderingWeight"`
	Tags           []string        `json:"tags"`
	Models         []struct {
		RequestModel string `json:"requestModel"`
	} `json:"allModelEntries"`
}

func (p channelPayload) channel() Channel {
	id := strings.Trim(string(p.ID), `"`)
	models := make([]string, 0, len(p.Models))
	for _, item := range p.Models {
		if model := strings.TrimSpace(item.RequestModel); model != "" {
			models = append(models, model)
		}
	}
	return Channel{
		ID: id, Name: p.Name, Type: strings.ToLower(p.Type), Status: strings.ToLower(p.Status), OrderingWeight: p.OrderingWeight,
		Tags: p.Tags, Models: models,
	}
}

type graphQLError struct {
	Message string `json:"message"`
}

func (c Client) graphql(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/admin/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.Token) == "" {
		return ErrUnauthorized
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return safeError(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: HTTP %d", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("AxonHub HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return errors.New("AxonHub 响应格式错误")
	}
	if len(envelope.Errors) > 0 {
		return errors.New("AxonHub GraphQL 请求失败")
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return errors.New("AxonHub 数据解析失败")
	}
	return nil
}

func safeError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("AxonHub 请求超时")
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return errors.New("AxonHub 连接失败")
}
