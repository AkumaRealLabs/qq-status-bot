package monitor

import "time"

type AlertDecision struct {
	Type     string
	Recover  bool
	Message  string
	NewState AlertState
}

func DecideAlert(now time.Time, kind string, failing bool, message string, prev AlertState) (AlertDecision, bool) {
	if !failing {
		if prev.Active {
			return AlertDecision{Type: kind, Recover: true, Message: message, NewState: AlertState{}}, true
		}
		return AlertDecision{NewState: prev}, false
	}
	if prev.Active && now.Sub(prev.LastAt) < time.Hour {
		return AlertDecision{NewState: prev}, false
	}
	return AlertDecision{
		Type:     kind,
		Message:  message,
		NewState: AlertState{Active: true, LastAt: now},
	}, true
}
