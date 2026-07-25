package httpapi

import (
	"errors"
	"io"
	"net/http"

	"ai-upstream-monitor/internal/app"
)

const oneBotEventBodyLimit = 128 << 10

func (s *Server) oneBotStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.OneBotStatus(r.Context())
	writeJSONOrError(w, out, err)
}

// oneBotEvents 无会话接收 LLBot HTTP POST；不记录请求体或鉴权头。
func (s *Server) oneBotEvents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, oneBotEventBodyLimit)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "JSON 请求体过大")
		return
	}
	if err := s.App.AuthorizeOneBotEvent(r.Context(), r.Header.Get("X-Signature"), payload); err != nil {
		if errors.Is(err, app.ErrOneBotUnauthorized) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "onebot event unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
