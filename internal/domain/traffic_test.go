package domain

import (
	"testing"
	"time"
)

func TestClassifyTrafficError(t *testing.T) {
	tests := []struct {
		name, errorType, code, message, want string
		status                               int
	}{
		{"success", "", "", "", TrafficEventSuccess, 200},
		{"quota", "quota_exhausted", "", "", TrafficEventHardFailure, 400},
		{"invalid key", "invalid_api_key", "", "", TrafficEventHardFailure, 401},
		{"rate limit", "rate_limit", "", "", TrafficEventSoftFailure, 429},
		{"server", "", "", "", TrafficEventSoftFailure, 502},
		{"auth confirm", "", "", "", TrafficEventSoftFailure, 401},
		{"context", "context_length_exceeded", "", "", TrafficEventUserError, 400},
		{"policy", "content_policy", "", "", TrafficEventUserError, 422},
		{"policy wrapped by upstream", "content_filter", "", "", TrafficEventUserError, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyTrafficError(tt.status, tt.errorType, tt.code, tt.message); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestTrafficDecisionThresholdsAndProtection(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		w15, w1, w5 TrafficWindow
		alternative bool
		state       string
		status      int
		weight      uint
	}{
		{name: "hard once", w15: TrafficWindow{Requests: 1, HardFailures: 1, FailureRate: 1}, state: "hard_blocked", status: 2},
		{name: "auth twice", w15: TrafficWindow{Requests: 2, SoftFailures: 2, AuthFailures: 2, FailureRate: 1}, state: "hard_blocked", status: 2},
		{name: "warning", w15: TrafficWindow{Requests: 5, SoftFailures: 2, FailureRate: .4}, alternative: true, state: "warning", status: 1, weight: 50},
		{name: "warning last route", w15: TrafficWindow{Requests: 5, SoftFailures: 2, FailureRate: .4}, state: "degraded", status: 1, weight: 10},
		{name: "degraded", w1: TrafficWindow{Requests: 10, SoftFailures: 5, FailureRate: .5}, alternative: true, state: "degraded", status: 1, weight: 10},
		{name: "soft blocked", w15: TrafficWindow{Requests: 10, SoftFailures: 8, FailureRate: .8}, alternative: true, state: "soft_blocked", status: 2},
		{name: "last route", w15: TrafficWindow{Requests: 10, SoftFailures: 8, FailureRate: .8}, state: "degraded", status: 1, weight: 10},
		{name: "user errors ignored", w15: TrafficWindow{Requests: 10, UserErrors: 10, FailureRate: 0}, state: "healthy", status: 1, weight: 100},
		{name: "low traffic probe", w15: TrafficWindow{Requests: 3, SoftFailures: 3, FailureRate: 1}, state: "probe_required", status: 1, weight: 100},
		{name: "low traffic probe after four failures", w15: TrafficWindow{Requests: 4, SoftFailures: 4, FailureRate: 1}, state: "probe_required", status: 1, weight: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, status, _, weight, _, _ := TrafficDecision(tt.w15, tt.w1, tt.w5, tt.alternative, now)
			if state != tt.state || status != tt.status || weight != tt.weight {
				t.Fatalf("decision=%s/%d/%d want=%s/%d/%d", state, status, weight, tt.state, tt.status, tt.weight)
			}
		})
	}
}

func TestAggregateTrafficP95AndUserErrors(t *testing.T) {
	start := time.Unix(1000, 0)
	events := []TrafficEvent{
		{ChannelID: "1", Model: "m", OccurredAt: start.Add(time.Second), Kind: TrafficEventSuccess, TTFTMS: 100},
		{ChannelID: "1", Model: "m", OccurredAt: start.Add(2 * time.Second), Kind: TrafficEventSoftFailure, HTTPStatus: 429, TTFTMS: 300},
		{ChannelID: "1", Model: "m", OccurredAt: start.Add(3 * time.Second), Kind: TrafficEventUserError, HTTPStatus: 400},
	}
	rows := AggregateTraffic(events, start, start.Add(10*time.Second))
	if len(rows) != 1 || rows[0].Requests != 3 || rows[0].UserErrors != 1 || rows[0].FailureRate != .5 || rows[0].P95TTFTMS != 100 || rows[0].AvgTTFTMS != 100 {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestAggregateTrafficExcludesSessionFailures(t *testing.T) {
	start := time.Unix(1000, 0)
	rows := AggregateTraffic([]TrafficEvent{
		{ChannelID: "1", Model: "m", OccurredAt: start.Add(time.Second), Kind: TrafficEventSoftFailure, SessionScoped: true},
		{ChannelID: "1", Model: "m", OccurredAt: start.Add(2 * time.Second), Kind: TrafficEventSuccess, TTFTMS: 120},
	}, start, start.Add(10*time.Second))
	if len(rows) != 1 || rows[0].Requests != 1 || rows[0].Successes != 1 || rows[0].SoftFailures != 0 {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestTrafficDedupeKeyStable(t *testing.T) {
	event := TrafficEvent{Source: "error", OccurredAt: time.Unix(1, 0), ChannelID: "9", RequestID: "req", Model: "m", Kind: TrafficEventSoftFailure}
	if TrafficDedupeKey(event) != TrafficDedupeKey(event) {
		t.Fatal("dedupe key is unstable")
	}
	event.ChannelID = "10"
	if TrafficDedupeKey(event) == TrafficDedupeKey(TrafficEvent{Source: "error", OccurredAt: time.Unix(1, 0), ChannelID: "9", RequestID: "req", Model: "m", Kind: TrafficEventSoftFailure}) {
		t.Fatal("different attempt identity collided")
	}
	event.ChannelID = "9"
	event.RetrySucceeded = true
	if TrafficDedupeKey(event) == TrafficDedupeKey(TrafficEvent{Source: "error", OccurredAt: time.Unix(1, 0), ChannelID: "9", RequestID: "req", Model: "m", Kind: TrafficEventSoftFailure}) {
		t.Fatal("retry outcome must be part of attempt identity")
	}
}

func TestTrafficRecoveryTarget(t *testing.T) {
	start := time.Unix(1000, 0)
	tests := []struct {
		stage      int
		at         time.Time
		wantStage  int
		wantWeight uint
		complete   bool
	}{
		{0, start, 1, 10, false},
		{1, start.Add(59 * time.Second), 1, 10, false},
		{1, start.Add(time.Minute), 2, 25, false},
		{2, start.Add(time.Minute), 3, 50, false},
		{3, start.Add(2 * time.Minute), 4, 100, true},
	}
	for _, tt := range tests {
		stage, weight, complete := TrafficRecoveryTarget(tt.stage, start, tt.at)
		if stage != tt.wantStage || weight != tt.wantWeight || complete != tt.complete {
			t.Fatalf("stage=%d at=%s got=%d/%d/%v", tt.stage, tt.at, stage, weight, complete)
		}
	}
}
