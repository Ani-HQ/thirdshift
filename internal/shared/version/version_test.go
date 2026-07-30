package version

import "testing"

func TestStringIncludesVersionCommitAndDate(t *testing.T) {
	oldVersion, oldCommit, oldBuildDate := Version, Commit, BuildDate
	defer func() {
		Version, Commit, BuildDate = oldVersion, oldCommit, oldBuildDate
	}()
	Version = "0.1.0-alpha"
	Commit = "abc123"
	BuildDate = "2026-07-30T00:00:00Z"
	if got, want := String(), "0.1.0-alpha commit=abc123 date=2026-07-30T00:00:00Z"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
