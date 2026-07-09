package httpapi

import (
	"net/http"
	"strconv"

	"ai-upstream-monitor/internal/domain"
)

func (s *Server) opsEvents(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.OpsEvents(r.Context(), opsEventFilterFromQuery(r))
	writeJSONOrError(w, out, err)
}

func (s *Server) opsEventGroups(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.OpsEventGroups(r.Context(), opsEventFilterFromQuery(r))
	writeJSONOrError(w, out, err)
}

func (s *Server) markOpsEventRead(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.MarkOpsEventRead(r.Context(), r.PathValue("id")))
}

func (s *Server) ackOpsEvent(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.AckOpsEvent(r.Context(), r.PathValue("id")))
}

func (s *Server) markOpsEventsRead(w http.ResponseWriter, r *http.Request) {
	filter, ok := opsEventFilterFromBody(w, r)
	if !ok {
		return
	}
	writeNoContentOrError(w, s.App.Store.MarkOpsEventsRead(r.Context(), filter))
}

func (s *Server) ackOpsEvents(w http.ResponseWriter, r *http.Request) {
	filter, ok := opsEventFilterFromBody(w, r)
	if !ok {
		return
	}
	writeNoContentOrError(w, s.App.Store.AckOpsEvents(r.Context(), filter))
}

func opsEventFilterFromQuery(r *http.Request) domain.OpsEventFilter {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return domain.OpsEventFilter{
		Type:       r.URL.Query().Get("type"),
		State:      r.URL.Query().Get("state"),
		TargetType: r.URL.Query().Get("target_type"),
		TargetID:   r.URL.Query().Get("target_id"),
		Limit:      limit,
	}
}

func opsEventFilterFromBody(w http.ResponseWriter, r *http.Request) (domain.OpsEventFilter, bool) {
	var filter domain.OpsEventFilter
	if !decode(w, r, &filter) {
		return filter, false
	}
	return filter, true
}

func (s *Server) auditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.App.AuditLogs(r.Context(), r.URL.Query().Get("action"), r.URL.Query().Get("target"), limit)
	writeJSONOrError(w, out, err)
}

func (s *Server) notificationRules(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.NotificationRules(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) updateNotificationRules(w http.ResponseWriter, r *http.Request) {
	var rules domain.NotificationRules
	if !decode(w, r, &rules) {
		return
	}
	out, err := s.App.SaveNotificationRules(r.Context(), rules)
	writeJSONOrError(w, out, err)
}

func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.TestNotification(r.Context()))
}

func (s *Server) opsProfit(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.Profit(r.Context(), r.URL.Query().Get("window"))
	writeJSONOrError(w, out, err)
}

func (s *Server) opsSelfCheck(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.SelfCheck(r.Context())
	writeJSONOrError(w, out, err)
}
