package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	nodestate "github.com/anianroid/thirdshift/internal/node/state"
	"github.com/anianroid/thirdshift/internal/shared/protocol"
	"nhooyr.io/websocket"
)

func TestBuildHeartbeatPayload(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	heartbeat := BuildHeartbeat(
		"node_01J0M000000000000000000000",
		7,
		nodestate.Available,
		RuntimeStatus{
			ModelID:     "thirdshift-tiny-chat-v1",
			RuntimeHash: "sha256:runtime",
			ModelHash:   "sha256:model",
		},
		protocol.GPUStatus{
			Name:               "NVIDIA GeForce RTX 4070",
			VRAMTotalMB:        12282,
			VRAMFreeMB:         10400,
			TemperatureC:       62,
			PowerW:             146,
			PowerLimitW:        180,
			UtilizationPercent: 4,
		},
		123,
		now,
	)
	if heartbeat.NodeID == "" || heartbeat.State != "AVAILABLE" {
		t.Fatalf("bad heartbeat identity/state: %#v", heartbeat)
	}
	if heartbeat.ModelID != "thirdshift-tiny-chat-v1" || heartbeat.RuntimeHash == "" || heartbeat.ModelHash == "" {
		t.Fatalf("bad heartbeat model/runtime fields: %#v", heartbeat)
	}
	if !heartbeat.Timestamp.Equal(now) {
		t.Fatalf("timestamp = %s, want %s", heartbeat.Timestamp, now)
	}
}

func TestCatalogRuntimeStatusProvider(t *testing.T) {
	status, err := CatalogRuntimeStatusProvider{CatalogDir: "../../../models/catalog"}.Prepare(context.Background(), "thirdshift-tiny-chat-v1")
	if err != nil {
		t.Fatalf("prepare from catalog: %v", err)
	}
	if status.ModelID != "thirdshift-tiny-chat-v1" {
		t.Fatalf("model_id = %q", status.ModelID)
	}
	if status.RuntimeHash == "sha256:" || status.ModelHash == "sha256:" {
		t.Fatalf("hash fields not populated: %#v", status)
	}
}

func TestCanonicalSHA256DoesNotDoublePrefix(t *testing.T) {
	if got := canonicalSHA256("sha256:abc"); got != "sha256:abc" {
		t.Fatalf("canonical prefixed hash = %q", got)
	}
	if got := canonicalSHA256("abc"); got != "sha256:abc" {
		t.Fatalf("canonical raw hash = %q", got)
	}
}

func TestExistingRuntimeProviderRejectsNonLoopback(t *testing.T) {
	_, err := (ExistingRuntimeProvider{
		CatalogDir: "../../../models/catalog",
		BaseURL:    "http://0.0.0.0:8081",
	}).Prepare(context.Background(), "thirdshift-tiny-chat-v1")
	if err == nil {
		t.Fatal("expected non-loopback runtime URL to be rejected")
	}
}

func TestAgentSessionLifecycleWithFakeCoordinator(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	validator, err := protocol.NewValidator("../../../packages/protocol/schemas")
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotHeartbeat := make(chan protocol.NodeHeartbeatPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_, helloData, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		hello, err := validator.ValidateEnvelope(helloData)
		if err != nil {
			t.Errorf("validate hello: %v", err)
			return
		}
		if hello.Type != protocol.TypeNodeHello {
			t.Errorf("hello type = %s", hello.Type)
			return
		}
		accepted, err := protocol.NewEnvelope("msg_01J0M000000000000000000000", protocol.TypeSessionAccepted, now, protocol.SessionAcceptedPayload{
			NodeID:                   "node_01J0M000000000000000000000",
			SessionID:                "sess_01J0M000000000000000000000",
			HeartbeatIntervalSeconds: 1,
			AcceptedAt:               now,
		})
		if err != nil {
			t.Errorf("accepted envelope: %v", err)
			return
		}
		acceptedData, err := validator.MarshalAndValidate(accepted)
		if err != nil {
			t.Errorf("validate accepted: %v", err)
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageText, acceptedData); err != nil {
			t.Errorf("write accepted: %v", err)
			return
		}
		_, heartbeatData, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("read heartbeat: %v", err)
			return
		}
		heartbeatEnvelope, err := validator.ValidateEnvelope(heartbeatData)
		if err != nil {
			t.Errorf("validate heartbeat: %v", err)
			return
		}
		var heartbeat protocol.NodeHeartbeatPayload
		if err := json.Unmarshal(heartbeatEnvelope.Payload, &heartbeat); err != nil {
			t.Errorf("decode heartbeat: %v", err)
			return
		}
		gotHeartbeat <- heartbeat
		cancel()
	}))
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Options{
			DataDir:           t.TempDir(),
			CoordinatorURL:    server.URL,
			NodeID:            "node_01J0M000000000000000000000",
			AccessToken:       "unused-by-fake-server",
			ModelID:           "thirdshift-tiny-chat-v1",
			HeartbeatInterval: 10 * time.Millisecond,
			Validator:         validator,
			Telemetry:         fakeTelemetry{},
			Runtime:           fakeRuntime{},
		})
	}()

	select {
	case heartbeat := <-gotHeartbeat:
		if heartbeat.NodeID != "node_01J0M000000000000000000000" || heartbeat.State != "AVAILABLE" {
			t.Fatalf("bad heartbeat: %#v", heartbeat)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for heartbeat")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("agent returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not stop after context cancellation")
	}
}

type fakeRuntime struct{}

func (fakeRuntime) Prepare(context.Context, string) (RuntimeStatus, error) {
	return RuntimeStatus{
		ModelID:     "thirdshift-tiny-chat-v1",
		RuntimeHash: "sha256:runtime",
		ModelHash:   "sha256:model",
	}, nil
}

type fakeTelemetry struct{}

func (fakeTelemetry) GPUStatus(context.Context) (protocol.GPUStatus, error) {
	return protocol.GPUStatus{
		Name:               "fake-gpu",
		VRAMTotalMB:        1,
		VRAMFreeMB:         1,
		TemperatureC:       1,
		PowerW:             1,
		PowerLimitW:        1,
		UtilizationPercent: 1,
	}, nil
}
