package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"qq-status-bot/internal/app"
	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/qqbot"
)

const (
	sessionCookie    = "qq_status_session"
	webhookBodyLimit = 256 << 10
	defaultJSONLimit = 1 << 20
)

type Server struct {
	App    *app.Service
	Static fs.FS
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.root)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.auth(s.logout))
	mux.HandleFunc("GET /api/auth/me", s.auth(s.me))
	mux.HandleFunc("GET /api/settings", s.auth(s.settings))
	mux.HandleFunc("PATCH /api/settings", s.auth(s.updateSettings))
	mux.HandleFunc("GET /api/status-preview", s.auth(s.statusPreview))
	mux.HandleFunc("POST /api/status/send", s.auth(s.statusSend))
	mux.HandleFunc("GET /api/logs", s.auth(s.logs))
	mux.HandleFunc("GET /api/groups/discovered", s.auth(s.discoveredGroups))
	mux.HandleFunc("POST /api/alerts/test", s.auth(s.alertTest))
	mux.HandleFunc("POST /api/alerts/simulate", s.auth(s.alertSimulate))
	mux.HandleFunc("POST /qqbot/events", s.qqBotEvents)
	mux.Handle("GET /admin/", s.static())
	mux.Handle("GET /assets/", s.static())
	return mux
}

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/admin/", http.StatusFound)
		return
	}
	s.static().ServeHTTP(w, r)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.App.Health())
}

func (s *Server) setupStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"initialized": s.App.SetupStatus()})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username,omitempty"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.App.Setup(input.Username, input.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.loginWithCredentials(w, input.Username, input.Password)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username,omitempty"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	s.loginWithCredentials(w, input.Username, input.Password)
}

func (s *Server) loginWithCredentials(w http.ResponseWriter, username, password string) {
	token, err := s.App.Login(username, password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "账号或密码错误")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || !s.App.Authenticated(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "未登录")
			return
		}
		next(w, r)
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.App.Logout(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (s *Server) settings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.App.Settings())
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var input domain.Settings
	if !decodeJSON(w, r, &input) {
		return
	}
	updated, err := s.App.UpdateSettings(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) statusPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	image, err := s.App.StatusPreview(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(image)
}

func (s *Server) statusSend(w http.ResponseWriter, r *http.Request) {
	var input struct {
		GroupOpenID string `json:"group_openid"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.App.SendStatus(r.Context(), input.GroupOpenID); err != nil {
		writeError(w, activeActionStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	writeJSON(w, http.StatusOK, s.App.Logs(limit))
}

func (s *Server) discoveredGroups(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.App.DiscoveredGroups())
}

func (s *Server) alertTest(w http.ResponseWriter, r *http.Request) {
	var input struct {
		GroupOpenID string `json:"group_openid"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.App.TestAlert(r.Context(), input.GroupOpenID); err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, app.ErrAlertGroupNotConfigured) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) alertSimulate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		GroupOpenID string `json:"group_openid"`
		Kind        string `json:"kind"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.App.SimulateAlert(r.Context(), input.GroupOpenID, input.Kind); err != nil {
		writeError(w, activeActionStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func activeActionStatus(err error) int {
	if errors.Is(err, app.ErrActiveGroupNotAvailable) || errors.Is(err, app.ErrInvalidAlertSimulation) {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

func (s *Server) qqBotEvents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, webhookBodyLimit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "请求体无效")
		return
	}
	response, err := s.App.HandleWebhook(r.Header.Get(qqbot.HeaderTimestamp), r.Header.Get(qqbot.HeaderSignature), body)
	if errors.Is(err, app.ErrUnauthorized) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid callback")
		return
	}
	if len(response) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response)
}

func (s *Server) static() http.Handler {
	if s.Static == nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.FS(s.Static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(path, "admin/") {
			path = strings.TrimPrefix(path, "admin/")
		}
		if path == "" || path == "admin" {
			path = ""
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + path
		files.ServeHTTP(w, r2)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, defaultJSONLimit)
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 请求体无效")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
