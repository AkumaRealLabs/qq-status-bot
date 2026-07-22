package httpapi

import (
	"net/http"
	"strconv"
	"strings"

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

func (s *Server) availabilityPolicy(w http.ResponseWriter, r *http.Request) {
	policy, err := s.App.AvailabilityPolicy(r.Context(), r.PathValue("id"))
	writeJSONOrError(w, policy, err)
}

func (s *Server) updateAvailabilityPolicy(w http.ResponseWriter, r *http.Request) {
	var policy domain.AvailabilityPolicy
	if !decode(w, r, &policy) {
		return
	}
	policy, err := s.App.SaveAvailabilityPolicy(r.Context(), r.PathValue("id"), policy)
	writeJSONOrError(w, policy, err)
}

func (s *Server) schedulerAvailability(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.AvailabilityRows(r.Context(), r.URL.Query().Get("upstream_id"), r.URL.Query().Get("state"))
	writeJSONOrError(w, rows, err)
}

func (s *Server) schedulerAvailabilityAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action  string `json:"action"`
		Minutes int    `json:"minutes"`
	}
	if !decode(w, r, &body) {
		return
	}
	row, err := s.App.AvailabilityAction(r.Context(), r.PathValue("card_id"), body.Action, body.Minutes)
	writeJSONOrError(w, row, err)
}

func (s *Server) reconcileSchedulerAvailability(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.ReconcileAvailability(r.Context()))
}

func (s *Server) schedulerTrafficStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.App.TrafficStatus(r.Context())
	writeJSONOrError(w, status, err)
}

func (s *Server) schedulerTraffic(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.TrafficRows(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) reconcileSchedulerTraffic(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.ReconcileTraffic(r.Context()))
}

func (s *Server) adoptSchedulerTrafficBaseline(w http.ResponseWriter, r *http.Request) {
	row, err := s.App.AdoptTrafficBaseline(r.Context(), r.PathValue("channel_id"))
	writeJSONOrError(w, row, err)
}

func (s *Server) schedulerControlPlane(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.SchedulerControlPlane(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) adoptSchedulerControlPlaneChannel(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.AdoptSchedulerControlPlaneChannel(r.Context(), r.PathValue("channel_id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) ggapiSettings(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.GGAPISettings(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) updateGGAPISettings(w http.ResponseWriter, r *http.Request) {
	var body domain.GGAPISettings
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.SaveGGAPISettings(r.Context(), body)
	writeJSONOrError(w, out, err)
}

func (s *Server) ggapiAffinityCache(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.GGAPIAffinityCache(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) clearGGAPIAffinityCache(w http.ResponseWriter, r *http.Request) {
	all := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("all")), "true")
	out, err := s.App.ClearGGAPIAffinityCache(r.Context(), r.URL.Query().Get("rule_name"), all)
	writeJSONOrError(w, out, err)
}
