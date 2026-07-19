package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"ai-upstream-monitor/internal/app"
	"ai-upstream-monitor/internal/onebot"
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
	var event onebot.Event
	if err := json.NewDecoder(bytes.NewReader(payload)).Decode(&event); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}
	err = s.App.HandleOneBotEvent(r.Context(), event)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "onebot event unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
