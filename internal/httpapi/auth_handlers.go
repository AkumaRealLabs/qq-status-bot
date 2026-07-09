package httpapi

import (
	"net"
	"net/http"
	"strings"
	"time"
)

func loginKey(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = r.RemoteAddr
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(username))
}

func (s *Server) loginLimited(key string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	now := time.Now()
	fails := pruneLoginFailures(s.loginFails[key], now)
	if s.loginFails != nil {
		s.loginFails[key] = fails
	}
	return len(fails) >= loginFailLimit
}

func (s *Server) recordLoginFailure(key string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	now := time.Now()
	if s.loginFails == nil {
		s.loginFails = map[string][]time.Time{}
	}
	s.loginFails[key] = append(pruneLoginFailures(s.loginFails[key], now), now)
}

func (s *Server) clearLoginFailures(key string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	delete(s.loginFails, key)
}

func pruneLoginFailures(fails []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-loginFailWindow)
	i := 0
	for ; i < len(fails); i++ {
		if fails[i].After(cutoff) {
			break
		}
	}
	return fails[i:]
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.SetupStatus(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	u, err := s.App.Setup(r.Context(), strings.TrimSpace(body.Username), body.Password)
	writeJSONOrError(w, map[string]any{"user": u}, err)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	key := loginKey(r, body.Username)
	if s.loginLimited(key) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	token, u, err := s.App.Login(r.Context(), body.Username, body.Password)
	if err != nil {
		s.recordLoginFailure(key)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.clearLoginFailures(key)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: secureCookie(r), Expires: time.Now().Add(30 * 24 * time.Hour),
	})
	writeJSON(w, map[string]any{"user": u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.App.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureCookie(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie(sessionCookie)
	u, err := s.App.Me(r.Context(), c.Value)
	writeJSONOrError(w, map[string]any{"user": u}, err)
}
