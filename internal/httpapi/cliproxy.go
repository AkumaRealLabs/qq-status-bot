package httpapi

import (
	"net/http"
	"strings"

	"ai-upstream-monitor/internal/domain"
)

func (s *Server) cliProxyConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.App.CLIProxyConfig(r.Context())
	writeJSONOrError(w, cfg, err)
}

func (s *Server) updateCLIProxyConfig(w http.ResponseWriter, r *http.Request) {
	var cfg domain.CLIProxyConfig
	if !decode(w, r, &cfg) {
		return
	}
	cfg, err := s.App.SaveCLIProxyConfig(r.Context(), cfg)
	writeJSONOrError(w, cfg, err)
}

func (s *Server) cliProxyAccounts(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.CLIProxyAccounts(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) cliProxyAccountQuota(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.App.CLIProxyAccountQuota(r.Context(), r.PathValue("name"), q.Get("auth_index"), q.Get("account"), q.Get("account_type"))
	writeJSONOrError(w, out, err)
}

func (s *Server) uploadCLIProxyAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if !decode(w, r, &body) {
		return
	}
	writeNoContentOrError(w, s.App.UploadCLIProxyAccount(r.Context(), body.Name, body.Content))
}

func (s *Server) downloadCLIProxyAccount(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	b, contentType, err := s.App.DownloadCLIProxyAccount(r.Context(), name)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	_, _ = w.Write(b)
}

func (s *Server) deleteCLIProxyAccount(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.DeleteCLIProxyAccount(r.Context(), r.PathValue("name")))
}

func (s *Server) resetCLIProxyQuota(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ResetCLIProxyQuota(r.Context(), r.PathValue("name"))
	writeJSONOrError(w, out, err)
}
