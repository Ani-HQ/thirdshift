//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/jobs"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
)

func TestM7PublicStatusCardAndListFieldsUseEmptyArrays(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()
	env.jobService.CreditHold = time.Millisecond
	configureM5Model(t, env, 1_000_000, 1_000_000, 500_000, 0)

	for _, endpoint := range []struct {
		name  string
		path  string
		token string
	}{
		{"internal nodes", "/internal/v1/nodes", "operator-token"},
		{"internal alerts", "/internal/v1/alerts", "operator-token"},
		{"internal models", "/internal/v1/models", "operator-token"},
		{"internal jobs", "/internal/v1/jobs", "operator-token"},
		{"internal ledger", "/internal/v1/ledger", "operator-token"},
		{"internal audit", "/internal/v1/audit", "operator-token"},
		{"public models", "/v1/models", env.apiKey},
		{"public status", "/v1/status", ""},
	} {
		result := getRawWithBearer(t, env, endpoint.path, endpoint.token)
		if result.status != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", endpoint.name, result.status, string(result.body))
		}
		assertNoNullListFields(t, endpoint.name, result.body)
	}

	status := getRawWithBearer(t, env, "/v1/status", "")
	if !bytes.Contains(status.body, []byte(`"connected_node_count"`)) ||
		!bytes.Contains(status.body, []byte(`"cities":[]`)) ||
		!bytes.Contains(status.body, []byte(`"models_available"`)) ||
		!bytes.Contains(status.body, []byte(`"jobs_completed_24h"`)) ||
		!bytes.Contains(status.body, []byte(`"output_tokens_served_total"`)) ||
		!bytes.Contains(status.body, []byte(`"estimated_gpu_hours_reused"`)) {
		t.Fatalf("public status missing launch fields: %s", string(status.body))
	}

	runtime := newM4Runtime("m7: ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(40), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(nodeSummary registration.NodeSummary) bool {
		return nodeSummary.State == "AVAILABLE" && nodeSummary.SessionStatus == "connected"
	})
	body, statusCode := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"card stats"}],"max_tokens":8,"stream":false}`), "m7-card")
	if statusCode != http.StatusOK {
		t.Fatalf("completion status=%d body=%s", statusCode, string(body))
	}
	var completion jobs.OpenAIResponse
	if err := json.Unmarshal(body, &completion); err != nil {
		t.Fatalf("decode completion: %v", err)
	}
	card := getRawWithBearer(t, env, "/v1/nodes/"+node.nodeID+"/card", "")
	if card.status != http.StatusOK {
		t.Fatalf("card status=%d body=%s", card.status, string(card.body))
	}
	if !bytes.Contains(card.body, []byte(`"jobs_accepted":1`)) || !bytes.Contains(card.body, []byte(`"credit_earned_microdollars"`)) {
		t.Fatalf("card missing accepted job stats for %s: %s", completion.Thirdshift.JobID, string(card.body))
	}
}

func getRawWithBearer(t *testing.T, env *m4Env, path, token string) operatorHTTPResult {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new GET %s: %v", path, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	return operatorHTTPResult{body: readAll(resp), status: resp.StatusCode}
}

func assertNoNullListFields(t *testing.T, name string, body []byte) {
	t.Helper()
	for _, forbidden := range [][]byte{
		[]byte(`"nodes":null`),
		[]byte(`"alerts":null`),
		[]byte(`"models":null`),
		[]byte(`"jobs":null`),
		[]byte(`"data":null`),
		[]byte(`"available_by_model":null`),
		[]byte(`"active_alerts":null`),
		[]byte(`"payout_batches":null`),
		[]byte(`"operator_actions":null`),
		[]byte(`"security_events":null`),
		[]byte(`"manifest_changes":null`),
		[]byte(`"cities":null`),
		[]byte(`"models_available":null`),
		[]byte(`"models":null`),
		[]byte(`"regions_online":null`),
	} {
		if bytes.Contains(body, forbidden) {
			t.Fatalf("%s response contains null list field %s: %s", name, string(forbidden), string(body))
		}
	}
}
