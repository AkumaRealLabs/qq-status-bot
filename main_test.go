package main

import "testing"

func TestConvertedBalanceValuesAppliesRate(t *testing.T) {
	_, _, remain := convertedBalanceValues("sub2api", 0.1, 10, 0, 10)
	if remain != 1 {
		t.Fatalf("remain = %v, want 1", remain)
	}

	_, _, remain = convertedBalanceValues("newapi", 0.1, 5000000, 0, 5000000)
	if remain != 1 {
		t.Fatalf("newapi remain = %v, want 1", remain)
	}
}

func TestNormalizedCheckInterval(t *testing.T) {
	if got := normalizedCheckInterval(0); got != 5 {
		t.Fatalf("zero interval = %d, want 5", got)
	}
	if got := normalizedCheckInterval(15); got != 15 {
		t.Fatalf("interval = %d, want 15", got)
	}
}
