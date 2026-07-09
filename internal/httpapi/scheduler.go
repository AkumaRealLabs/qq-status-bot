package httpapi

import (
	"net/http"
	"strconv"

	"ai-upstream-monitor/internal/domain"
)

func (s *Server) schedulerConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.App.SchedulerConfig(r.Context())
	writeJSONOrError(w, cfg, err)
}

func (s *Server) updateSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	var cfg domain.SchedulerConfig
	if !decode(w, r, &cfg) {
		return
	}
	cfg, err := s.App.SaveSchedulerConfig(r.Context(), cfg)
	writeJSONOrError(w, cfg, err)
}

func (s *Server) schedulerChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.SchedulerChannels(r.Context(), r.URL.Query().Get("keyword"))
	writeJSONOrError(w, rows, err)
}

func (s *Server) schedulerGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.SchedulerGroups(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) schedulerLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.App.SchedulerLogs(r.Context(), limit)
	writeJSONOrError(w, rows, err)
}

func (s *Server) applySchedulerGroups(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ApplySchedulerGroups(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) setCardSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status int `json:"status"`
	}
	if !decode(w, r, &body) {
		return
	}
	card, err := s.App.SetCardSchedulerChannelStatus(r.Context(), r.PathValue("id"), body.Status)
	writeJSONOrError(w, card, err)
}
