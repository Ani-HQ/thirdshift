package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/auth"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
	"nhooyr.io/websocket"
)

func TestHealthz(t *testing.T) {
	server := httptest.NewServer(NewMux("test-version"))
	defer server.Close()

	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status = %q, want ok", body.Status)
	}
	if body.Version != "test-version" {
		t.Fatalf("version = %q, want test-version", body.Version)
	}
}

func TestNodesListRequiresOperatorToken(t *testing.T) {
	server := httptest.NewServer(NewMuxWithOptions(Options{
		Version:       "test",
		SessionStore:  &fakeSessionStore{},
		OperatorToken: "operator-token",
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/internal/v1/nodes")
	if err != nil {
		t.Fatalf("GET nodes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want 401", resp.StatusCode)
	}
}

func TestHeartbeatIntervalSecondsClampsSubsecondIntervals(t *testing.T) {
	if got := heartbeatIntervalSeconds(20 * time.Millisecond); got != 1 {
		t.Fatalf("heartbeat interval seconds = %d, want 1", got)
	}
	if got := heartbeatIntervalSeconds(2500 * time.Millisecond); got != 3 {
		t.Fatalf("heartbeat interval seconds = %d, want 3", got)
	}
}

func TestWriteJSONNormalizesNilLists(t *testing.T) {
	type nested struct {
		Items []string `json:"items"`
	}
	body := struct {
		Nodes  []registration.NodeSummary `json:"nodes"`
		Nested nested                     `json:"nested"`
		Raw    json.RawMessage            `json:"raw"`
	}{
		Raw: json.RawMessage(`{"kept":true}`),
	}
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, body)
	got := recorder.Body.String()
	for _, want := range []string{`"nodes":[]`, `"items":[]`, `"raw":{"kept":true}`} {
		if !strings.Contains(got, want) {
			t.Fatalf("response %s missing %s", got, want)
		}
	}
	if strings.Contains(got, `null`) {
		t.Fatalf("response contains null: %s", got)
	}
}

func TestSessionAcceptsHelloAndRecordsHeartbeat(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	validator, err := protocol.NewValidator("../../../packages/protocol/schemas")
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	store := &fakeSessionStore{}
	signer := auth.TokenSigner{
		Secret: []byte("test-secret"),
		Now:    func() time.Time { return now },
		TTL:    time.Hour,
	}
	token, _, err := signer.Issue("node_01J0M000000000000000000000")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	server := httptest.NewServer(NewMuxWithOptions(Options{
		Version:           "test",
		SessionStore:      store,
		TokenSigner:       signer,
		ProtocolValidator: validator,
		HeartbeatInterval: time.Second,
		Now:               func() time.Time { return now },
	}))
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	conn, _, err := websocket.Dial(context.Background(), "ws"+server.URL[len("http"):]+"/v1/node/session", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hello, err := protocol.NewEnvelope("msg_01J0M000000000000000000000", protocol.TypeNodeHello, now, protocol.NodeHelloPayload{
		NodeID:                    "node_01J0M000000000000000000000",
		AgentVersion:              "test",
		SupportedProtocolVersions: []string{"1.0"},
		Capabilities:              []string{"chat_completions"},
	})
	if err != nil {
		t.Fatalf("hello envelope: %v", err)
	}
	helloData, err := validator.MarshalAndValidate(hello)
	if err != nil {
		t.Fatalf("validate hello: %v", err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, helloData); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	_, acceptedData, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("read accepted: %v", err)
	}
	accepted, err := validator.ValidateEnvelope(acceptedData)
	if err != nil {
		t.Fatalf("validate accepted: %v", err)
	}
	if accepted.Type != protocol.TypeSessionAccepted {
		t.Fatalf("accepted type = %s", accepted.Type)
	}

	heartbeat, err := protocol.NewEnvelope("msg_01J0M000000000000000000001", protocol.TypeNodeHeartbeat, now, protocol.NodeHeartbeatPayload{
		NodeID:      "node_01J0M000000000000000000000",
		Sequence:    1,
		State:       "AVAILABLE",
		ModelID:     "thirdshift-tiny-chat-v1",
		RuntimeHash: "sha256:runtime",
		ModelHash:   "sha256:model",
		GPU: protocol.GPUStatus{
			Name:               "fake-gpu",
			VRAMTotalMB:        1,
			VRAMFreeMB:         1,
			TemperatureC:       1,
			PowerW:             1,
			PowerLimitW:        1,
			UtilizationPercent: 1,
		},
		ActiveJobID:   nil,
		UptimeSeconds: 1,
		Timestamp:     now,
	})
	if err != nil {
		t.Fatalf("heartbeat envelope: %v", err)
	}
	heartbeatData, err := validator.MarshalAndValidate(heartbeat)
	if err != nil {
		t.Fatalf("validate heartbeat: %v", err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, heartbeatData); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		if store.heartbeat.NodeID != "" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("heartbeat was not recorded")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if store.heartbeat.State != "AVAILABLE" || store.sessionID == "" {
		t.Fatalf("bad recorded session heartbeat: session=%q heartbeat=%#v", store.sessionID, store.heartbeat)
	}
}

type fakeSessionStore struct {
	sessionID string
	heartbeat protocol.NodeHeartbeatPayload
}

func (f *fakeSessionStore) OpenSession(_ context.Context, _, _, _ string, _ time.Time) (string, error) {
	f.sessionID = "sess_01J0M000000000000000000000"
	return f.sessionID, nil
}

func (f *fakeSessionStore) RecordHeartbeat(_ context.Context, sessionID string, heartbeat protocol.NodeHeartbeatPayload, _ time.Time) error {
	f.sessionID = sessionID
	f.heartbeat = heartbeat
	return nil
}

func (f *fakeSessionStore) RecordStateChanged(context.Context, protocol.NodeStateChangedPayload, time.Time) error {
	return nil
}

func (f *fakeSessionStore) RecordSafetyEvent(context.Context, protocol.NodeSafetyEventPayload, time.Time) error {
	return nil
}

func (f *fakeSessionStore) CloseSession(context.Context, string, string, time.Time) error {
	return nil
}

func (f *fakeSessionStore) ListNodes(context.Context) ([]registration.NodeSummary, error) {
	return []registration.NodeSummary{{ID: "node_01J0M000000000000000000000", State: "AVAILABLE", SessionStatus: "connected"}}, nil
}

func (f *fakeSessionStore) PublicKeyForNode(context.Context, string) (string, error) {
	return "", nil
}
