package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr            string
	QQBotAppID          string
	QQBotAppSecret      string
	QQBotAPIBaseURL     string
	QQBotTokenURL       string
	DataPath            string
	AllowedGroups       []string
	Commands            []string
	StatusURL           string
	StatusPageID        string
	StatusPeriod        string
	ScreenshotTimeout   time.Duration
	ScreenshotQueueSize int
	GGAPIBalanceEnabled bool
	GGAPIBaseURL        string
	GGAPIAdminToken     string
	GGAPISmtpHost       string
	GGAPISmtpPort       int
	GGAPISmtpUsername   string
	GGAPISmtpPassword   string
	GGAPISmtpFrom       string
	GGAPISmtpFromName   string
	GGAPISmtpTLSMode    string
}

func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:            value(getenv, "HTTP_ADDR", "0.0.0.0:8090"),
		QQBotAppID:          strings.TrimSpace(getenv("QQBOT_APP_ID")),
		QQBotAppSecret:      strings.TrimSpace(getenv("QQBOT_APP_SECRET")),
		QQBotAPIBaseURL:     value(getenv, "QQBOT_API_BASE_URL", "https://api.bot.qq.com"),
		QQBotTokenURL:       value(getenv, "QQBOT_TOKEN_URL", "https://bots.qq.com/app/getAppAccessToken"),
		AllowedGroups:       splitList(getenv("QQBOT_ALLOWED_GROUPS")),
		Commands:            splitList(value(getenv, "STATUS_COMMANDS", "状态,status")),
		StatusURL:           value(getenv, "STATUS_URL", "https://status.ggapi.cc"),
		StatusPageID:        value(getenv, "STATUS_PAGE_ID", "default"),
		StatusPeriod:        value(getenv, "STATUS_PERIOD", "1y"),
		ScreenshotTimeout:   durationValue(getenv, "SCREENSHOT_TIMEOUT", 90*time.Second),
		ScreenshotQueueSize: intValue(getenv, "SCREENSHOT_QUEUE_SIZE", 3),
		GGAPIBalanceEnabled: boolValue(getenv, "GGAPI_BALANCE_ENABLED", false),
		GGAPIBaseURL:        value(getenv, "GGAPI_BASE_URL", "https://www.ggapi.cc"),
		GGAPIAdminToken:     strings.TrimSpace(getenv("GGAPI_ADMIN_TOKEN")),
		GGAPISmtpHost:       strings.TrimSpace(getenv("GGAPI_SMTP_HOST")),
		GGAPISmtpPort:       intValue(getenv, "GGAPI_SMTP_PORT", 587),
		GGAPISmtpUsername:   strings.TrimSpace(getenv("GGAPI_SMTP_USERNAME")),
		GGAPISmtpPassword:   strings.TrimSpace(getenv("GGAPI_SMTP_PASSWORD")),
		GGAPISmtpFrom:       strings.TrimSpace(getenv("GGAPI_SMTP_FROM")),
		GGAPISmtpFromName:   strings.TrimSpace(getenv("GGAPI_SMTP_FROM_NAME")),
		GGAPISmtpTLSMode:    strings.ToLower(value(getenv, "GGAPI_SMTP_TLS_MODE", "starttls")),
	}
	cfg.DataPath = value(getenv, "DATA_PATH", "/app/data/qq-status.json")
	if len(cfg.Commands) == 0 {
		cfg.Commands = []string{"状态"}
	}
	if raw := strings.TrimSpace(getenv("GGAPI_BALANCE_ENABLED")); raw != "" {
		if _, err := strconv.ParseBool(raw); err != nil {
			return Config{}, errors.New("GGAPI_BALANCE_ENABLED 必须是 true 或 false")
		}
	}
	if strings.TrimSpace(cfg.StatusPageID) == "" {
		return Config{}, errors.New("STATUS_PAGE_ID 不能为空")
	}
	if strings.TrimSpace(cfg.StatusPeriod) == "" {
		return Config{}, errors.New("STATUS_PERIOD 不能为空")
	}
	for name, rawURL := range map[string]string{
		"QQBOT_API_BASE_URL": cfg.QQBotAPIBaseURL,
		"QQBOT_TOKEN_URL":    cfg.QQBotTokenURL,
		"STATUS_URL":         cfg.StatusURL,
	} {
		if err := validateHTTPURL(rawURL, true); err != nil {
			return Config{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	if cfg.ScreenshotTimeout < 15*time.Second || cfg.ScreenshotTimeout > 4*time.Minute {
		return Config{}, errors.New("SCREENSHOT_TIMEOUT 必须在 15 秒到 4 分钟之间")
	}
	if cfg.ScreenshotQueueSize < 1 || cfg.ScreenshotQueueSize > 20 {
		return Config{}, errors.New("SCREENSHOT_QUEUE_SIZE 必须在 1 到 20 之间")
	}
	if cfg.GGAPIBalanceEnabled {
		if err := validateHTTPSURL(cfg.GGAPIBaseURL); err != nil {
			return Config{}, fmt.Errorf("GGAPI_BASE_URL: %w", err)
		}
		if cfg.GGAPIAdminToken == "" {
			return Config{}, errors.New("GGAPI_ADMIN_TOKEN 不能为空")
		}
		if cfg.GGAPISmtpHost == "" || cfg.GGAPISmtpUsername == "" || cfg.GGAPISmtpPassword == "" || cfg.GGAPISmtpFrom == "" {
			return Config{}, errors.New("GGAPI SMTP 配置不完整")
		}
		if _, err := mail.ParseAddress(cfg.GGAPISmtpFrom); err != nil {
			return Config{}, errors.New("GGAPI_SMTP_FROM 格式无效")
		}
		if cfg.GGAPISmtpPort < 1 || cfg.GGAPISmtpPort > 65535 {
			return Config{}, errors.New("GGAPI_SMTP_PORT 必须在 1 到 65535 之间")
		}
		if cfg.GGAPISmtpTLSMode != "starttls" && cfg.GGAPISmtpTLSMode != "implicit_tls" && cfg.GGAPISmtpTLSMode != "implicit-tls" && cfg.GGAPISmtpTLSMode != "tls" {
			return Config{}, errors.New("GGAPI_SMTP_TLS_MODE 只支持 starttls 或 implicit_tls")
		}
	}
	return cfg, nil
}

func value(getenv func(string) string, key, fallback string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitList(raw string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func intValue(getenv func(string) string, key string, fallback int) int {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}

func boolValue(getenv func(string) string, key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(getenv(key)))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return parsed
}

func durationValue(getenv func(string) string, key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return -1
	}
	return d
}

func validateHTTPURL(rawURL string, allowHTTPS bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return errors.New("必须是完整的 HTTP URL")
	}
	if parsed.Scheme != "http" && (!allowHTTPS || parsed.Scheme != "https") {
		return errors.New("URL 协议不受支持")
	}
	return nil
}

func validateHTTPSURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" || parsed.User != nil {
		return errors.New("必须是完整的 HTTPS URL")
	}
	return nil
}
