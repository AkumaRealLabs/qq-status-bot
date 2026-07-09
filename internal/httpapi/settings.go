package httpapi

import (
	"encoding/json"
	"net/http"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"
)

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.App.Settings(r.Context())
	writeJSONOrError(w, cfg, err)
}

func (s *Server) publicSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.App.Store.Settings(r.Context())
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"site_name": cfg.SiteName, "site_icon": cfg.SiteIcon})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var cfg domain.Settings
	if !decode(w, r, &cfg) {
		return
	}
	cfg, err := s.App.UpdateSettings(r.Context(), cfg)
	writeJSONOrError(w, cfg, err)
}

func (s *Server) exportData(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.Store.ExportData(r.Context())
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ai-upstream-monitor-sensitive-export.json"`)
	w.Header().Set("X-Backup-Contains-Secrets", "true")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) importData(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	var in store.ExportData
	if !decodeJSON(w, r, &in, 0) {
		return
	}
	writeNoContentOrError(w, s.App.Store.ImportData(r.Context(), in))
}
