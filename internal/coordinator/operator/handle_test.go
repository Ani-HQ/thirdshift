package operator

import (
	"regexp"
	"strings"
	"testing"
)

func TestHostHandleIsDeterministicAndAnonymous(t *testing.T) {
	const nodeID = "node_01J0M000000000000000000001"
	first := HostHandle(nodeID)
	if first != HostHandle(nodeID) {
		t.Fatal("handle is not stable for the same node id")
	}
	if HostHandle("node_01J0M000000000000000000002") == first {
		t.Fatal("two different node ids produced the same handle")
	}

	shape := regexp.MustCompile(`^[a-z]+-[a-z]+$`)
	if !shape.MatchString(first) {
		t.Fatalf("handle %q is not adjective-animal", first)
	}
	// The handle must not carry any part of the id it was derived from.
	if strings.Contains(first, "node") || strings.Contains(first, "01J0M") {
		t.Fatalf("handle %q leaks the node id", first)
	}
	for _, fragment := range []string{nodeID, strings.ToLower(nodeID), strings.TrimPrefix(nodeID, "node_")} {
		if strings.Contains(first, fragment) {
			t.Fatalf("handle %q contains the node id fragment %q", first, fragment)
		}
	}
}

func TestHostHandleSpreadsAcrossTheNameSpace(t *testing.T) {
	seen := map[string]int{}
	for _, id := range []string{
		"node_01J0M000000000000000000001",
		"node_01J0M000000000000000000002",
		"node_01J0M000000000000000000003",
		"node_01J0M000000000000000000004",
		"node_01J0M000000000000000000005",
		"node_01J0M000000000000000000006",
		"node_01J0M000000000000000000007",
		"node_01J0M000000000000000000008",
	} {
		seen[HostHandle(id)]++
	}
	if len(seen) < 7 {
		t.Fatalf("eight node ids collapsed into %d handles: %#v", len(seen), seen)
	}
}

func TestPublicHostState(t *testing.T) {
	for _, tc := range []struct {
		name         string
		nodeState    string
		sessionState string
		fresh        bool
		want         string
	}{
		{"busy and connected is serving", "BUSY", "connected", true, HostStateServing},
		{"available and connected is idle", "AVAILABLE", "connected", true, HostStateIdle},
		{"draining and connected is idle", "DRAINING", "draining", true, HostStateIdle},
		{"stale heartbeat is offline", "BUSY", "connected", false, HostStateOffline},
		{"closed session is offline", "AVAILABLE", "closed", true, HostStateOffline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicHostState(tc.nodeState, tc.sessionState, tc.fresh); got != tc.want {
				t.Fatalf("state = %q, want %q", got, tc.want)
			}
		})
	}
}
