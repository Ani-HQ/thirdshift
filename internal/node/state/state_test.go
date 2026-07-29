package state

import "testing"

func TestRejectsIllegalTransition(t *testing.T) {
	if err := Transition(Unregistered, Busy); err == nil {
		t.Fatal("UNREGISTERED -> BUSY accepted, want error")
	}
}

func TestPauseResumeAndDrainLegality(t *testing.T) {
	tests := []struct {
		from State
		to   State
	}{
		{Available, Paused},
		{Paused, Idle},
		{Idle, Draining},
		{Busy, Draining},
		{Draining, Paused},
		{Draining, Available},
	}
	for _, tt := range tests {
		if err := Transition(tt.from, tt.to); err != nil {
			t.Fatalf("%s -> %s rejected: %v", tt.from, tt.to, err)
		}
	}
}

func TestCanonicalStartupPath(t *testing.T) {
	path := []State{Unregistered, Registering, Offline, Starting, Preparing, Available, Busy, Available, Offline}
	for i := 1; i < len(path); i++ {
		if err := Transition(path[i-1], path[i]); err != nil {
			t.Fatalf("%s -> %s rejected: %v", path[i-1], path[i], err)
		}
	}
}
