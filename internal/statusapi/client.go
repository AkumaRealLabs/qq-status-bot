package statusapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout   = 15 * time.Second
	maxResponseBytes = 2 << 20
)

type Client struct {
	HTTP    *http.Client
	Timeout time.Duration
}

type StatusPage struct {
	Title        string
	Description  string
	MatrixStatus string
	Groups       []MonitorGroup
	Heartbeats   map[int][]Heartbeat
	Uptime       map[string]float64
	Period       string
	Timestamp    int64
}

type MonitorGroup struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	Weight   int       `json:"weight"`
	Monitors []Monitor `json:"monitorList"`
}

type Monitor struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Heartbeat struct {
	Status int      `json:"status"`
	Time   string   `json:"time"`
	Msg    string   `json:"msg"`
	Ping   *float64 `json:"ping"`
}

type configResponse struct {
	Config struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
	} `json:"config"`
	MatrixStatus string `json:"matrixStatus"`
	Timestamp    int64  `json:"timestamp"`
}

type monitorResponse struct {
	Groups []MonitorGroup `json:"monitorGroups"`
	Data   struct {
		Heartbeats map[string][]Heartbeat `json:"heartbeatList"`
		Uptime     map[string]float64     `json:"uptimeList"`
	} `json:"data"`
	Period    string `json:"uptimePeriod"`
	Timestamp int64  `json:"timestamp"`
}

func (c Client) Fetch(ctx context.Context, baseURL, pageID, period string) (StatusPage, error) {
	base, err := validateBaseURL(baseURL)
	if err != nil {
		return StatusPage{}, err
	}
	pageID = strings.TrimSpace(pageID)
	period = strings.TrimSpace(period)
	if pageID == "" || period == "" {
		return StatusPage{}, errors.New("Page ID 和统计周期不能为空")
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	var config configResponse
	var monitors monitorResponse
	var configErr, monitorErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		configErr = c.getJSON(requestCtx, endpoint(base, "config", pageID, ""), &config)
	}()
	go func() {
		defer wg.Done()
		monitorErr = c.getJSON(requestCtx, endpoint(base, "monitor", pageID, period), &monitors)
	}()
	wg.Wait()
	if configErr != nil {
		return StatusPage{}, fmt.Errorf("读取状态页配置: %w", configErr)
	}
	if monitorErr != nil {
		return StatusPage{}, fmt.Errorf("读取监控数据: %w", monitorErr)
	}
	if monitors.Data.Heartbeats == nil {
		return StatusPage{}, errors.New("监控 API 未返回心跳数据")
	}

	heartbeats := make(map[int][]Heartbeat, len(monitors.Data.Heartbeats))
	for _, group := range monitors.Groups {
		for _, monitor := range group.Monitors {
			items, ok := monitors.Data.Heartbeats[fmt.Sprint(monitor.ID)]
			if !ok {
				return StatusPage{}, fmt.Errorf("监控 %q 缺少心跳数据", monitor.Name)
			}
			heartbeats[monitor.ID] = items
		}
	}
	timestamp := monitors.Timestamp
	if timestamp == 0 {
		timestamp = config.Timestamp
	}
	description := ""
	if config.Config.Description != nil {
		description = *config.Config.Description
	}
	resolvedPeriod := strings.TrimSpace(monitors.Period)
	if resolvedPeriod == "" {
		resolvedPeriod = period
	}
	return StatusPage{
		Title: config.Config.Title, Description: description, MatrixStatus: config.MatrixStatus,
		Groups: monitors.Groups, Heartbeats: heartbeats, Uptime: monitors.Data.Uptime,
		Period: resolvedPeriod, Timestamp: timestamp,
	}, nil
}

func (c Client) getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes {
		return errors.New("响应超过 2 MiB")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("上游返回了无效 JSON")
	}
	return nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (c Client) timeout() time.Duration {
	if c.Timeout > 0 && c.Timeout <= defaultTimeout {
		return c.Timeout
	}
	return defaultTimeout
}

func validateBaseURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return nil, errors.New("状态图数据源必须是完整的 HTTP/HTTPS URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("状态图数据源只支持 HTTP/HTTPS")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func endpoint(base *url.URL, resource, pageID, period string) string {
	u := *base
	u.Path = strings.TrimRight(u.Path, "/") + "/api/" + resource
	query := url.Values{"pageId": []string{pageID}}
	if period != "" {
		query.Set("period", period)
	}
	u.RawQuery = query.Encode()
	return u.String()
}
