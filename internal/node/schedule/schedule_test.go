package schedule

import (
	"testing"
	"time"
)

func TestWindowAcrossMidnight(t *testing.T) {
	window, err := ParseWindow("23:00", "08:00")
	if err != nil {
		t.Fatalf("parse window: %v", err)
	}
	loc := time.FixedZone("test", 5*60*60+30*60)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"before start", time.Date(2026, 7, 29, 21, 59, 0, 0, loc), StateOutOfWindow},
		{"at start", time.Date(2026, 7, 29, 23, 0, 0, 0, loc), StateInWindow},
		{"after midnight", time.Date(2026, 7, 30, 1, 0, 0, 0, loc), StateInWindow},
		{"at end", time.Date(2026, 7, 30, 8, 0, 0, 0, loc), StateOutOfWindow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := window.StateAt(tt.now, loc); got != tt.want {
				t.Fatalf("state = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestWindowSameDay(t *testing.T) {
	window, err := ParseWindow("09:30", "17:15")
	if err != nil {
		t.Fatalf("parse window: %v", err)
	}
	loc := time.UTC
	if got := window.StateAt(time.Date(2026, 7, 29, 12, 0, 0, 0, loc), loc); got != StateInWindow {
		t.Fatalf("midday state = %s", got)
	}
	if got := window.StateAt(time.Date(2026, 7, 29, 18, 0, 0, 0, loc), loc); got != StateOutOfWindow {
		t.Fatalf("evening state = %s", got)
	}
}

func TestWindowUsesInjectedTimezone(t *testing.T) {
	window, err := ParseWindow("23:00", "08:00")
	if err != nil {
		t.Fatalf("parse window: %v", err)
	}
	utcInstant := time.Date(2026, 7, 29, 18, 0, 0, 0, time.UTC)
	local := time.FixedZone("utc-plus-530", 5*60*60+30*60)
	if got := window.StateAt(utcInstant, local); got != StateInWindow {
		t.Fatalf("state with injected timezone = %s, want %s", got, StateInWindow)
	}
	if got := window.StateAt(utcInstant, time.UTC); got != StateOutOfWindow {
		t.Fatalf("state in UTC = %s, want %s", got, StateOutOfWindow)
	}
}

func TestParseWindowRejectsBadTime(t *testing.T) {
	if _, err := ParseWindow("24:00", "08:00"); err == nil {
		t.Fatal("expected bad hour to fail")
	}
}
