package httpapi

import (
	"net/http"

	"ai-upstream-monitor/internal/domain"
)

func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.ListCards(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                 string `json:"name"`
		BaseURL              string `json:"base_url"`
		APIKey               string `json:"api_key"`
		UpstreamID           string `json:"upstream_id"`
		KeyID                string `json:"key_id"`
		DisplayGroup         string `json:"display_group"`
		PoolEnabled          *bool  `json:"pool_enabled"`
		ManualCostRatio      string `json:"manual_cost_ratio"`
		SchedulerGroup       string `json:"scheduler_group"`
		SchedulerChannelID   string `json:"scheduler_channel_id"`
		SchedulerChannelName string `json:"scheduler_channel_name"`
		Enabled              *bool  `json:"enabled"`
		PublicEnabled        *bool  `json:"public_enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	publicEnabled := false
	if body.PublicEnabled != nil {
		publicEnabled = *body.PublicEnabled
	}
	poolEnabled := true
	if body.PoolEnabled != nil {
		poolEnabled = *body.PoolEnabled
	}
	card, err := s.App.SaveCard(r.Context(), "", domain.ModelCard{
		Name: body.Name, BaseURL: body.BaseURL, APIKey: body.APIKey, UpstreamID: body.UpstreamID, KeyID: body.KeyID,
		DisplayGroup: body.DisplayGroup, PoolEnabled: poolEnabled, PoolEnabledSet: true, ManualCostRatio: body.ManualCostRatio,
		SchedulerGroup: body.SchedulerGroup, SchedulerChannelID: body.SchedulerChannelID, SchedulerChannelName: body.SchedulerChannelName, Enabled: enabled, PublicEnabled: publicEnabled,
	})
	writeJSONOrError(w, card, err)
}

func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                 string  `json:"name"`
		BaseURL              string  `json:"base_url"`
		APIKey               string  `json:"api_key"`
		UpstreamID           string  `json:"upstream_id"`
		KeyID                string  `json:"key_id"`
		DisplayGroup         *string `json:"display_group"`
		PoolEnabled          *bool   `json:"pool_enabled"`
		ManualCostRatio      *string `json:"manual_cost_ratio"`
		SchedulerGroup       *string `json:"scheduler_group"`
		SchedulerChannelID   *string `json:"scheduler_channel_id"`
		SchedulerChannelName *string `json:"scheduler_channel_name"`
		Enabled              *bool   `json:"enabled"`
		PublicEnabled        *bool   `json:"public_enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	old, err := s.App.GetCard(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	enabled := old.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	publicEnabled := old.PublicEnabled
	if body.PublicEnabled != nil {
		publicEnabled = *body.PublicEnabled
	}
	name, baseURL, apiKey, upstreamID, keyID := body.Name, body.BaseURL, body.APIKey, body.UpstreamID, body.KeyID
	displayGroup := old.DisplayGroup
	if name == "" {
		name = old.Name
	}
	if body.DisplayGroup != nil {
		displayGroup = *body.DisplayGroup
	}
	poolEnabled, manualCostRatio := old.PoolEnabled, old.ManualCostRatio
	if body.PoolEnabled != nil {
		poolEnabled = *body.PoolEnabled
	}
	if body.ManualCostRatio != nil {
		manualCostRatio = *body.ManualCostRatio
	}
	schedulerGroup, schedulerChannelID, schedulerChannelName, schedulerAutoDisabled := old.SchedulerGroup, old.SchedulerChannelID, old.SchedulerChannelName, old.SchedulerAutoDisabled
	if body.SchedulerGroup != nil {
		schedulerGroup, schedulerChannelID, schedulerChannelName, schedulerAutoDisabled = domain.ApplySchedulerGroupPatch(
			old.SchedulerGroup, schedulerChannelID, schedulerChannelName, schedulerAutoDisabled, *body.SchedulerGroup,
		)
	}
	if body.SchedulerChannelID != nil {
		schedulerChannelID, schedulerAutoDisabled = domain.ApplySchedulerChannelPatch(schedulerChannelID, schedulerAutoDisabled, *body.SchedulerChannelID)
	}
	if body.SchedulerChannelName != nil {
		schedulerChannelName = *body.SchedulerChannelName
	}
	// Empty source/secret fields: SaveCard → ModelCard.MergeUpdate keeps stored values.
	card, err := s.App.SaveCard(r.Context(), r.PathValue("id"), domain.ModelCard{
		Name: name, BaseURL: baseURL, APIKey: apiKey, UpstreamID: upstreamID, KeyID: keyID,
		DisplayGroup: displayGroup, PoolEnabled: poolEnabled, PoolEnabledSet: true, ManualCostRatio: manualCostRatio,
		SchedulerGroup: schedulerGroup, SchedulerChannelID: schedulerChannelID, SchedulerChannelName: schedulerChannelName, SchedulerAutoDisabled: schedulerAutoDisabled, Enabled: enabled, PublicEnabled: publicEnabled,
	})
	writeJSONOrError(w, card, err)
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.DeleteCard(r.Context(), r.PathValue("id")))
}

func (s *Server) sortCards(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	writeNoContentOrError(w, s.App.SortCards(r.Context(), body.IDs))
}

func (s *Server) checkCard(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.CheckCard(r.Context(), r.PathValue("id")))
}
