package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type auditResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func shouldAuditRequest(r *http.Request) bool {
	// 敏感备份导出含上游密钥与 Bot Token，虽是 GET 也要留审计；
	// 自检的「最近备份」项也依赖这条记录。
	if r.Method == http.MethodGet && r.URL.Path == "/api/settings/export" {
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
}

func auditBodyFields(r *http.Request) (string, []string) {
	if r.Body == nil || !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "json") {
		return "", nil
	}
	if r.ContentLength > defaultJSONBodyLimit {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, defaultJSONBodyLimit))
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(nil))
		return "", nil
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	if len(b) == 0 {
		return "", nil
	}
	var raw any
	if json.Unmarshal(b, &raw) != nil {
		return "", nil
	}
	seen := map[string]bool{}
	collectJSONFields("", raw, seen)
	fields := make([]string, 0, len(seen))
	for field := range seen {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return string(b), fields
}

func collectJSONFields(prefix string, value any, seen map[string]bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			seen[path] = true
			collectJSONFields(path, child, seen)
		}
	case []any:
		for _, child := range v {
			collectJSONFields(prefix, child, seen)
		}
	}
}

func auditAction(r *http.Request) string {
	pattern := r.Pattern
	if pattern == "" {
		pattern = r.URL.Path
	}
	if strings.HasPrefix(pattern, r.Method+" ") {
		return pattern
	}
	return r.Method + " " + pattern
}

func auditTarget(r *http.Request) (string, string) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	targetType := ""
	if len(parts) >= 2 {
		targetType = parts[1]
	}
	for _, key := range []string{"id", "name", "log_id"} {
		if v := r.PathValue(key); v != "" {
			return targetType, v
		}
	}
	return targetType, ""
}

func auditSummary(r *http.Request, body string) string {
	if body == "" {
		return "no json body"
	}
	var raw any
	if json.Unmarshal([]byte(body), &raw) != nil {
		return "json body"
	}
	return "json fields recorded; values omitted"
}

func sameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	for _, host := range []string{r.Host, firstHeader(r.Header.Get("X-Forwarded-Host"))} {
		if host != "" && strings.EqualFold(u.Host, host) {
			return true
		}
	}
	return false
}

func secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(firstHeader(r.Header.Get("X-Forwarded-Proto")), "https") || strings.EqualFold(r.Header.Get("X-Forwarded-Ssl"), "on")
}

func firstHeader(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
