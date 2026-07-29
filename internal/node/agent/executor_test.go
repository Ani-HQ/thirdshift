package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/anianroid/thirdshift/internal/shared/nodeauth"
	"github.com/anianroid/thirdshift/internal/shared/protocol"
	"nhooyr.io/websocket"
)

func TestAgentExecutesOfferAndSignsCompletion(t *testing.T) {
	validator := testValidator(t)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"role": "assistant", "content": "routed fake completion"},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
		})
	}))
	defer runtimeServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completedCh := make(chan protocol.JobCompletedPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		readEnvelope(t, validator, conn, protocol.TypeNodeHello)
		writeEnvelopeForTest(t, validator, conn, protocol.TypeSessionAccepted, protocol.SessionAcceptedPayload{
			NodeID:                   testNodeID,
			SessionID:                testSessionID,
			HeartbeatIntervalSeconds: 1,
			AcceptedAt:               testNow,
		})
		writeEnvelopeForTest(t, validator, conn, protocol.TypeJobOffer, validOffer(testNow.Add(time.Second)))
		for {
			envelope := readAnyEnvelope(t, validator, conn)
			switch envelope.Type {
			case protocol.TypeNodeHeartbeat, protocol.TypeJobAccepted, protocol.TypeJobStarted:
			case protocol.TypeJobCompleted:
				var payload protocol.JobCompletedPayload
				if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
					t.Errorf("decode completion: %v", err)
					return
				}
				completedCh <- payload
				cancel()
				return
			default:
				t.Errorf("unexpected envelope type %s", envelope.Type)
				return
			}
		}
	}))
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Options{
			DataDir:           t.TempDir(),
			CoordinatorURL:    server.URL,
			NodeID:            testNodeID,
			AccessToken:       "unused",
			ModelID:           "thirdshift-tiny-chat-v1",
			HeartbeatInterval: time.Hour,
			Validator:         validator,
			Telemetry:         fakeTelemetry{},
			Runtime: executorRuntime{
				status: RuntimeStatus{
					ModelID:     "thirdshift-tiny-chat-v1",
					RuntimeHash: "sha256:runtime",
					ModelHash:   "sha256:model",
					BaseURL:     runtimeServer.URL,
				},
			},
			PrivateKey: privateKey,
			Now:        func() time.Time { return testNow },
		})
	}()

	var completed protocol.JobCompletedPayload
	select {
	case completed = <-completedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for completion")
	}
	if completed.Message == nil || completed.Message.Content != "routed fake completion" {
		t.Fatalf("bad completion message: %#v", completed.Message)
	}
	if completed.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", completed.Usage)
	}
	if err := nodeauth.VerifyJobCompleted(publicKey, completed); err != nil {
		t.Fatalf("verify completion signature: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("agent returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop")
	}
}

func TestAgentRejectsExpiredOfferAndMissingHashes(t *testing.T) {
	tests := []struct {
		name   string
		offer  protocol.JobOfferPayload
		status RuntimeStatus
	}{
		{
			name:  "expired lease",
			offer: validOffer(testNow.Add(-time.Second)),
			status: RuntimeStatus{
				ModelID:     "thirdshift-tiny-chat-v1",
				RuntimeHash: "sha256:runtime",
				ModelHash:   "sha256:model",
				BaseURL:     "http://127.0.0.1:1",
			},
		},
		{
			name:  "missing hashes",
			offer: validOffer(testNow.Add(time.Second)),
			status: RuntimeStatus{
				ModelID: "thirdshift-tiny-chat-v1",
				BaseURL: "http://127.0.0.1:1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := testValidator(t)
			_, privateKey, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatalf("generate key: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rejectedCh := make(chan protocol.JobRejectedPayload, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Errorf("accept websocket: %v", err)
					return
				}
				defer conn.Close(websocket.StatusNormalClosure, "")
				readEnvelope(t, validator, conn, protocol.TypeNodeHello)
				writeEnvelopeForTest(t, validator, conn, protocol.TypeSessionAccepted, protocol.SessionAcceptedPayload{
					NodeID:                   testNodeID,
					SessionID:                testSessionID,
					HeartbeatIntervalSeconds: 1,
					AcceptedAt:               testNow,
				})
				writeEnvelopeForTest(t, validator, conn, protocol.TypeJobOffer, tt.offer)
				for {
					envelope := readAnyEnvelope(t, validator, conn)
					switch envelope.Type {
					case protocol.TypeNodeHeartbeat:
					case protocol.TypeJobRejected:
						var payload protocol.JobRejectedPayload
						if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
							t.Errorf("decode rejection: %v", err)
							return
						}
						rejectedCh <- payload
						cancel()
						return
					default:
						t.Errorf("unexpected envelope type %s", envelope.Type)
						return
					}
				}
			}))
			defer server.Close()

			errCh := make(chan error, 1)
			go func() {
				errCh <- Run(ctx, Options{
					DataDir:           t.TempDir(),
					CoordinatorURL:    server.URL,
					NodeID:            testNodeID,
					AccessToken:       "unused",
					ModelID:           "thirdshift-tiny-chat-v1",
					HeartbeatInterval: time.Hour,
					Validator:         validator,
					Telemetry:         fakeTelemetry{},
					Runtime:           executorRuntime{status: tt.status},
					PrivateKey:        privateKey,
					Now:               func() time.Time { return testNow },
				})
			}()
			select {
			case rejected := <-rejectedCh:
				if rejected.JobID != tt.offer.JobID || rejected.AttemptID != tt.offer.AttemptID {
					t.Fatalf("bad rejection payload: %#v", rejected)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("timed out waiting for rejection")
			}
			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("agent returned error: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("agent did not stop")
			}
		})
	}
}

type executorRuntime struct {
	status RuntimeStatus
}

func (r executorRuntime) Prepare(context.Context, string) (RuntimeStatus, error) {
	return r.status, nil
}

func validOffer(leaseExpiresAt time.Time) protocol.JobOfferPayload {
	return protocol.JobOfferPayload{
		JobID:          testJobID,
		AttemptID:      testAttemptID,
		LeaseExpiresAt: leaseExpiresAt,
		DeadlineAt:     time.Now().UTC().Add(time.Minute),
		ModelID:        "thirdshift-tiny-chat-v1",
		Request: protocol.ChatRequest{
			Messages:  []protocol.ChatMessage{{Role: "user", Content: "hello"}},
			MaxTokens: 8,
			Stream:    false,
		},
		Verification: protocol.JobVerification{Kind: "standard"},
	}
}

func testValidator(t *testing.T) *protocol.Validator {
	t.Helper()
	validator, err := protocol.NewValidator(filepath.Join("..", "..", "..", "packages", "protocol", "schemas"))
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	return validator
}

func readEnvelope(t *testing.T, validator *protocol.Validator, conn *websocket.Conn, typ protocol.MessageType) protocol.Envelope {
	t.Helper()
	envelope := readAnyEnvelope(t, validator, conn)
	if envelope.Type != typ {
		t.Fatalf("envelope type = %s, want %s", envelope.Type, typ)
	}
	return envelope
}

func readAnyEnvelope(t *testing.T, validator *protocol.Validator, conn *websocket.Conn) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	envelope, err := validator.ValidateEnvelope(data)
	if err != nil {
		t.Fatalf("validate envelope: %v", err)
	}
	return envelope
}

func writeEnvelopeForTest(t *testing.T, validator *protocol.Validator, conn *websocket.Conn, typ protocol.MessageType, payload any) {
	t.Helper()
	envelope, err := protocol.NewEnvelope("msg_01J0M00000000000000000000T", typ, testNow, payload)
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	data, err := validator.MarshalAndValidate(envelope)
	if err != nil {
		t.Fatalf("validate envelope: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
}

var (
	testNow       = time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	testNodeID    = "node_01J0M000000000000000000000"
	testSessionID = "sess_01J0M000000000000000000000"
	testJobID     = "job_01J0M000000000000000000000"
	testAttemptID = "att_01J0M000000000000000000000"
)
