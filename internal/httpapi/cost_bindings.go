package httpapi

import (
	"net/http"

	"ai-upstream-monitor/internal/domain"
)

func (s *Server) listCostBindings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.CostBindings(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) createCostBinding(w http.ResponseWriter, r *http.Request) {
	var in domain.SchedulerCostBinding
	if !decode(w, r, &in) {
		return
	}
	out, err := s.App.SaveCostBinding(r.Context(), "", in)
	writeJSONOrError(w, out, err)
}

func (s *Server) updateCostBinding(w http.ResponseWriter, r *http.Request) {
	var in domain.SchedulerCostBinding
	if !decode(w, r, &in) {
		return
	}
	out, err := s.App.SaveCostBinding(r.Context(), r.PathValue("id"), in)
	writeJSONOrError(w, out, err)
}

func (s *Server) deleteCostBinding(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.DeleteCostBinding(r.Context(), r.PathValue("id")))
}

func (s *Server) costBindingChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.SchedulerChannelsForProvider(r.Context(), r.URL.Query().Get("provider"), r.URL.Query().Get("keyword"))
	writeJSONOrError(w, rows, err)
}

func (s *Server) adoptCostBinding(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.AdoptCostBinding(r.Context(), r.PathValue("id"), body.Provider)
	writeJSONOrError(w, out, err)
}
