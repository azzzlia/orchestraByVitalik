package session

import (
	"strings"
	"testing"
)

func TestParsePlanUsage(t *testing.T) {
	u, err := parsePlanUsage(strings.NewReader(`{"five_hour":{"utilization":41.0,"resets_at":"2026-10-29T17:00:00.123+00:00"},"seven_day":{"utilization":33,"resets_at":"2026-11-02T09:00:00+00:00"},"seven_day_opus":null}`))
	if err != nil || u.FiveHour != 41 || u.Week != 33 || u.FiveReset.IsZero() || u.WeekReset.IsZero() {
		t.Fatalf("got %+v %v", u, err)
	}
	if _, err := parsePlanUsage(strings.NewReader(`{"error":"x"}`)); err == nil {
		t.Fatal("want an error without limits")
	}
}
