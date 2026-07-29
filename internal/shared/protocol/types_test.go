package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEnvelopeRoundTripForRequiredPayloads(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	for typ, payload := range samplePayloads(now) {
		t.Run(string(typ), func(t *testing.T) {
			envelope, err := NewEnvelope("msg_01J0M000000000000000000000", typ, now, payload)
			if err != nil {
				t.Fatalf("new envelope: %v", err)
			}

			data, err := Marshal(envelope)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			gotEnvelope, err := Unmarshal(data)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if gotEnvelope.Type != typ {
				t.Fatalf("type = %q, want %q", gotEnvelope.Type, typ)
			}

			target := reflect.New(reflect.TypeOf(payload)).Interface()
			if err := json.Unmarshal(gotEnvelope.Payload, target); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			got := reflect.ValueOf(target).Elem().Interface()
			if !reflect.DeepEqual(got, payload) {
				t.Fatalf("payload mismatch\n got: %#v\nwant: %#v", got, payload)
			}
		})
	}
}

func TestRequiredMessageTypesAreUnique(t *testing.T) {
	seen := map[MessageType]bool{}
	for _, typ := range RequiredMessageTypes {
		if seen[typ] {
			t.Fatalf("duplicate message type %q", typ)
		}
		seen[typ] = true
	}
	if len(seen) != 20 {
		t.Fatalf("message type count = %d, want 20", len(seen))
	}
}

func samplePayloads(now time.Time) map[MessageType]any {
	temp := 82
	power := 205
	drainBy := now.Add(30 * time.Second)
	temperature := 0.2
	seed := int64(42)
	return map[MessageType]any{
		TypeNodeHello: NodeHelloPayload{
			NodeID:                    "node_01J0M000000000000000000000",
			AgentVersion:              "0.1.0-alpha.0",
			SupportedProtocolVersions: []string{"1.0"},
			Capabilities:              []string{"chat_completions"},
			Hostname:                  "host-01",
		},
		TypeNodeHeartbeat: NodeHeartbeatPayload{
			NodeID:      "node_01J0M000000000000000000000",
			Sequence:    1832,
			State:       "AVAILABLE",
			ModelID:     "thirdshift-small-chat-v1",
			RuntimeHash: "sha256:91aa",
			ModelHash:   "sha256:6b2f",
			GPU: GPUStatus{
				Name:               "NVIDIA GeForce RTX 4070",
				VRAMTotalMB:        12282,
				VRAMFreeMB:         10400,
				TemperatureC:       62,
				PowerW:             146,
				PowerLimitW:        180,
				UtilizationPercent: 4,
			},
			ActiveJobID:   nil,
			UptimeSeconds: 19420,
			Timestamp:     now,
		},
		TypeNodeStateChanged: NodeStateChangedPayload{
			NodeID:        "node_01J0M000000000000000000000",
			PreviousState: "PREPARING_MODEL",
			State:         "AVAILABLE",
			Reason:        "model_ready",
			Timestamp:     now,
		},
		TypeModelDownloadProgress: ModelDownloadProgressPayload{
			NodeID:          "node_01J0M000000000000000000000",
			ModelID:         "thirdshift-small-chat-v1",
			ArtifactID:      "artifact_01J0M000000000000000000000",
			BytesDownloaded: 512,
			BytesTotal:      1024,
			Percent:         50,
			Timestamp:       now,
		},
		TypeModelReady: ModelReadyPayload{
			NodeID:      "node_01J0M000000000000000000000",
			ModelID:     "thirdshift-small-chat-v1",
			ModelHash:   "sha256:6b2f",
			RuntimeHash: "sha256:91aa",
			LoadedAt:    now,
		},
		TypeJobAccepted: JobAcceptedPayload{
			JobID:      "job_01J0M000000000000000000000",
			AttemptID:  "att_01J0M000000000000000000000",
			AcceptedAt: now,
		},
		TypeJobRejected: JobRejectedPayload{
			JobID:      "job_01J0M000000000000000000000",
			AttemptID:  "att_01J0M000000000000000000000",
			ReasonCode: "model_not_ready",
			Message:    "model is not loaded",
			Retryable:  true,
			RejectedAt: now,
		},
		TypeJobStarted: JobStartedPayload{
			JobID:     "job_01J0M000000000000000000000",
			AttemptID: "att_01J0M000000000000000000000",
			StartedAt: now,
		},
		TypeJobCompleted: JobCompletedPayload{
			JobID:       "job_01J0M000000000000000000000",
			AttemptID:   "att_01J0M000000000000000000000",
			ModelID:     "thirdshift-small-chat-v1",
			RuntimeHash: "sha256:91aa",
			ModelHash:   "sha256:6b2f",
			Usage: Usage{
				PromptTokens:     42,
				CompletionTokens: 118,
				TotalTokens:      160,
			},
			DurationMillis: 1200,
			FinishReason:   "stop",
			CompletedAt:    now,
		},
		TypeJobFailed: JobFailedPayload{
			JobID:     "job_01J0M000000000000000000000",
			AttemptID: "att_01J0M000000000000000000000",
			ErrorCode: "runtime_error",
			Message:   "runtime exited",
			Retryable: true,
			FailedAt:  now,
		},
		TypeNodeSafetyEvent: NodeSafetyEventPayload{
			NodeID:       "node_01J0M000000000000000000000",
			EventCode:    "over_temperature",
			Severity:     "warning",
			Message:      "temperature exceeded configured limit",
			TemperatureC: &temp,
			PowerW:       &power,
			OccurredAt:   now,
		},
		TypeNodeLogEvent: NodeLogEventPayload{
			NodeID:  "node_01J0M000000000000000000000",
			Level:   "info",
			Message: "job lifecycle event",
			Fields: map[string]string{
				"job_id": "job_01J0M000000000000000000000",
			},
			LoggedAt: now,
		},
		TypeSessionAccepted: SessionAcceptedPayload{
			NodeID:                   "node_01J0M000000000000000000000",
			SessionID:                "sess_01J0M000000000000000000000",
			HeartbeatIntervalSeconds: 15,
			AcceptedAt:               now,
		},
		TypeNodeConfigUpdated: NodeConfigUpdatedPayload{
			ConfigVersion:            "cfg_01J0M000000000000000000000",
			HeartbeatIntervalSeconds: 15,
			MaxJobSeconds:            120,
			UpdatedAt:                now,
		},
		TypeModelAssign: ModelAssignPayload{
			AssignmentID: "assign_01J0M000000000000000000000",
			ModelID:      "thirdshift-small-chat-v1",
			VersionID:    "mv_01J0M000000000000000000000",
			ManifestURL:  "https://models.thirdshift.example/catalog/thirdshift-small-chat-v1.yaml",
			ManifestHash: "sha256:abcdef",
			AssignedAt:   now,
		},
		TypeModelUnload: ModelUnloadPayload{
			ModelID:    "thirdshift-small-chat-v1",
			Reason:     "operator_requested",
			DeadlineAt: now.Add(time.Minute),
		},
		TypeJobOffer: JobOfferPayload{
			JobID:          "job_01J0M000000000000000000000",
			AttemptID:      "att_01J0M000000000000000000000",
			LeaseExpiresAt: now.Add(10 * time.Second),
			DeadlineAt:     now.Add(2 * time.Minute),
			ModelID:        "thirdshift-small-chat-v1",
			Request: ChatRequest{
				Messages: []ChatMessage{
					{Role: "user", Content: "Summarize this public text."},
				},
				Temperature: &temperature,
				MaxTokens:   512,
				Seed:        &seed,
				Stream:      false,
			},
			Verification: JobVerification{
				Kind:        "standard",
				ChallengeID: nil,
			},
		},
		TypeJobCancel: JobCancelPayload{
			JobID:       "job_01J0M000000000000000000000",
			AttemptID:   "att_01J0M000000000000000000000",
			Reason:      "deadline_exceeded",
			CancelledAt: now,
		},
		TypeNodeDrain: NodeDrainPayload{
			Reason:  "operator_requested",
			DrainBy: &drainBy,
		},
		TypeRuntimeUpdateAvailable: RuntimeUpdateAvailablePayload{
			RuntimeID:       "runtime_01J0M000000000000000000000",
			Version:         "llama-cpp-thirdshift-2026-07-01",
			ManifestURL:     "https://releases.thirdshift.example/runtime/manifest.json",
			BinarySHA256:    "sha256:91aa",
			Required:        false,
			ReleaseNotesURL: "https://releases.thirdshift.example/runtime/notes",
			AvailableAt:     now,
		},
	}
}
