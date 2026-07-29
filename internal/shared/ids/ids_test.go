package ids

import (
	"regexp"
	"testing"
	"time"
)

func TestNewAtCreatesSortablePrefixedID(t *testing.T) {
	first, err := NewAt("node", time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("first id: %v", err)
	}
	second, err := NewAt("node", time.Date(2026, 7, 29, 8, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("second id: %v", err)
	}

	pattern := regexp.MustCompile(`^node_[0-9A-HJKMNP-TV-Z]{26}$`)
	if !pattern.MatchString(first) {
		t.Fatalf("id %q does not match expected shape", first)
	}
	if first >= second {
		t.Fatalf("ids are not time-sortable: %q >= %q", first, second)
	}
}

func TestNewRejectsInvalidPrefix(t *testing.T) {
	if _, err := NewAt("Node", time.Now()); err == nil {
		t.Fatal("uppercase prefix accepted, want error")
	}
	if _, err := NewAt("1node", time.Now()); err == nil {
		t.Fatal("numeric-leading prefix accepted, want error")
	}
}
