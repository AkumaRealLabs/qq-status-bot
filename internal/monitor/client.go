package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	HTTP      *http.Client
	ProbeMode string
	CodexPath string
}

const (
	ProbeModeHTTP = "http"
	ProbeModeCLI  = "cli"

	StatusOperational = "operational"
	StatusDegraded    = "degraded"
	StatusFailed      = "failed"
	StatusError       = "error"

	codexAPIKeyEnv = "AUM_CODEX_API_KEY"
	probeInput     = "ping"
)

var degradedAfter = 6 * time.Second

type upstreamGroup struct {
	ID          string
	Name        string
	Description string
	Ratio       string
}

func (c Client) Check(ctx context.Context, u *Upstream, probeModel, selectedKey string) (CheckResult, error) {
	var out CheckResult
	var err error

	switch u.Type {
	case "newapi":
		out.Balance, err = c.newapiBalance(ctx, u)
		if err != nil {
			return out, err
		}
		out.Keys, err = c.newapiKeys(ctx, u)
	case "sub2api":
		if err = c.sub2apiAuth(ctx, u); err != nil {
			return out, err
		}
		out.Balance, err = c.sub2apiBalance(ctx, u)
		if IsAuthError(err) {
			if err = c.sub2apiForceAuth(ctx, u); err != nil {
				return out, err
			}
			out.Balance, err = c.sub2apiBalance(ctx, u)
		}
		if err != nil {
			return out, err
		}
		out.Keys, err = c.sub2apiKeys(ctx, u)
		if IsAuthError(err) {
			if err = c.sub2apiForceAuth(ctx, u); err != nil {
				return out, err
			}
			out.Keys, err = c.sub2apiKeys(ctx, u)
		}
	default:
		err = fmt.Errorf("unsupported upstream type %q", u.Type)
	}
	if err != nil {
		return out, err
	}
	if selectedKey == "" || probeModel == "" {
		return out, nil
	}
	out.Probe = c.Probe(ctx, u.BaseURL, selectedKey, probeModel)
	return out, nil
}

func (c Client) newapiBalance(ctx context.Context, u *Upstream) (Balance, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/user/self"), nil, newapiHeaders(u), &raw); err != nil {
		return Balance{}, markAuth(err)
	}
	data := obj(raw["data"])
	if len(data) == 0 {
		data = raw
	}
	return Balance{
		Balance:  num(first(data, "quota", "balance", "remain_quota", "remain")),
		Used:     num(first(data, "used_quota", "used", "used_amount")),
		Remain:   num(first(data, "remain_quota", "remain", "quota", "balance")),
		Requests: int(num(first(data, "request_count", "requests", "used_count"))),
	}, nil
}

func (c Client) newapiKeys(ctx context.Context, u *Upstream) ([]APIKey, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/token/"), nil, newapiHeaders(u), &raw); err != nil {
		return nil, markAuth(err)
	}
	groups := c.newapiGroups(ctx, u)
	items := array(first(raw, "data", "tokens", "items"))
	keys := make([]APIKey, 0, len(items))
	for _, item := range items {
		m := obj(item)
		id := str(first(m, "id", "token_id"))
		key := str(first(m, "key"))
		groupName := str(first(m, "group"))
		group := groups[groupName]
		if group.Name == "" {
			group.Name = groupName
		}
		if id != "" && (key == "" || strings.Contains(key, "*")) {
			var full map[string]any
			if err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/token/"+url.PathEscape(id)+"/key"), nil, newapiHeaders(u), &full); err != nil {
				return nil, markAuth(err)
			}
			key = str(first(full, "key", "data"))
			if key == "" {
				key = str(first(obj(full["data"]), "key"))
			}
		}
		keys = append(keys, APIKey{
			RemoteID:    id,
			Name:        str(first(m, "name", "token_name")),
			Key:         key,
			Status:      str(first(m, "status")),
			Description: keyDescription(m, group),
			Group:       group.Name,
			GroupRatio:  group.Ratio,
			Quota:       num(first(m, "quota", "remain_quota")),
			UsedQuota:   num(first(m, "used_quota", "used")),
		})
	}
	return keys, nil
}

func (c Client) newapiGroups(ctx context.Context, u *Upstream) map[string]upstreamGroup {
	if groups, err := c.fetchNewapiGroups(ctx, u, "/api/user/self/groups"); err == nil {
		return groups
	}
	if groups, err := c.fetchNewapiGroups(ctx, u, "/api/user/groups"); err == nil {
		return groups
	}
	return nil
}

func (c Client) fetchNewapiGroups(ctx context.Context, u *Upstream, path string) (map[string]upstreamGroup, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, path), nil, newapiHeaders(u), &raw); err != nil {
		return nil, err
	}
	data := obj(raw["data"])
	groups := make(map[string]upstreamGroup, len(data))
	for name, value := range data {
		group := groupFromMap(obj(value))
		group.Name = name
		if group.Description == "" {
			group.Description = group.Name
		}
		groups[name] = group
	}
	return groups, nil
}

func (c Client) sub2apiAuth(ctx context.Context, u *Upstream) error {
	if u.Sub2APIAccessToken != "" {
		return nil
	}
	return c.sub2apiForceAuth(ctx, u)
}

func (c Client) sub2apiForceAuth(ctx context.Context, u *Upstream) error {
	if u.Sub2APIRefreshToken != "" {
		var raw map[string]any
		err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/v1/auth/refresh"), map[string]string{
			"refresh_token": u.Sub2APIRefreshToken,
		}, nil, &raw)
		if err == nil && applySub2Tokens(u, raw) {
			return nil
		}
	}

	var raw map[string]any
	err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/v1/auth/login"), map[string]string{
		"email":    u.Email,
		"password": u.Password,
	}, nil, &raw)
	if err != nil {
		return AuthError{Err: err}
	}
	if !applySub2Tokens(u, raw) {
		return AuthError{Err: errors.New("sub2api login did not return access token")}
	}
	return nil
}

func (c Client) sub2apiBalance(ctx context.Context, u *Upstream) (Balance, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/user/profile"), nil, bearer(u.Sub2APIAccessToken), &raw); err != nil {
		return Balance{}, markAuth(err)
	}
	data := obj(raw["data"])
	if len(data) == 0 {
		data = raw
	}
	return Balance{
		Balance:  num(first(data, "balance", "quota", "remaining_quota")),
		Used:     num(first(data, "used", "used_quota")),
		Remain:   num(first(data, "remaining_quota", "remain", "balance")),
		Requests: int(num(first(data, "request_count", "requests"))),
	}, nil
}

func (c Client) sub2apiKeys(ctx context.Context, u *Upstream) ([]APIKey, error) {
	var raw map[string]any
	err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/api-keys?page_size=200"), nil, bearer(u.Sub2APIAccessToken), &raw)
	if err != nil {
		raw = nil
		if fallbackErr := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/keys"), nil, bearer(u.Sub2APIAccessToken), &raw); fallbackErr != nil {
			return nil, markAuth(err)
		}
	}
	groups := c.sub2apiGroups(ctx, u)
	items := array(first(raw, "data", "keys", "items"))
	keys := make([]APIKey, 0, len(items))
	for _, item := range items {
		m := obj(item)
		group := groupFromMap(obj(first(m, "group")))
		groupID := str(first(m, "group_id", "groupId"))
		if group.Name == "" && groupID != "" {
			group = groups[groupID]
		}
		keys = append(keys, APIKey{
			RemoteID:    str(first(m, "id")),
			Name:        str(first(m, "name")),
			Key:         str(first(m, "key", "api_key")),
			Status:      str(first(m, "status")),
			Description: keyDescription(m, group),
			Group:       group.Name,
			GroupRatio:  group.Ratio,
			Quota:       num(first(m, "quota", "remaining_quota")),
			UsedQuota:   num(first(m, "used_quota", "used")),
		})
	}
	return keys, nil
}

func (c Client) sub2apiGroups(ctx context.Context, u *Upstream) map[string]upstreamGroup {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/groups/available"), nil, bearer(u.Sub2APIAccessToken), &raw); err != nil {
		return nil
	}
	items := array(first(raw, "data", "groups", "items"))
	groups := make(map[string]upstreamGroup, len(items)*2)
	for _, item := range items {
		group := groupFromMap(obj(item))
		if group.ID != "" {
			groups[group.ID] = group
		}
		if group.Name != "" {
			groups[group.Name] = group
		}
	}
	return groups
}

func (c Client) Probe(ctx context.Context, baseURL, key, model string) ProbeResult {
	if strings.EqualFold(c.ProbeMode, ProbeModeCLI) {
		return c.probeCodexCLI(ctx, baseURL, key, model)
	}
	return c.probeHTTP(ctx, baseURL, key, model)
}

func IsInternalProbeError(errText string) bool {
	lower := strings.ToLower(errText)
	for _, needle := range []string{
		"model instructions file is empty",
		"approval_policy",
		"exec: \"codex\"",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func (c Client) probeHTTP(ctx context.Context, baseURL, key, model string) ProbeResult {
	start := time.Now()
	var raw map[string]any
	err := c.doJSON(ctx, http.MethodPost, joinURL(baseURL, "/v1/responses"), map[string]any{
		"model": model,
		"input": []map[string]any{{
			"role": "user",
			"content": []map[string]string{{
				"type": "input_text",
				"text": probeInput,
			}},
		}},
		"max_output_tokens": 2,
		"stream":            false,
	}, bearer(key), &raw)
	latency := time.Since(start)
	if err != nil {
		var httpErr httpStatusError
		if errors.As(err, &httpErr) {
			return ProbeResult{HTTPStatus: httpErr.Status, Latency: latency, Status: StatusFailed, Input: probeInput, Error: httpErr.Error()}
		}
		return ProbeResult{Latency: latency, Status: StatusError, Input: probeInput, Error: err.Error()}
	}
	return probeResultFromOutput(responseText(raw), http.StatusOK, latency)
}

func (c Client) probeCodexCLI(ctx context.Context, baseURL, key, model string) ProbeResult {
	start := time.Now()
	dir, err := os.MkdirTemp("", "aum-codex-probe-*")
	if err != nil {
		return ProbeResult{Latency: time.Since(start), Status: StatusError, Input: probeInput, Error: err.Error()}
	}
	defer os.RemoveAll(dir)

	codexHome := filepath.Join(dir, "codex-home")
	workDir := filepath.Join(dir, "work")
	homeDir := filepath.Join(dir, "home")
	for _, path := range []string{codexHome, workDir, homeDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return ProbeResult{Latency: time.Since(start), Status: StatusError, Input: probeInput, Error: err.Error()}
		}
	}

	instructionsPath := filepath.Join(codexHome, "instructions.txt")
	if err := os.WriteFile(instructionsPath, []byte("Answer briefly.\n"), 0600); err != nil {
		return ProbeResult{Latency: time.Since(start), Status: StatusError, Input: probeInput, Error: err.Error()}
	}
	config := fmt.Sprintf(`model_provider = "aum_card"
model = %q
model_instructions_file = %q
disable_response_storage = true
project_doc_max_bytes = 0
web_search = "disabled"
model_reasoning_effort = "low"
model_verbosity = "low"
model_reasoning_summary = "none"

[shell_environment_policy]
inherit = "none"

[model_providers.aum_card]
name = "AUM Card"
base_url = %q
wire_api = "responses"
env_key = %q
`, model, instructionsPath, codexProviderBaseURL(baseURL), codexAPIKeyEnv)
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0600); err != nil {
		return ProbeResult{Latency: time.Since(start), Status: StatusError, Input: probeInput, Error: err.Error()}
	}

	answerPath := filepath.Join(dir, "answer.txt")
	codex := c.CodexPath
	if codex == "" {
		codex = "codex"
	}
	cmd := exec.CommandContext(ctx, codex,
		"exec",
		"-c", `approval_policy="never"`,
		"-m", model,
		"--skip-git-repo-check",
		"--ephemeral",
		"--ignore-rules",
		"--sandbox", "read-only",
		"-C", workDir,
		"-o", answerPath,
		probeInput,
	)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"CODEX_HOME="+codexHome,
		"HOME="+homeDir,
		codexAPIKeyEnv+"="+key,
	)
	configureProbeCommand(cmd)
	output, err := cmd.CombinedOutput()
	latency := time.Since(start)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ProbeResult{Latency: latency, Status: StatusFailed, Input: probeInput, Error: "Codex CLI 探测超时"}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return ProbeResult{Latency: latency, Status: StatusError, Input: probeInput, Error: "Codex CLI 探测已取消"}
		}
		return ProbeResult{Latency: latency, Status: StatusError, Input: probeInput, Error: codexCLIError(err, output, key)}
	}
	answer, err := os.ReadFile(answerPath)
	if err != nil {
		return ProbeResult{Latency: latency, Status: StatusError, Input: probeInput, Error: err.Error()}
	}
	return probeResultFromOutput(strings.TrimSpace(string(answer)), 0, latency)
}

func codexCLIError(err error, output []byte, key string) string {
	text := string(output)
	if key != "" {
		text = strings.ReplaceAll(text, key, "[redacted]")
	}
	if IsInternalProbeError(text) {
		return limitText(err.Error()+": "+strings.TrimSpace(text), 2000)
	}
	if msg := upstreamErrorMessage(text); msg != "" {
		return err.Error() + ": " + msg
	}
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if line == "" || strings.HasPrefix(lower, "warning:") || strings.HasPrefix(lower, "tip:") || strings.HasPrefix(lower, "usage:") || strings.HasPrefix(lower, "for more information") {
			continue
		}
		if strings.Contains(lower, "error") && (len(lines) == 0 || lines[len(lines)-1] != line) {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		return limitText(err.Error()+": "+lines[len(lines)-1], 2000)
	}
	return err.Error()
}

func upstreamErrorMessage(text string) string {
	for i, r := range text {
		if r != '{' {
			continue
		}
		var raw map[string]any
		if err := json.NewDecoder(strings.NewReader(text[i:])).Decode(&raw); err != nil {
			continue
		}
		return strings.TrimSpace(firstErrorMessage(raw))
	}
	return ""
}

func firstErrorMessage(raw map[string]any) string {
	if msg := strings.TrimSpace(str(first(raw, "message", "msg", "detail"))); msg != "" {
		return msg
	}
	if errText := strings.TrimSpace(str(raw["error"])); errText != "" {
		return errText
	}
	if errObj := obj(raw["error"]); len(errObj) != 0 {
		return firstErrorMessage(errObj)
	}
	return ""
}

func limitText(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func probeResultFromOutput(output string, httpStatus int, latency time.Duration) ProbeResult {
	if output == "" {
		return ProbeResult{HTTPStatus: httpStatus, Latency: latency, Status: StatusFailed, Input: probeInput, Error: "回复为空"}
	}
	status := StatusOperational
	if latency > degradedAfter {
		status = StatusDegraded
	}
	return ProbeResult{HTTPStatus: httpStatus, Latency: latency, Status: status, Input: probeInput, Output: output, Success: true}
}

func codexProviderBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return joinURL(baseURL, "/v1")
}

func responseText(raw map[string]any) string {
	if text := strings.TrimSpace(str(raw["output_text"])); text != "" {
		return text
	}
	var parts []string
	for _, item := range array(raw["output"]) {
		m := obj(item)
		if text := strings.TrimSpace(str(m["text"])); text != "" {
			parts = append(parts, text)
		}
		for _, content := range array(m["content"]) {
			if text := strings.TrimSpace(str(obj(content)["text"])); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (c Client) doJSON(ctx context.Context, method, rawURL string, body any, headers map[string]string, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpStatusError{Status: resp.StatusCode, Message: strings.TrimSpace(string(b))}
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	if strings.HasSuffix(strings.TrimRight(rawURL, "/"), "/v1/responses") && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		if m, ok := out.(*map[string]any); ok {
			text := responseTextFromSSE(b)
			if text == "" {
				return httpStatusError{Status: resp.StatusCode, Message: strings.TrimSpace(string(b))}
			}
			*m = map[string]any{"output_text": text}
			return nil
		}
	}
	if err := json.Unmarshal(b, out); err != nil {
		if msg := strings.TrimSpace(string(b)); msg != "" {
			return httpStatusError{Status: resp.StatusCode, Message: msg}
		}
		return err
	}
	return nil
}

func responseTextFromSSE(body []byte) string {
	var delta strings.Builder
	final := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}
		if text := str(raw["delta"]); text != "" {
			delta.WriteString(text)
		}
		if text := strings.TrimSpace(str(raw["text"])); text != "" {
			final = text
		}
		if text := strings.TrimSpace(str(obj(raw["part"])["text"])); text != "" {
			final = text
		}
		if text := responseText(obj(raw["item"])); text != "" {
			final = text
		}
		if text := responseText(obj(raw["response"])); text != "" {
			final = text
		}
	}
	if final != "" {
		return final
	}
	return strings.TrimSpace(delta.String())
}

type httpStatusError struct {
	Status  int
	Message string
}

func (e httpStatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("http %d: %s", e.Status, http.StatusText(e.Status))
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Message)
}

type AuthError struct {
	Err error
}

func (e AuthError) Error() string { return e.Err.Error() }
func (e AuthError) Unwrap() error { return e.Err }

func IsAuthError(err error) bool {
	var ae AuthError
	return errors.As(err, &ae)
}

func markAuth(err error) error {
	var he httpStatusError
	if errors.As(err, &he) && (he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden) {
		return AuthError{Err: err}
	}
	return err
}

func newapiHeaders(u *Upstream) map[string]string {
	return map[string]string{
		"Authorization": u.AccessToken,
		"New-Api-User":  u.UserID,
	}
}

func bearer(token string) map[string]string {
	if token == "" {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return map[string]string{"Authorization": token}
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func applySub2Tokens(u *Upstream, raw map[string]any) bool {
	data := obj(raw["data"])
	if len(data) == 0 {
		data = raw
	}
	access := str(first(data, "access_token", "accessToken", "token"))
	if access == "" {
		return false
	}
	u.Sub2APIAccessToken = access
	if refresh := str(first(data, "refresh_token", "refreshToken")); refresh != "" {
		u.Sub2APIRefreshToken = refresh
	}
	return true
}

func joinURL(base, path string) string { return strings.TrimRight(base, "/") + path }

func first(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func groupFromMap(m map[string]any) upstreamGroup {
	if len(m) == 0 {
		return upstreamGroup{}
	}
	return upstreamGroup{
		ID:          str(first(m, "id", "group_id", "groupId")),
		Name:        str(first(m, "name", "group", "group_name", "groupName")),
		Description: str(first(m, "description", "desc", "remark", "memo", "note")),
		Ratio:       ratioText(first(m, "rate_multiplier", "rateMultiplier", "ratio", "group_ratio", "groupRatio")),
	}
}

func keyDescription(m map[string]any, group upstreamGroup) string {
	if desc := str(first(m, "description", "desc", "remark", "memo", "note")); desc != "" {
		return desc
	}
	if group.Description != "" {
		return group.Description
	}
	return group.Name
}

func ratioText(v any) string {
	switch vv := v.(type) {
	case string:
		return strings.TrimSuffix(strings.TrimSpace(vv), "x")
	case float64:
		return strconv.FormatFloat(vv, 'f', -1, 64)
	case int:
		return strconv.Itoa(vv)
	case int64:
		return strconv.FormatInt(vv, 10)
	case json.Number:
		return vv.String()
	default:
		return ""
	}
}

func obj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func array(v any) []any {
	switch vv := v.(type) {
	case []any:
		return vv
	case map[string]any:
		return array(first(vv, "items", "records", "list"))
	default:
		return nil
	}
}

func str(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case float64:
		return strconv.FormatFloat(vv, 'f', -1, 64)
	case int:
		return strconv.Itoa(vv)
	case int64:
		return strconv.FormatInt(vv, 10)
	case json.Number:
		return vv.String()
	default:
		return ""
	}
}

func num(v any) float64 {
	switch vv := v.(type) {
	case float64:
		return vv
	case int:
		return float64(vv)
	case json.Number:
		f, _ := vv.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(vv, 64)
		return f
	default:
		return 0
	}
}
