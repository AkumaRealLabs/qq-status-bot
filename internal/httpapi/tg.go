package httpapi

import (
	"net/http"
	"path/filepath"
	"strconv"

	"ai-upstream-monitor/internal/domain"
)

func (s *Server) tgSessionStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.TGSessionStatus(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) startTGSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIID   int    `json:"api_id"`
		APIHash string `json:"api_hash"`
		Phone   string `json:"phone"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.StartTGSession(r.Context(), body.APIID, body.APIHash, body.Phone)
	writeJSONOrError(w, out, err)
}

func (s *Server) verifyTGSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.VerifyTGSession(r.Context(), body.Code)
	writeJSONOrError(w, out, err)
}

func (s *Server) tgSessionPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.TGSessionPassword(r.Context(), body.Password)
	writeJSONOrError(w, out, err)
}

func (s *Server) listTGChannels(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ListTGChannels(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) createTGChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName  string `json:"display_name"`
		Identifier   string `json:"identifier"`
		Enabled      *bool  `json:"enabled"`
		MessageLimit int    `json:"message_limit"`
		PinnedOnly   *bool  `json:"pinned_only"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	pinnedOnly := false
	if body.PinnedOnly != nil {
		pinnedOnly = *body.PinnedOnly
	}
	out, err := s.App.SaveTGChannel(r.Context(), "", domain.TGChannel{
		DisplayName: body.DisplayName, Identifier: body.Identifier, Enabled: enabled, MessageLimit: body.MessageLimit, PinnedOnly: pinnedOnly,
	})
	writeJSONOrError(w, out, err)
}

func (s *Server) updateTGChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName  string `json:"display_name"`
		Enabled      *bool  `json:"enabled"`
		MessageLimit int    `json:"message_limit"`
		PinnedOnly   *bool  `json:"pinned_only"`
	}
	if !decode(w, r, &body) {
		return
	}
	old, err := s.App.GetTGChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	enabled := old.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if body.DisplayName == "" {
		body.DisplayName = old.DisplayName
	}
	if body.MessageLimit == 0 {
		body.MessageLimit = old.MessageLimit
	}
	pinnedOnly := old.PinnedOnly
	if body.PinnedOnly != nil {
		pinnedOnly = *body.PinnedOnly
	}
	out, err := s.App.SaveTGChannel(r.Context(), old.ID, domain.TGChannel{
		DisplayName: body.DisplayName, Identifier: old.Identifier, Username: old.Username, PeerID: old.PeerID, AccessHash: old.AccessHash,
		AvatarURL: old.AvatarURL, Enabled: enabled, MessageLimit: body.MessageLimit, PinnedOnly: pinnedOnly,
	})
	writeJSONOrError(w, out, err)
}

func (s *Server) deleteTGChannel(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.DeleteTGChannel(r.Context(), r.PathValue("id")))
}

func (s *Server) syncTGChannels(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.SyncTGChannels(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) listTGMessages(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.App.ListTGMessages(r.Context(), r.URL.Query().Get("channel_id"), limit)
	writeJSONOrError(w, out, err)
}

func (s *Server) refreshTGMessages(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChannelID string `json:"channel_id"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decode(w, r, &body) {
		return
	}
	writeNoContentOrError(w, s.App.RefreshTGMessages(r.Context(), body.ChannelID))
}

func (s *Server) clearTGMessages(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.DeleteAllTGMessages(r.Context()))
}

func (s *Server) deleteTGMessage(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.DeleteTGMessage(r.Context(), r.PathValue("id")))
}

func (s *Server) tgMedia(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || name != filepath.Base(name) {
		writeError(w, http.StatusBadRequest, "bad media name")
		return
	}
	http.ServeFile(w, r, filepath.Join(s.App.TGMediaDir, name))
}
