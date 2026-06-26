package monitor

import (
	"testing"
	"time"
)

func TestDecideAlertThrottleAndRecover(t *testing.T) {
	now := time.Unix(1000, 0)
	dec, ok := DecideAlert(now, "ping", true, "down", AlertState{})
	if !ok || !dec.NewState.Active {
		t.Fatalf("expected first alert, got %#v ok=%v", dec, ok)
	}

	_, ok = DecideAlert(now.Add(30*time.Minute), "ping", true, "still down", dec.NewState)
	if ok {
		t.Fatal("did not expect throttled duplicate alert")
	}

	_, ok = DecideAlert(now.Add(2*time.Hour), "ping", true, "still down", dec.NewState)
	if !ok {
		t.Fatal("expected repeat after throttle window")
	}

	dec, ok = DecideAlert(now.Add(3*time.Hour), "ping", false, "up", dec.NewState)
	if !ok || !dec.Recover || dec.NewState.Active {
		t.Fatalf("expected recovery alert, got %#v ok=%v", dec, ok)
	}
}
