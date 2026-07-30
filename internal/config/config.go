package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultScreenshotSelector = "main > div:not(.sticky)"

type Config struct {
	HTTPAddr            string
	QQBotAppID          string
	QQBotAppSecret      string
	QQBotAPIBaseURL     string
	QQBotTokenURL       string
	DataPath            string
	AllowedGroups       []string
	Commands            []string
	BrowserDebugURL     string
	BrowserHostHeader   string
	StatusURL           string
	ScreenshotSelector  string
	ScreenshotWidth     int
	ScreenshotHeight    int
	ScreenshotWait      time.Duration
	ScreenshotTimeout   time.Duration
	ScreenshotQueueSize int
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
		BrowserDebugURL:     value(getenv, "BROWSER_DEBUG_URL", "http://127.0.0.1:9222"),
		BrowserHostHeader:   strings.TrimSpace(getenv("BROWSER_DEBUG_HOST_HEADER")),
		StatusURL:           value(getenv, "STATUS_URL", "https://status.ggapi.cc"),
		ScreenshotSelector:  value(getenv, "SCREENSHOT_SELECTOR", defaultScreenshotSelector),
		ScreenshotWidth:     intValue(getenv, "SCREENSHOT_WIDTH", 1280),
		ScreenshotHeight:    intValue(getenv, "SCREENSHOT_HEIGHT", 900),
		ScreenshotWait:      durationValue(getenv, "SCREENSHOT_WAIT", 5*time.Second),
		ScreenshotTimeout:   durationValue(getenv, "SCREENSHOT_TIMEOUT", 90*time.Second),
		ScreenshotQueueSize: intValue(getenv, "SCREENSHOT_QUEUE_SIZE", 3),
	}
	cfg.DataPath = value(getenv, "DATA_PATH", "/app/data/qq-status.json")
	if len(cfg.Commands) == 0 {
		cfg.Commands = []string{"状态"}
	}
	if strings.TrimSpace(cfg.ScreenshotSelector) == "" {
		return Config{}, errors.New("SCREENSHOT_SELECTOR 不能为空")
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
	if err := validateHTTPURL(cfg.BrowserDebugURL, false); err != nil {
		return Config{}, fmt.Errorf("BROWSER_DEBUG_URL: %w", err)
	}
	if cfg.ScreenshotWidth < 640 || cfg.ScreenshotWidth > 1920 {
		return Config{}, errors.New("SCREENSHOT_WIDTH 必须在 640 到 1920 之间")
	}
	if cfg.ScreenshotHeight < 480 || cfg.ScreenshotHeight > 2160 {
		return Config{}, errors.New("SCREENSHOT_HEIGHT 必须在 480 到 2160 之间")
	}
	if cfg.ScreenshotWait < 0 || cfg.ScreenshotWait > 30*time.Second {
		return Config{}, errors.New("SCREENSHOT_WAIT 必须在 0 到 30 秒之间")
	}
	if cfg.ScreenshotTimeout < 15*time.Second || cfg.ScreenshotTimeout > 4*time.Minute {
		return Config{}, errors.New("SCREENSHOT_TIMEOUT 必须在 15 秒到 4 分钟之间")
	}
	if cfg.ScreenshotQueueSize < 1 || cfg.ScreenshotQueueSize > 20 {
		return Config{}, errors.New("SCREENSHOT_QUEUE_SIZE 必须在 1 到 20 之间")
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
