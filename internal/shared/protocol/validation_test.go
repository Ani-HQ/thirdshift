package protocol

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestValidatorAcceptsValidEnvelope(t *testing.T) {
	validator, err := NewValidator(filepath.Join("..", "..", "..", "packages", "protocol", "schemas"))
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	envelope, err := NewEnvelope("msg_01J0M000000000000000000000", TypeNodeHello, time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC), NodeHelloPayload{
		NodeID:                    "node_01J0M000000000000000000000",
		AgentVersion:              "test",
		SupportedProtocolVersions: []string{"1.0"},
		Capabilities:              []string{"chat_completions"},
	})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	data, err := validator.MarshalAndValidate(envelope)
	if err != nil {
		t.Fatalf("marshal and validate: %v", err)
	}
	if _, err := validator.ValidateEnvelope(data); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidatorRejectsSchemaMismatch(t *testing.T) {
	validator, err := NewValidator(filepath.Join("..", "..", "..", "packages", "protocol", "schemas"))
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	data := []byte(`{"protocol_version":"1.0","message_id":"msg_01J0M000000000000000000000","type":"node.hello","sent_at":"2026-07-29T08:00:00Z","payload":{"node_id":"node_01J0M000000000000000000000"}}`)
	if _, err := validator.ValidateEnvelope(data); err == nil {
		t.Fatal("invalid node.hello validated, want error")
	}
}

// One validator is shared by every node session goroutine in the coordinator,
// so concurrent first use of the schema cache must be safe.
func TestValidatorIsSafeForConcurrentUse(t *testing.T) {
	validator, err := NewValidator(filepath.Join("..", "..", "..", "packages", "protocol", "schemas"))
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	types := []MessageType{
		TypeNodeHello,
		TypeSessionAccepted,
		TypeNodeHeartbeat,
		TypeNodeStateChanged,
		TypeJobOffer,
	}
	payloads := map[MessageType]any{
		TypeNodeHello: NodeHelloPayload{
			NodeID:                    "node_01J0M000000000000000000000",
			AgentVersion:              "test",
			SupportedProtocolVersions: []string{"1.0"},
			Capabilities:              []string{"chat_completions"},
		},
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(types)*8)
	for i := 0; i < 8; i++ {
		for _, typ := range types {
			wg.Add(1)
			go func(typ MessageType) {
				defer wg.Done()
				if _, err := validator.schemaFor(typ); err != nil {
					errs <- err
					return
				}
				if payload, ok := payloads[typ]; ok {
					envelope, err := NewEnvelope("msg_01J0M000000000000000000000", typ, time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC), payload)
					if err != nil {
						errs <- err
						return
					}
					if _, err := validator.MarshalAndValidate(envelope); err != nil {
						errs <- err
					}
				}
			}(typ)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent validator use: %v", err)
	}
}

func TestDefaultSchemasDirFindsRepositoryPath(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
	if err := os.Chdir(filepath.Join("..", "..", "..", "cmd", "thirdshift")); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	dir, err := DefaultSchemasDir()
	if err != nil {
		t.Fatalf("default schemas dir: %v", err)
	}
	if filepath.Base(dir) != "schemas" {
		t.Fatalf("schemas dir base = %q, want schemas", filepath.Base(dir))
	}
}
