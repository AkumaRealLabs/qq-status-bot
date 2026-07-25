package httpapi

import (
	"net/http"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
)

func (s *Server) listUpstreams(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.UpstreamRows(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) createUpstream(w http.ResponseWriter, r *http.Request) {
	var body domain.Upstream
	if !decode(w, r, &body) {
		return
	}
	u, err := s.App.SaveUpstream(r.Context(), "", body)
	writeJSONOrError(w, u, err)
}

func (s *Server) updateUpstream(w http.ResponseWriter, r *http.Request) {
	var body domain.Upstream
	if !decode(w, r, &body) {
		return
	}
	u, err := s.App.SaveUpstream(r.Context(), r.PathValue("id"), body)
	writeJSONOrError(w, u, err)
}

func (s *Server) deleteUpstream(w http.ResponseWriter, r *http.Request) {
	err := s.App.DeleteUpstream(r.Context(), r.PathValue("id"))
	writeNoContentOrError(w, err)
}

func (s *Server) checkUpstream(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.RefreshUpstreamBalance(r.Context(), r.PathValue("id")))
}

func (s *Server) syncKeys(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.SyncKeys(r.Context(), r.PathValue("id")))
}

func (s *Server) balanceRechargeCapabilities(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.BalanceRechargeCapabilities(r.Context(), r.PathValue("id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) redeemBalance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.RedeemBalance(r.Context(), r.PathValue("id"), body.Code)
	writeJSONOrError(w, out, err)
}

func (s *Server) createBalanceRechargeOrder(w http.ResponseWriter, r *http.Request) {
	var body monitor.RechargeOrderRequest
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.CreateBalanceRechargeOrder(r.Context(), r.PathValue("id"), body)
	writeJSONOrError(w, out, err)
}

func (s *Server) balanceRechargeLogs(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.BalanceRechargeLogs(r.Context(), r.PathValue("id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) refreshBalanceRechargeLog(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.RefreshBalanceRechargeLog(r.Context(), r.PathValue("id"), r.PathValue("log_id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) deleteBalanceRechargeLog(w http.ResponseWriter, r *http.Request) {
	err := s.App.DeleteBalanceRechargeLog(r.Context(), r.PathValue("id"), r.PathValue("log_id"))
	writeNoContentOrError(w, err)
}
