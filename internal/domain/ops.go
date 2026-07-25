package domain

import "time"

type NotificationRules struct {
	Enabled          bool            `json:"enabled"`
	EventTypes       map[string]bool `json:"event_types"`
	FailureThreshold int             `json:"failure_threshold"`
	Recovery         bool            `json:"recovery"`
}

func DefaultNotificationRules() NotificationRules {
	return NotificationRules{
		Enabled:          true,
		FailureThreshold: 2,
		Recovery:         true,
		EventTypes: map[string]bool{
			"balance_low":          true,
			"credential_invalid":   true,
			"balance_query_failed": true,
			"balance_runway_low":   true,
			"cost_sync_failed":     true,
		},
	}
}

func NormalizeNotificationRules(in NotificationRules) NotificationRules {
	out := DefaultNotificationRules()
	out.Enabled = in.Enabled
	out.Recovery = in.Recovery
	if in.FailureThreshold > 0 {
		out.FailureThreshold = in.FailureThreshold
	}
	for key := range out.EventTypes {
		if in.EventTypes != nil {
			if enabled, ok := in.EventTypes[key]; ok {
				out.EventTypes[key] = enabled
			}
		}
	}
	return out
}

type OpsEvent struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Severity   string    `json:"severity"`
	Title      string    `json:"title"`
	Message    string    `json:"message"`
	TargetType string    `json:"target_type,omitempty"`
	TargetID   string    `json:"target_id,omitempty"`
	Actions    []string  `json:"actions"`
	Read       bool      `json:"read"`
	Acked      bool      `json:"acked"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type OpsEventFilter struct {
	Type       string `json:"type,omitempty"`
	State      string `json:"state,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type OpsEventGroup struct {
	Type         string   `json:"type"`
	TargetType   string   `json:"target_type,omitempty"`
	TargetID     string   `json:"target_id,omitempty"`
	Count        int      `json:"count"`
	UnreadCount  int      `json:"unread_count"`
	UnackedCount int      `json:"unacked_count"`
	Latest       OpsEvent `json:"latest"`
}

type AuditLog struct {
	ID         string    `json:"id"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type,omitempty"`
	TargetID   string    `json:"target_id,omitempty"`
	Summary    string    `json:"summary"`
	Fields     []string  `json:"fields"`
	CreatedAt  time.Time `json:"created_at"`
}

type RevenueSnapshot struct {
	ID         string    `json:"id"`
	SourceID   string    `json:"source_id"`
	SourceName string    `json:"source_name"`
	SourceType string    `json:"source_type"`
	CheckedAt  time.Time `json:"checked_at"`
	Revenue    float64   `json:"revenue"`
	Error      string    `json:"error,omitempty"`
}

type SelfCheckItem struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type SelfCheckResponse struct {
	CheckedAt time.Time       `json:"checked_at"`
	Items     []SelfCheckItem `json:"items"`
}
