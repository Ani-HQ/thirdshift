package protocol

import (
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestEmbeddedSchemasValidateEnvelopes(t *testing.T) {
	v := &Validator{embedded: true, cache: map[MessageType]*jsonschema.Schema{}}
	payload := []byte(`{"protocol_version":"1.0","message_id":"msg_01J00000000000000000000000","type":"node.heartbeat","sent_at":"2026-07-31T00:00:00Z","payload":{"node_id":"node_01J00000000000000000000000","sequence":1,"state":"AVAILABLE","model_id":"m","runtime_hash":"sha256:a","model_hash":"sha256:b","gpu":{"name":"g","vram_total_mb":8192,"vram_free_mb":8000,"temperature_c":50,"power_w":100,"power_limit_w":150,"utilization_percent":1},"active_job_id":null,"uptime_seconds":5,"timestamp":"2026-07-31T00:00:00Z"}}`)
	if _, err := v.ValidateEnvelope(payload); err != nil {
		t.Fatalf("embedded validation failed: %v", err)
	}
	garbage := []byte(`{"protocol_version":"1.0","message_id":"msg_01J00000000000000000000000","type":"node.heartbeat","sent_at":"2026-07-31T00:00:00Z","payload":{"bogus":true}}`)
	if _, err := v.ValidateEnvelope(garbage); err == nil {
		t.Fatal("embedded validation accepted invalid heartbeat payload")
	}
}
