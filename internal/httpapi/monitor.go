package httpapi

import (
	"net/http"

	"ai-upstream-monitor/internal/domain"
)

func (s *Server) monitorStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.MonitorStatus(r.Context(), r.URL.Query().Get("window"))
	writeJSONOrError(w, out, err)
}

func (s *Server) publicMonitorStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.PublicMonitorStatus(r.Context(), r.URL.Query().Get("window"))
	writeJSONOrError(w, out, err)
}

func (s *Server) balances(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.BalanceRows(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) refreshBalances(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.RefreshBalances(r.Context()))
}

func (s *Server) todayRevenue(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.TodayRevenue(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) listRevenueCards(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ListRevenueCards(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) revenueCardOrders(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.RevenueCardOrders(r.Context(), r.PathValue("id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) createRevenueCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		SourceType  string `json:"source_type"`
		BaseURL     string `json:"base_url"`
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
		AdminAPIKey string `json:"admin_api_key"`
		EpayPID     string `json:"epay_pid"`
		EpayKey     string `json:"epay_key"`
		UpstreamID  string `json:"upstream_id"`
		Enabled     *bool  `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	card, err := s.App.SaveRevenueCard(r.Context(), "", domain.RevenueCard{
		Name: body.Name, SourceType: body.SourceType, BaseURL: body.BaseURL, UserID: body.UserID, AccessToken: body.AccessToken,
		AdminAPIKey: body.AdminAPIKey, EpayPID: body.EpayPID, EpayKey: body.EpayKey, UpstreamID: body.UpstreamID, Enabled: enabled,
	})
	writeJSONOrError(w, card, err)
}

func (s *Server) updateRevenueCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		SourceType  string `json:"source_type"`
		BaseURL     string `json:"base_url"`
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
		AdminAPIKey string `json:"admin_api_key"`
		EpayPID     string `json:"epay_pid"`
		EpayKey     string `json:"epay_key"`
		UpstreamID  string `json:"upstream_id"`
		Enabled     *bool  `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	old, err := s.App.Store.RevenueCard(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	enabled := old.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	name, sourceType, upstreamID := body.Name, body.SourceType, body.UpstreamID
	if name == "" {
		name = old.Name
	}
	if sourceType == "" {
		sourceType = old.SourceType
	}
	// Empty secrets mean keep existing; SaveRevenueCard merges with stored values.
	in := domain.RevenueCard{
		Name: name, SourceType: sourceType, BaseURL: body.BaseURL, UserID: body.UserID, AccessToken: body.AccessToken,
		AdminAPIKey: body.AdminAPIKey, EpayPID: body.EpayPID, EpayKey: body.EpayKey, UpstreamID: upstreamID, Enabled: enabled,
	}
	card, err := s.App.SaveRevenueCard(r.Context(), r.PathValue("id"), in)
	writeJSONOrError(w, card, err)
}

func (s *Server) deleteRevenueCard(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.DeleteRevenueCard(r.Context(), r.PathValue("id")))
}

func (s *Server) sortRevenueCards(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	writeNoContentOrError(w, s.App.SortRevenueCards(r.Context(), body.IDs))
}
