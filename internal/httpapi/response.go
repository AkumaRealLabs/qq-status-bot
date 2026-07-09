package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"ai-upstream-monitor/internal/app"
)

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	return decodeJSON(w, r, out, defaultJSONBodyLimit)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	if limit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusBadRequest, "JSON 请求体过大")
			return false
		}
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return false
	}
	return true
}

func writeJSONOrError(w http.ResponseWriter, out any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if app.IsBadRequest(err) {
			status = http.StatusBadRequest
		} else if code, ok := app.ErrorStatus(err); ok {
			status = code
		} else if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, out)
}

func writeNoContentOrError(w http.ResponseWriter, err error) {
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, out any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
