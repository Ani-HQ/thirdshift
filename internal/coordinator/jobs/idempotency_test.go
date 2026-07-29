package jobs

import (
	"encoding/json"
	"testing"
)

func TestResolveIdempotencyReplay(t *testing.T) {
	decision := ResolveIdempotency(IdempotencyRecord{
		RequestHash:      "sha256:one",
		ResponseStatus:   200,
		ResponseMetadata: json.RawMessage(`{"ok":true}`),
	}, true, "sha256:one")
	if !decision.Replay || decision.Status != 200 || string(decision.Body) != `{"ok":true}` {
		t.Fatalf("bad replay decision: %#v", decision)
	}
}

func TestResolveIdempotencyRejectsHashMismatch(t *testing.T) {
	decision := ResolveIdempotency(IdempotencyRecord{RequestHash: "sha256:one"}, true, "sha256:two")
	if decision.Error.Code != CodeInvalidRequest || decision.Error.Status != 409 {
		t.Fatalf("bad mismatch decision: %#v", decision)
	}
}

func TestResolveIdempotencyAllowsNewRequest(t *testing.T) {
	decision := ResolveIdempotency(IdempotencyRecord{}, false, "sha256:new")
	if decision.Replay || decision.Error.Code != "" {
		t.Fatalf("bad new request decision: %#v", decision)
	}
}
