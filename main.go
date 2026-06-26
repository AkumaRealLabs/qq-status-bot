package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"ai-upstream-monitor/internal/monitor"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"golang.org/x/net/websocket"
)

//go:embed all:frontend/apps/web-antd/dist
var frontendFS embed.FS

const fixedProbeModel = "gpt-5.5"

type appState struct {
	mu        sync.Mutex
	client    monitor.Client
	running   bool
	lastCheck time.Time
}

func main() {
	if len(os.Args) == 1 {
		os.Args = append(os.Args, "serve", "--http", "0.0.0.0:8090")
	}

	app := pocketbase.New()
	distFS, err := fs.Sub(frontendFS, "frontend/apps/web-antd/dist")
	if err != nil {
		log.Fatal(err)
	}
	state := &appState{client: monitor.Client{HTTP: &http.Client{Timeout: 45 * time.Second}}}

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if err := ensureSchema(e.App); err != nil {
			return err
		}
		app.Cron().MustAdd("monitor_check", "* * * * *", func() {
			state.checkDue(app)
		})
		return nil
	})

	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Func: func(e *core.ServeEvent) error {
			api := e.Router.Group("/api/monitor").Bind(apis.RequireSuperuserAuth())
			api.GET("/summary", func(re *core.RequestEvent) error {
				s, err := summary(re.App)
				if err != nil {
					return re.InternalServerError("summary failed", err)
				}
				return re.JSON(http.StatusOK, s)
			})
			api.GET("/status", func(re *core.RequestEvent) error {
				s, err := statusSummary(re.App, re.Request.URL.Query().Get("window"))
				if err != nil {
					return re.InternalServerError("status failed", err)
				}
				return re.JSON(http.StatusOK, s)
			})
			api.GET("/balances", func(re *core.RequestEvent) error {
				s, err := balanceSummary(re.App)
				if err != nil {
					return re.InternalServerError("balances failed", err)
				}
				return re.JSON(http.StatusOK, s)
			})
			api.GET("/upstreams/{id}/keys", func(re *core.RequestEvent) error {
				keys, err := upstreamKeys(re.App, re.Request.PathValue("id"))
				if err != nil {
					return re.InternalServerError("keys failed", err)
				}
				return re.JSON(http.StatusOK, keys)
			})
			api.POST("/cards", func(re *core.RequestEvent) error {
				card, err := saveModelCard(re.App, "", re)
				if err != nil {
					return re.BadRequestError("save card failed", err)
				}
				return re.JSON(http.StatusOK, card)
			})
			api.PATCH("/cards/{id}", func(re *core.RequestEvent) error {
				card, err := saveModelCard(re.App, re.Request.PathValue("id"), re)
				if err != nil {
					return re.BadRequestError("save card failed", err)
				}
				return re.JSON(http.StatusOK, card)
			})
			api.DELETE("/cards/{id}", func(re *core.RequestEvent) error {
				rec, err := re.App.FindRecordById("model_cards", re.Request.PathValue("id"))
				if err != nil {
					return re.NotFoundError("card not found", err)
				}
				if err := re.App.Delete(rec); err != nil {
					return re.InternalServerError("delete card failed", err)
				}
				return re.NoContent(http.StatusNoContent)
			})
			api.POST("/cards/{id}/check", func(re *core.RequestEvent) error {
				if err := state.checkCard(re.App, re.Request.PathValue("id")); err != nil {
					return re.InternalServerError("check card failed", err)
				}
				return re.NoContent(http.StatusNoContent)
			})
			api.POST("/upstreams/{id}/check", func(re *core.RequestEvent) error {
				if err := state.checkOne(re.App, re.Request.PathValue("id")); err != nil {
					return re.InternalServerError("check failed", err)
				}
				return re.NoContent(http.StatusNoContent)
			})
			api.POST("/upstreams/{id}/sync-keys", func(re *core.RequestEvent) error {
				if err := state.syncKeysOnly(re.App, re.Request.PathValue("id")); err != nil {
					return re.InternalServerError("sync keys failed", err)
				}
				return re.NoContent(http.StatusNoContent)
			})
			api.POST("/upstreams/{id}/browser-login", func(re *core.RequestEvent) error {
				info, err := openLoginBrowser(re.App, re.Request.PathValue("id"))
				if err != nil {
					return re.InternalServerError("browser login failed", err)
				}
				return re.JSON(http.StatusOK, info)
			})
			api.POST("/upstreams/{id}/browser-capture", func(re *core.RequestEvent) error {
				info, err := captureBrowserCredentials(re.App, re.Request.PathValue("id"))
				if err != nil {
					return re.InternalServerError("capture credentials failed", err)
				}
				return re.JSON(http.StatusOK, info)
			})
			api.POST("/upstreams/{id}/selected-key", func(re *core.RequestEvent) error {
				var body struct {
					KeyID string `json:"key_id"`
				}
				if err := re.BindBody(&body); err != nil {
					return re.BadRequestError("bad json", err)
				}
				rec, err := re.App.FindRecordById("upstreams", re.Request.PathValue("id"))
				if err != nil {
					return re.NotFoundError("upstream not found", err)
				}
				rec.Set("selected_key", body.KeyID)
				if err := re.App.Save(rec); err != nil {
					return re.InternalServerError("save failed", err)
				}
				return re.NoContent(http.StatusNoContent)
			})
			e.Router.GET("/browser/{path...}", proxyBrowser)
			if !e.Router.HasRoute(http.MethodGet, "/{path...}") {
				e.Router.GET("/{path...}", apis.Static(distFS, true))
			}
			return e.Next()
		},
		Priority: 999,
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func (s *appState) checkAll(app core.App) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	records, err := app.FindRecordsByFilter("upstreams", "enabled=true", "name", 0, 0)
	if err != nil {
		app.Logger().Error("load upstreams failed", "error", err)
		return
	}
	for _, rec := range records {
		if err := s.refreshUpstream(app, rec); err != nil {
			app.Logger().Error("monitor check failed", "upstream", rec.Id, "error", err)
		}
	}
	cards, err := app.FindRecordsByFilter("model_cards", "enabled=true", "name", 0, 0)
	if err != nil {
		app.Logger().Error("load model cards failed", "error", err)
		return
	}
	for _, card := range cards {
		if err := s.checkCardRecord(app, card); err != nil {
			app.Logger().Error("model card check failed", "card", card.Id, "error", err)
		}
	}
}

func (s *appState) checkDue(app core.App) {
	settings, err := loadSettings(app)
	if err != nil {
		app.Logger().Error("load settings failed", "error", err)
		return
	}
	interval := time.Duration(settings.CheckIntervalMinutes) * time.Minute
	s.mu.Lock()
	if !s.lastCheck.IsZero() && time.Since(s.lastCheck) < interval {
		s.mu.Unlock()
		return
	}
	s.lastCheck = time.Now()
	s.mu.Unlock()
	s.checkAll(app)
}

func (s *appState) checkOne(app core.App, id string) error {
	rec, err := app.FindRecordById("upstreams", id)
	if err != nil {
		return err
	}
	return s.refreshUpstream(app, rec)
}

func (s *appState) syncKeysOnly(app core.App, id string) error {
	rec, err := app.FindRecordById("upstreams", id)
	if err != nil {
		return err
	}
	u := upstreamFromRecord(rec)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	result, err := s.client.Check(ctx, &u, "", "")
	if err != nil {
		return err
	}
	if err := persistUpstreamTokens(app, rec, u); err != nil {
		app.Logger().Warn("failed to persist refreshed tokens", "upstream", rec.Id, "error", err)
	}
	return saveKeys(app, rec.Id, result.Keys)
}

func (s *appState) refreshUpstream(app core.App, rec *core.Record) error {
	u := upstreamFromRecord(rec)
	settings, err := loadSettings(app)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := s.client.Check(ctx, &u, fixedProbeModel, "")
	if u.Sub2APIAccessToken != rec.GetString("sub2api_access_token") || u.Sub2APIRefreshToken != rec.GetString("sub2api_refresh_token") {
		if err := persistUpstreamTokens(app, rec, u); err != nil {
			app.Logger().Warn("failed to persist refreshed tokens", "upstream", rec.Id, "error", err)
		}
	}
	if err != nil {
		rec.Set("last_error", err.Error())
		_ = app.Save(rec)
		if monitor.IsAuthError(err) {
			_ = handleAlerts(app, rec, settings, []alertCheck{
				{kind: "credential", known: true, failing: true, msg: rec.GetString("name") + " 凭据失效: " + err.Error()},
			})
		}
		return err
	}

	if err := saveKeys(app, rec.Id, result.Keys); err != nil {
		return err
	}
	if err := saveBalance(app, rec.Id, result.Balance); err != nil {
		return err
	}
	rec.Set("last_error", "")
	if err := app.Save(rec); err != nil {
		return err
	}
	checks := []alertCheck{
		{kind: "balance", known: true, failing: lowBalance(rec, result.Balance), msg: fmt.Sprintf("%s 余额低于阈值", rec.GetString("name"))},
		{kind: "credential", known: true, failing: false, msg: rec.GetString("name") + " 凭据已恢复"},
	}
	return handleAlerts(app, rec, settings, checks)
}

func (s *appState) checkCard(app core.App, id string) error {
	card, err := app.FindRecordById("model_cards", id)
	if err != nil {
		return err
	}
	return s.checkCardRecord(app, card)
}

func (s *appState) checkCardRecord(app core.App, card *core.Record) error {
	upstreamID := card.GetString("upstream")
	keyID := card.GetString("key")
	model := fixedProbeModel
	upstream, err := app.FindRecordById("upstreams", upstreamID)
	if err != nil {
		return err
	}
	var probe monitor.ProbeResult
	if keyID == "" {
		probe = monitor.ProbeResult{Success: false, Error: "未选择 Key"}
	} else {
		key, err := app.FindRecordById("upstream_keys", keyID)
		if err != nil {
			probe = monitor.ProbeResult{Success: false, Error: "Key 不存在"}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			probe = s.client.Probe(ctx, upstream.GetString("base_url"), key.GetString("key"), model)
		}
	}
	if err := saveProbe(app, upstream.Id, card.Id, model, probe); err != nil {
		return err
	}
	if probe.Success {
		card.Set("failure_count", 0)
		card.Set("last_error", "")
	} else if probe.Error != "未选择 Key" {
		card.Set("failure_count", card.GetInt("failure_count")+1)
		card.Set("last_error", probe.Error)
	}
	if err := app.Save(card); err != nil {
		return err
	}
	settings, err := loadSettings(app)
	if err != nil {
		return err
	}
	failing := !probe.Success && probe.Error != "未选择 Key"
	if failing {
		failing = card.GetInt("failure_count") >= 2
	}
	return handleAlerts(app, upstream, settings, []alertCheck{{
		kind:    "ping:" + card.Id,
		known:   probe.Error != "未选择 Key",
		failing: failing,
		msg:     card.GetString("name") + " ping 失败: " + probe.Error,
	}})
}

type settings struct {
	CheckIntervalMinutes int
	TelegramBotToken     string
	TelegramChatID       string
}

func normalizedCheckInterval(minutes int) int {
	if minutes < 1 {
		return 5
	}
	return minutes
}

func loadSettings(app core.App) (settings, error) {
	rec, err := app.FindFirstRecordByFilter("settings", "1=1")
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		col, colErr := app.FindCollectionByNameOrId("settings")
		if colErr != nil {
			return settings{}, colErr
		}
		rec = core.NewRecord(col)
		rec.Set("probe_model", fixedProbeModel)
		rec.Set("check_interval_minutes", 5)
		if saveErr := app.Save(rec); saveErr != nil {
			return settings{}, saveErr
		}
	} else if err != nil {
		return settings{}, err
	}
	interval := normalizedCheckInterval(rec.GetInt("check_interval_minutes"))
	changed := false
	if rec.GetString("probe_model") != fixedProbeModel {
		rec.Set("probe_model", fixedProbeModel)
		changed = true
	}
	if rec.GetInt("check_interval_minutes") != interval {
		rec.Set("check_interval_minutes", interval)
		changed = true
	}
	if changed {
		if err := app.Save(rec); err != nil {
			return settings{}, err
		}
	}
	return settings{
		CheckIntervalMinutes: interval,
		TelegramBotToken:     rec.GetString("telegram_bot_token"),
		TelegramChatID:       rec.GetString("telegram_chat_id"),
	}, nil
}

func upstreamFromRecord(rec *core.Record) monitor.Upstream {
	return monitor.Upstream{
		ID:                  rec.Id,
		Name:                rec.GetString("name"),
		Type:                rec.GetString("type"),
		BaseURL:             rec.GetString("base_url"),
		Enabled:             rec.GetBool("enabled"),
		UserID:              rec.GetString("user_id"),
		AccessToken:         rec.GetString("access_token"),
		Email:               rec.GetString("email"),
		Password:            rec.GetString("password"),
		Sub2APIAccessToken:  rec.GetString("sub2api_access_token"),
		Sub2APIRefreshToken: rec.GetString("sub2api_refresh_token"),
		LowBalanceThreshold: rec.GetFloat("low_balance_threshold"),
		SelectedKeyID:       rec.GetString("selected_key"),
		FailureCount:        rec.GetInt("failure_count"),
	}
}

func saveKeys(app core.App, upstreamID string, keys []monitor.APIKey) error {
	col, err := app.FindCollectionByNameOrId("upstream_keys")
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k.RemoteID == "" && k.Key == "" {
			continue
		}
		remoteID := k.RemoteID
		if remoteID == "" {
			remoteID = k.Key
		}
		rec, err := app.FindFirstRecordByFilter("upstream_keys", "upstream={:upstream} && remote_id={:remote}", dbx.Params{"upstream": upstreamID, "remote": remoteID})
		if err != nil && errors.Is(err, sql.ErrNoRows) {
			rec = core.NewRecord(col)
			rec.Set("upstream", upstreamID)
			rec.Set("remote_id", remoteID)
		} else if err != nil {
			return err
		}
		rec.Set("name", k.Name)
		rec.Set("key", k.Key)
		rec.Set("status", k.Status)
		rec.Set("description", k.Description)
		rec.Set("group", k.Group)
		rec.Set("group_ratio", k.GroupRatio)
		rec.Set("quota", k.Quota)
		rec.Set("used_quota", k.UsedQuota)
		if err := app.Save(rec); err != nil {
			return err
		}
	}
	return nil
}

func saveBalance(app core.App, upstreamID string, b monitor.Balance) error {
	col, err := app.FindCollectionByNameOrId("balance_snapshots")
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("upstream", upstreamID)
	rec.Set("balance", b.Balance)
	rec.Set("used", b.Used)
	rec.Set("remain", b.Remain)
	rec.Set("requests", b.Requests)
	rec.Set("error", "")
	rec.Set("latency_ms", 0)
	rec.Set("checked_at", time.Now().UTC())
	return app.Save(rec)
}

func saveProbe(app core.App, upstreamID, cardID, model string, p monitor.ProbeResult) error {
	col, err := app.FindCollectionByNameOrId("probe_runs")
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("upstream", upstreamID)
	rec.Set("card", cardID)
	rec.Set("model", model)
	rec.Set("input", "ping")
	rec.Set("http_status", p.HTTPStatus)
	rec.Set("latency_ms", p.Latency.Milliseconds())
	rec.Set("success", p.Success)
	rec.Set("error", p.Error)
	rec.Set("checked_at", time.Now().UTC())
	return app.Save(rec)
}

func upstreamKeys(app core.App, upstreamID string) ([]map[string]string, error) {
	keys, err := app.FindRecordsByFilter("upstream_keys", "upstream={:id}", "name", 200, 0, dbx.Params{"id": upstreamID})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, keyRow(k))
	}
	return out, nil
}

func keyRow(k *core.Record) map[string]string {
	return map[string]string{
		"id":          k.Id,
		"name":        k.GetString("name"),
		"status":      k.GetString("status"),
		"description": k.GetString("description"),
		"group":       k.GetString("group"),
		"group_ratio": k.GetString("group_ratio"),
	}
}

func saveModelCard(app core.App, id string, re *core.RequestEvent) (map[string]any, error) {
	var body struct {
		Name     string `json:"name"`
		Upstream string `json:"upstream"`
		Key      string `json:"key"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := re.BindBody(&body); err != nil {
		return nil, err
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Upstream = strings.TrimSpace(body.Upstream)
	body.Key = strings.TrimSpace(body.Key)
	if body.Upstream == "" {
		return nil, errors.New("upstream is required")
	}
	upstream, err := app.FindRecordById("upstreams", body.Upstream)
	if err != nil {
		return nil, err
	}
	var key *core.Record
	if body.Key != "" {
		key, err = app.FindRecordById("upstream_keys", body.Key)
		if err != nil {
			return nil, err
		}
		if key.GetString("upstream") != body.Upstream {
			return nil, errors.New("key does not belong to upstream")
		}
	}
	col, err := app.FindCollectionByNameOrId("model_cards")
	if err != nil {
		return nil, err
	}
	var rec *core.Record
	if id == "" {
		rec = core.NewRecord(col)
		rec.Set("failure_count", 0)
	} else {
		rec, err = app.FindRecordById("model_cards", id)
		if err != nil {
			return nil, err
		}
	}
	rec.Set("name", cardName(upstream, key))
	rec.Set("upstream", body.Upstream)
	rec.Set("key", body.Key)
	rec.Set("model", fixedProbeModel)
	if body.Enabled == nil {
		if id == "" {
			rec.Set("enabled", true)
		}
	} else {
		rec.Set("enabled", *body.Enabled)
	}
	if err := app.Save(rec); err != nil {
		return nil, err
	}
	row, err := modelCardRow(app, rec, nil)
	if err != nil {
		return nil, err
	}
	return row, nil
}

type alertCheck struct {
	kind    string
	known   bool
	failing bool
	msg     string
}

func handleAlerts(app core.App, upstream *core.Record, cfg settings, checks []alertCheck) error {
	for _, c := range checks {
		if !c.known {
			continue
		}
		prev := alertState(app, upstream.Id, c.kind)
		dec, send := monitor.DecideAlert(time.Now(), c.kind, c.failing, c.msg, prev)
		if !send {
			continue
		}
		if dec.Recover {
			dec.Message = upstream.GetString("name") + " " + c.kind + " 已恢复"
		}
		if err := sendTelegram(cfg, dec.Message); err != nil {
			app.Logger().Warn("telegram alert failed", "error", err)
		}
		if err := saveAlert(app, upstream.Id, dec); err != nil {
			return err
		}
	}
	return nil
}

func alertState(app core.App, upstreamID, kind string) monitor.AlertState {
	rec, err := latestRecord(app, "alert_events", "upstream={:upstream} && type={:type}", "-created", dbx.Params{"upstream": upstreamID, "type": kind})
	if err != nil {
		return monitor.AlertState{}
	}
	if rec.GetBool("recover") {
		return monitor.AlertState{}
	}
	return monitor.AlertState{Active: true, LastAt: rec.GetDateTime("created").Time()}
}

func saveAlert(app core.App, upstreamID string, dec monitor.AlertDecision) error {
	col, err := app.FindCollectionByNameOrId("alert_events")
	if err != nil {
		return err
	}
	rec := core.NewRecord(col)
	rec.Set("upstream", upstreamID)
	rec.Set("type", dec.Type)
	rec.Set("recover", dec.Recover)
	rec.Set("message", dec.Message)
	rec.Set("sent", true)
	return app.Save(rec)
}

func sendTelegram(cfg settings, message string) error {
	if cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
		return nil
	}
	form := url.Values{"chat_id": {cfg.TelegramChatID}, "text": {message}}
	resp, err := http.PostForm("https://api.telegram.org/bot"+cfg.TelegramBotToken+"/sendMessage", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram status %d", resp.StatusCode)
	}
	return nil
}

func summary(app core.App) ([]map[string]any, error) {
	upstreams, err := app.FindRecordsByFilter("upstreams", "", "name", 0, 0)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(upstreams))
	for _, u := range upstreams {
		row := map[string]any{
			"id":            u.Id,
			"name":          u.GetString("name"),
			"type":          u.GetString("type"),
			"enabled":       u.GetBool("enabled"),
			"selected_key":  u.GetString("selected_key"),
			"last_error":    u.GetString("last_error"),
			"failure_count": u.GetInt("failure_count"),
		}
		if b, err := latestSnapshot(app, "balance_snapshots", u.Id); err == nil {
			row["balance"] = b.GetFloat("balance")
			row["used"] = b.GetFloat("used")
			row["remain"] = b.GetFloat("remain")
			row["last_check"] = snapshotTime(b)
		}
		if p, err := latestSnapshot(app, "probe_runs", u.Id); err == nil {
			row["ping_success"] = p.GetBool("success")
			row["latency_ms"] = p.GetInt("latency_ms")
			row["http_status"] = p.GetInt("http_status")
			row["probe_error"] = p.GetString("error")
		}
		keys, _ := app.FindRecordsByFilter("upstream_keys", "upstream={:id}", "name", 200, 0, dbx.Params{"id": u.Id})
		keyRows := make([]map[string]string, 0, len(keys))
		for _, k := range keys {
			keyRows = append(keyRows, keyRow(k))
		}
		row["keys"] = keyRows
		out = append(out, row)
	}
	return out, nil
}

func statusSummary(app core.App, window string) (map[string]any, error) {
	since, label := windowSince(window)
	cards, err := app.FindRecordsByFilter("model_cards", "", "name", 0, 0)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(cards))
	totalRequests, totalSuccess, totalFailed, totalLatency, latencyCount := 0, 0, 0, 0, 0
	for _, card := range cards {
		probes, err := app.FindRecordsByFilter(
			"probe_runs",
			"card={:id} && checked_at>={:since}",
			"-checked_at",
			0,
			0,
			dbx.Params{"id": card.Id, "since": since},
		)
		if err != nil {
			return nil, err
		}
		success, failed, latencyTotal, latencySamples := 0, 0, 0, 0
		var latest *core.Record
		if len(probes) > 0 {
			latest = probes[0]
		}
		for _, p := range probes {
			if p.GetBool("success") {
				success++
			} else {
				failed++
			}
			if ms := p.GetInt("latency_ms"); ms > 0 {
				latencyTotal += ms
				latencySamples++
			}
		}
		totalRequests += len(probes)
		totalSuccess += success
		totalFailed += failed
		totalLatency += latencyTotal
		latencyCount += latencySamples
		row, err := modelCardRow(app, card, probes)
		if err != nil {
			return nil, err
		}
		row["requests"] = len(probes)
		row["success"] = success
		row["failed"] = failed
		row["success_rate"] = percent(success, len(probes))
		row["avg_latency"] = avg(latencyTotal, latencySamples)
		if latest != nil {
			row["last_check"] = snapshotTime(latest)
			row["last_success"] = latest.GetBool("success")
			row["last_http_status"] = latest.GetInt("http_status")
			row["last_latency"] = latest.GetInt("latency_ms")
			row["last_probe_error"] = latest.GetString("error")
			row["model"] = latest.GetString("model")
		}
		rows = append(rows, row)
	}
	return map[string]any{
		"window":       label,
		"rows":         rows,
		"requests":     totalRequests,
		"success":      totalSuccess,
		"failed":       totalFailed,
		"success_rate": percent(totalSuccess, totalRequests),
		"avg_latency":  avg(totalLatency, latencyCount),
	}, nil
}

func modelCardRow(app core.App, card *core.Record, probes []*core.Record) (map[string]any, error) {
	upstream, err := app.FindRecordById("upstreams", card.GetString("upstream"))
	if err != nil {
		return nil, err
	}
	keys, _ := upstreamKeys(app, upstream.Id)
	var key *core.Record
	if keyID := card.GetString("key"); keyID != "" {
		if rec, err := app.FindRecordById("upstream_keys", keyID); err == nil {
			key = rec
		}
	}
	history := make([]map[string]any, 0, minInt(len(probes), 60))
	limit := minInt(len(probes), 60)
	for i := limit - 1; i >= 0; i-- {
		p := probes[i]
		history = append(history, map[string]any{
			"success":    p.GetBool("success"),
			"latency_ms": p.GetInt("latency_ms"),
			"checked_at": snapshotTime(p),
		})
	}
	return map[string]any{
		"id":              card.Id,
		"name":            cardName(upstream, key),
		"upstream":        upstream.Id,
		"upstream_name":   upstream.GetString("name"),
		"type":            upstream.GetString("type"),
		"key":             card.GetString("key"),
		"key_name":        keyString(key, "name"),
		"key_description": keyString(key, "description"),
		"key_group":       keyString(key, "group"),
		"key_group_ratio": keyString(key, "group_ratio"),
		"model":           card.GetString("model"),
		"enabled":         card.GetBool("enabled"),
		"last_error":      card.GetString("last_error"),
		"failure_count":   card.GetInt("failure_count"),
		"keys":            keys,
		"history":         history,
	}, nil
}

func cardName(upstream, key *core.Record) string {
	if upstream == nil {
		return ""
	}
	name := upstream.GetString("name")
	if key == nil {
		return name
	}
	keyName := key.GetString("name")
	if keyName == "" {
		keyName = key.GetString("description")
	}
	if keyName == "" {
		keyName = key.Id
	}
	return name + " · " + keyName
}

func keyString(key *core.Record, field string) string {
	if key == nil {
		return ""
	}
	return key.GetString(field)
}

func balanceSummary(app core.App) ([]map[string]any, error) {
	upstreams, err := app.FindRecordsByFilter("upstreams", "", "name", 0, 0)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(upstreams))
	for _, u := range upstreams {
		row := map[string]any{
			"id":                    u.Id,
			"name":                  u.GetString("name"),
			"type":                  u.GetString("type"),
			"enabled":               u.GetBool("enabled"),
			"balance_rate":          balanceRate(u),
			"low_balance_threshold": u.GetFloat("low_balance_threshold"),
		}
		if b, err := latestSnapshot(app, "balance_snapshots", u.Id); err == nil {
			balance, used, remain := displayBalance(u, b)
			row["balance"] = balance
			row["used"] = used
			row["remain"] = remain
			sourceBalance, sourceUsed, sourceRemain := sourceDisplayBalance(u, b)
			row["source_balance"] = sourceBalance
			row["source_used"] = sourceUsed
			row["source_remain"] = sourceRemain
			row["requests"] = b.GetInt("requests")
			row["last_check"] = snapshotTime(b)
			row["error"] = b.GetString("error")
			row["low_balance"] = u.GetFloat("low_balance_threshold") > 0 && remain <= u.GetFloat("low_balance_threshold")
		}
		out = append(out, row)
	}
	return out, nil
}

func displayBalance(upstream, snapshot *core.Record) (float64, float64, float64) {
	return convertedBalanceValues(
		upstream.GetString("type"),
		balanceRate(upstream),
		snapshot.GetFloat("balance"),
		snapshot.GetFloat("used"),
		snapshot.GetFloat("remain"),
	)
}

func sourceDisplayBalance(upstream, snapshot *core.Record) (float64, float64, float64) {
	return normalizedBalanceValues(
		upstream.GetString("type"),
		snapshot.GetFloat("balance"),
		snapshot.GetFloat("used"),
		snapshot.GetFloat("remain"),
	)
}

func lowBalance(upstream *core.Record, balance monitor.Balance) bool {
	threshold := upstream.GetFloat("low_balance_threshold")
	if threshold <= 0 {
		return false
	}
	_, _, remain := convertedBalanceValues(upstream.GetString("type"), balanceRate(upstream), balance.Balance, balance.Used, balance.Remain)
	return remain <= threshold
}

func balanceRate(upstream *core.Record) float64 {
	rate := upstream.GetFloat("balance_rate")
	if rate <= 0 {
		return 1
	}
	return rate
}

func normalizedBalanceValues(upstreamType string, balance, used, remain float64) (float64, float64, float64) {
	if upstreamType == "newapi" {
		balance, used, remain = balance/500000, used/500000, remain/500000
	}
	return balance, used, remain
}

func convertedBalanceValues(upstreamType string, rate, balance, used, remain float64) (float64, float64, float64) {
	balance, used, remain = normalizedBalanceValues(upstreamType, balance, used, remain)
	if rate <= 0 {
		rate = 1
	}
	return balance * rate, used * rate, remain * rate
}

func windowSince(window string) (string, string) {
	windows := map[string]time.Duration{
		"1h":  time.Hour,
		"3h":  3 * time.Hour,
		"5h":  5 * time.Hour,
		"1d":  24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"15d": 15 * 24 * time.Hour,
	}
	if _, ok := windows[window]; !ok {
		window = "1h"
	}
	return time.Now().Add(-windows[window]).UTC().Format("2006-01-02 15:04:05.000Z"), window
}

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func avg(total, count int) int {
	if count == 0 {
		return 0
	}
	return total / count
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func ensureSchema(app core.App) error {
	upstreams, err := ensureCollection(app, "upstreams", nil,
		&core.TextField{Name: "name", Required: true, Max: 200},
		&core.SelectField{Name: "type", Required: true, Values: []string{"newapi", "sub2api"}},
		&core.URLField{Name: "base_url", Required: true},
		&core.BoolField{Name: "enabled"},
		&core.TextField{Name: "user_id", Max: 200},
		&core.TextField{Name: "access_token", Max: 5000},
		&core.EmailField{Name: "email"},
		&core.TextField{Name: "password", Max: 5000},
		&core.TextField{Name: "sub2api_access_token", Max: 5000},
		&core.TextField{Name: "sub2api_refresh_token", Max: 5000},
		&core.NumberField{Name: "balance_rate"},
		&core.NumberField{Name: "low_balance_threshold"},
		&core.TextField{Name: "selected_key", Max: 64},
		&core.TextField{Name: "last_error", Max: 5000},
		&core.NumberField{Name: "failure_count", OnlyInt: true},
	)
	if err != nil {
		return err
	}
	keys, err := ensureCollection(app, "upstream_keys", map[string]string{"idx_upstream_keys_remote": "CREATE UNIQUE INDEX IF NOT EXISTS idx_upstream_keys_remote ON upstream_keys (upstream, remote_id)"},
		&core.RelationField{Name: "upstream", CollectionId: upstreams.Id, Required: true, CascadeDelete: true},
		&core.TextField{Name: "remote_id", Max: 200},
		&core.TextField{Name: "name", Max: 200},
		&core.TextField{Name: "key", Max: 5000},
		&core.TextField{Name: "status", Max: 200},
		&core.TextField{Name: "description", Max: 1000},
		&core.TextField{Name: "group", Max: 200},
		&core.TextField{Name: "group_ratio", Max: 100},
		&core.NumberField{Name: "quota"},
		&core.NumberField{Name: "used_quota"},
	)
	if err != nil {
		return err
	}
	modelCards, err := ensureCollection(app, "model_cards", nil,
		&core.TextField{Name: "name", Required: true, Max: 200},
		&core.RelationField{Name: "upstream", CollectionId: upstreams.Id, Required: true, CascadeDelete: true},
		&core.RelationField{Name: "key", CollectionId: keys.Id, CascadeDelete: true},
		&core.TextField{Name: "model", Required: true, Max: 200},
		&core.BoolField{Name: "enabled"},
		&core.TextField{Name: "last_error", Max: 5000},
		&core.NumberField{Name: "failure_count", OnlyInt: true},
	)
	if err != nil {
		return err
	}
	for name, fields := range map[string][]core.Field{
		"balance_snapshots": {
			&core.RelationField{Name: "upstream", CollectionId: upstreams.Id, Required: true, CascadeDelete: true},
			&core.AutodateField{Name: "checked_at", OnCreate: true},
			&core.NumberField{Name: "balance"}, &core.NumberField{Name: "used"}, &core.NumberField{Name: "remain"},
			&core.NumberField{Name: "requests", OnlyInt: true}, &core.TextField{Name: "error", Max: 5000}, &core.NumberField{Name: "latency_ms", OnlyInt: true},
		},
		"probe_runs": {
			&core.RelationField{Name: "upstream", CollectionId: upstreams.Id, Required: true, CascadeDelete: true},
			&core.RelationField{Name: "card", CollectionId: modelCards.Id, CascadeDelete: true},
			&core.AutodateField{Name: "checked_at", OnCreate: true},
			&core.TextField{Name: "model", Max: 200}, &core.TextField{Name: "input", Max: 200}, &core.NumberField{Name: "http_status", OnlyInt: true},
			&core.NumberField{Name: "latency_ms", OnlyInt: true}, &core.BoolField{Name: "success"}, &core.TextField{Name: "error", Max: 5000},
		},
		"alert_events": {
			&core.RelationField{Name: "upstream", CollectionId: upstreams.Id, Required: true, CascadeDelete: true},
			&core.TextField{Name: "type", Max: 200}, &core.BoolField{Name: "recover"}, &core.BoolField{Name: "sent"}, &core.TextField{Name: "message", Max: 5000},
		},
		"settings": {
			&core.TextField{Name: "probe_model", Max: 200}, &core.NumberField{Name: "check_interval_minutes", OnlyInt: true},
			&core.TextField{Name: "telegram_bot_token", Max: 5000}, &core.TextField{Name: "telegram_chat_id", Max: 200},
		},
	} {
		if _, err := ensureCollection(app, name, nil, fields...); err != nil {
			return err
		}
	}
	_, err = loadSettings(app)
	if err != nil {
		return err
	}
	if err := seedModelCards(app); err != nil {
		return err
	}
	return backfillCheckedAt(app)
}

func ensureCollection(app core.App, name string, indexes map[string]string, fields ...core.Field) (*core.Collection, error) {
	col, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		col = core.NewBaseCollection(name)
	}
	lockSuperuser(col)
	col.Fields.Add(fields...)
	for idxName, idxSQL := range indexes {
		found := false
		for _, idx := range col.Indexes {
			if strings.Contains(idx, idxName) {
				found = true
				break
			}
		}
		if !found {
			col.Indexes = append(col.Indexes, idxSQL)
		}
	}
	if err := app.Save(col); err != nil {
		return nil, err
	}
	return col, nil
}

func latestRecord(app core.App, collection, filter, sort string, params ...dbx.Params) (*core.Record, error) {
	records, err := app.FindRecordsByFilter(collection, filter, sort, 1, 0, params...)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, sql.ErrNoRows
	}
	return records[0], nil
}

func latestSnapshot(app core.App, collection, upstreamID string) (*core.Record, error) {
	return latestRecord(app, collection, "upstream={:id}", "-checked_at", dbx.Params{"id": upstreamID})
}

func snapshotTime(rec *core.Record) string {
	if v := rec.GetDateTime("checked_at").String(); v != "" {
		return v
	}
	return rec.GetString("checked_at")
}

func backfillCheckedAt(app core.App) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05.000Z")
	for _, table := range []string{"balance_snapshots", "probe_runs"} {
		_, err := app.DB().NewQuery("UPDATE " + table + " SET checked_at={:now} WHERE checked_at IS NULL OR checked_at=''").Bind(dbx.Params{"now": now}).Execute()
		if err != nil {
			return err
		}
	}
	return nil
}

func seedModelCards(app core.App) error {
	existing, err := app.FindRecordsByFilter("model_cards", "", "name", 1, 0)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	col, err := app.FindCollectionByNameOrId("model_cards")
	if err != nil {
		return err
	}
	upstreams, err := app.FindRecordsByFilter("upstreams", "", "name", 0, 0)
	if err != nil {
		return err
	}
	for _, u := range upstreams {
		rec := core.NewRecord(col)
		rec.Set("name", u.GetString("name"))
		rec.Set("upstream", u.Id)
		rec.Set("key", u.GetString("selected_key"))
		rec.Set("model", fixedProbeModel)
		rec.Set("enabled", u.GetBool("enabled"))
		if err := app.Save(rec); err != nil {
			return err
		}
	}
	return nil
}

func persistUpstreamTokens(app core.App, rec *core.Record, u monitor.Upstream) error {
	if rec.GetString("sub2api_access_token") == u.Sub2APIAccessToken && rec.GetString("sub2api_refresh_token") == u.Sub2APIRefreshToken {
		return nil
	}
	rec.Set("sub2api_access_token", u.Sub2APIAccessToken)
	rec.Set("sub2api_refresh_token", u.Sub2APIRefreshToken)
	return app.Save(rec)
}

func lockSuperuser(col *core.Collection) {
	col.ListRule = nil
	col.ViewRule = nil
	col.CreateRule = nil
	col.UpdateRule = nil
	col.DeleteRule = nil
}

const (
	defaultBrowserDebugURL = "http://127.0.0.1:19222"
	defaultBrowserProxyURL = "http://127.0.0.1:6080"
	defaultBrowserVNCURL   = "/browser/vnc.html?autoconnect=true&resize=scale"
)

func openLoginBrowser(app core.App, upstreamID string) (map[string]any, error) {
	rec, err := app.FindRecordById("upstreams", upstreamID)
	if err != nil {
		return nil, err
	}
	if err := openBrowserURL(rec.GetString("base_url")); err != nil {
		return nil, err
	}
	return map[string]any{
		"url":     rec.GetString("base_url"),
		"vnc_url": browserVNCURL(),
	}, nil
}

func captureBrowserCredentials(app core.App, upstreamID string) (map[string]any, error) {
	rec, err := app.FindRecordById("upstreams", upstreamID)
	if err != nil {
		return nil, err
	}
	tab, err := browserTab(rec.GetString("base_url"))
	if err != nil {
		return nil, err
	}
	access, refresh, err := readBrowserTokens(tab)
	if err != nil {
		return nil, err
	}
	if access == "" && refresh == "" {
		return nil, errors.New("没有在浏览器 localStorage/sessionStorage/cookie 里找到 token")
	}
	if access != "" {
		rec.Set("sub2api_access_token", access)
	}
	if refresh != "" {
		rec.Set("sub2api_refresh_token", refresh)
	}
	if err := app.Save(rec); err != nil {
		return nil, err
	}
	return map[string]any{"access_token": access != "", "refresh_token": refresh != ""}, nil
}

type debugTab struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func browserTab(baseURL string) (debugTab, error) {
	debugURL := browserDebugURL()
	resp, err := browserDebugDo(http.MethodGet, "/json")
	if err != nil {
		return debugTab{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return debugTab{}, fmt.Errorf("browser devtools status %d", resp.StatusCode)
	}
	var tabs []debugTab
	if err := json.NewDecoder(resp.Body).Decode(&tabs); err != nil {
		return debugTab{}, err
	}
	base, _ := url.Parse(baseURL)
	for _, tab := range tabs {
		u, _ := url.Parse(tab.URL)
		if tab.Type == "page" && tab.WebSocketDebuggerURL != "" && u.Host == base.Host {
			tab.WebSocketDebuggerURL = rewriteWebSocketURL(debugURL, tab.WebSocketDebuggerURL)
			return tab, nil
		}
	}
	return debugTab{}, errors.New("找不到已打开的登录页")
}

func browserDebugURL() string {
	if v := strings.TrimRight(os.Getenv("BROWSER_DEBUG_URL"), "/"); v != "" {
		return v
	}
	return defaultBrowserDebugURL
}

func browserVNCURL() string {
	if v := os.Getenv("BROWSER_VNC_URL"); strings.TrimSpace(v) != "" {
		return v
	}
	return defaultBrowserVNCURL
}

func browserProxyURL() string {
	if v := strings.TrimRight(os.Getenv("BROWSER_PROXY_URL"), "/"); v != "" {
		return v
	}
	return defaultBrowserProxyURL
}

func proxyBrowser(re *core.RequestEvent) error {
	target, err := url.Parse(browserProxyURL())
	if err != nil {
		return re.InternalServerError("bad browser proxy url", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = "/" + strings.TrimPrefix(req.URL.Path, "/browser/")
		req.Host = target.Host
	}
	proxy.ServeHTTP(re.Response, re.Request)
	return nil
}

func openBrowserURL(rawurl string) error {
	resp, err := browserDebugDo(http.MethodPut, "/json/new?"+url.QueryEscape(rawurl))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("browser devtools status %d", resp.StatusCode)
	}
	return nil
}

func browserDebugDo(method, path string) (*http.Response, error) {
	req, err := http.NewRequest(method, browserDebugURL()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Host = browserDebugHostHeader()
	return http.DefaultClient.Do(req)
}

func rewriteWebSocketURL(debugURL, wsURL string) string {
	debug, err := url.Parse(debugURL)
	if err != nil {
		return wsURL
	}
	ws, err := url.Parse(wsURL)
	if err != nil {
		return wsURL
	}
	ws.Host = browserDebugHostHeader()
	if debug.Scheme == "https" {
		ws.Scheme = "wss"
	} else if debug.Scheme == "http" {
		ws.Scheme = "ws"
	}
	return ws.String()
}

func readBrowserTokens(tab debugTab) (string, string, error) {
	ws, err := dialBrowserWebSocket(tab.WebSocketDebuggerURL)
	if err != nil {
		return "", "", err
	}
	defer ws.Close()

	snapshot, err := cdpEval(ws)
	if err != nil {
		return "", "", err
	}
	cookies, err := cdpCookies(ws)
	if err != nil {
		return "", "", err
	}
	for k, v := range cookies {
		snapshot[k] = v
	}
	access, refresh := "", ""
	for k, v := range snapshot {
		access, refresh = pickToken(k, v, access, refresh)
	}
	return access, refresh, nil
}

func dialBrowserWebSocket(location string) (*websocket.Conn, error) {
	config, err := websocket.NewConfig(location, browserDebugOrigin())
	if err != nil {
		return nil, err
	}
	if browserDebugHostHeader() == browserDebugConnectAddress() {
		return websocket.DialConfig(config)
	}
	conn, err := net.DialTimeout("tcp", browserDebugConnectAddress(), 10*time.Second)
	if err != nil {
		return nil, err
	}
	ws, err := websocket.NewClient(config, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ws, nil
}

func browserDebugOrigin() string {
	scheme := "http"
	if debug, err := url.Parse(browserDebugURL()); err == nil && debug.Scheme != "" {
		scheme = debug.Scheme
	}
	return scheme + "://" + browserDebugHostHeader()
}

func browserDebugHostHeader() string {
	if v := strings.TrimSpace(os.Getenv("BROWSER_DEBUG_HOST_HEADER")); v != "" {
		return v
	}
	debug, err := url.Parse(browserDebugURL())
	if err != nil {
		return mustDebugHost(defaultBrowserDebugURL)
	}
	host := debug.Hostname()
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return hostWithDefaultPort(debug.Host, debug.Scheme)
	}
	return mustDebugHost(defaultBrowserDebugURL)
}

func browserDebugConnectAddress() string {
	debug, err := url.Parse(browserDebugURL())
	if err != nil {
		return mustDebugHost(defaultBrowserDebugURL)
	}
	return hostWithDefaultPort(debug.Host, debug.Scheme)
}

func mustDebugHost(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "127.0.0.1:19222"
	}
	return hostWithDefaultPort(u.Host, u.Scheme)
}

func hostWithDefaultPort(host, scheme string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if strings.Contains(host, ":") {
		return host
	}
	if scheme == "https" || scheme == "wss" {
		return net.JoinHostPort(host, "443")
	}
	return net.JoinHostPort(host, "80")
}

func cdpEval(ws *websocket.Conn) (map[string]string, error) {
	expr := `(() => {
const out = {};
for (const store of [localStorage, sessionStorage]) {
  for (let i = 0; i < store.length; i++) {
    const k = store.key(i);
    out[k] = store.getItem(k);
  }
}
out.cookie = document.cookie || "";
return JSON.stringify(out);
})()`
	msg, err := cdpCall(ws, 1, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	result := objMap(objMap(msg["result"])["result"])
	raw, _ := result["value"].(string)
	out := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

func cdpCookies(ws *websocket.Conn) (map[string]string, error) {
	msg, err := cdpCall(ws, 2, "Network.getAllCookies", map[string]any{})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, item := range arrayAny(objMap(msg["result"])["cookies"]) {
		c := objMap(item)
		name, _ := c["name"].(string)
		value, _ := c["value"].(string)
		if name != "" {
			out[name] = value
		}
	}
	return out, nil
}

func cdpCall(ws *websocket.Conn, id int, method string, params map[string]any) (map[string]any, error) {
	if err := json.NewEncoder(ws).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(ws)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			return nil, err
		}
		if got, _ := msg["id"].(float64); int(got) == id {
			if msg["error"] != nil {
				return nil, fmt.Errorf("%v", msg["error"])
			}
			return msg, nil
		}
	}
}

func pickToken(key, value, access, refresh string) (string, string) {
	k := strings.ToLower(key)
	if refresh == "" && strings.Contains(k, "refresh") && strings.Contains(k, "token") {
		refresh = strings.TrimSpace(value)
	}
	if access == "" && ((strings.Contains(k, "access") && strings.Contains(k, "token")) || k == "token" || k == "auth_token") {
		access = strings.TrimSpace(value)
	}
	var nested map[string]any
	if json.Unmarshal([]byte(value), &nested) == nil {
		for nk, nv := range nested {
			if s, ok := nv.(string); ok {
				access, refresh = pickToken(nk, s, access, refresh)
			}
		}
	}
	return strings.TrimPrefix(access, "Bearer "), strings.TrimPrefix(refresh, "Bearer ")
}

func objMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func arrayAny(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}
