//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	operatorstore "github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
)

func TestPublicCatalogStatusModelsPricingMedianAndRegions(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()
	configureM5Model(t, env, 1_000_000, 2_000_000, 500_000, 0)
	seedOfflineCatalogModel(t, env)
	seedAcceptedResultForMedian(t, env, "thirdshift-tiny-chat-v1", 30, 2000)
	// The tiny model ships as listing.status hidden. Listing it for the live
	// assertions keeps this test focused on measured supply; the hidden path
	// is asserted at the end and in
	// TestPublicStatusListsWaitlistModelsAndHidesInternalModel.
	setListingStatus(t, env, "thirdshift-tiny-chat-v1", "live")

	runtime := newM4Runtime("catalog: ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(40), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})
	if err := env.operator.SetFleetRegion(env.ctx, "fleet_01J0M000000000000000000000", "in-south", "catalog test", time.Now().UTC()); err != nil {
		t.Fatalf("set fleet region: %v", err)
	}

	status := publicStatus(t, env, "X-Geo-Region", "eu-west")
	if status.RequesterRegion == nil || *status.RequesterRegion != "eu-west" {
		t.Fatalf("requester_region = %v, want eu-west", status.RequesterRegion)
	}
	tiny := findPublicModel(t, status, "thirdshift-tiny-chat-v1")
	if tiny.Availability.State != "available" || tiny.Availability.AvailableNodes != 1 {
		t.Fatalf("tiny availability = %#v, want available with one node", tiny.Availability)
	}
	if tiny.Price.InputPerMillionMicrodollars != 1_000_000 || tiny.Price.OutputPerMillionMicrodollars != 2_000_000 {
		t.Fatalf("tiny prices = %#v", tiny.Price)
	}
	if tiny.TypicalOutputTokensPerSecond == nil || *tiny.TypicalOutputTokensPerSecond != 15 {
		t.Fatalf("tiny median speed = %v, want 15", tiny.TypicalOutputTokensPerSecond)
	}
	if strings.Join(tiny.Regions, ",") != "in-south" {
		t.Fatalf("tiny regions = %#v, want in-south", tiny.Regions)
	}
	if strings.Join(status.RegionsOnline, ",") != "in-south" {
		t.Fatalf("regions_online = %#v, want in-south", status.RegionsOnline)
	}

	if err := env.operator.SetNodeRegion(env.ctx, node.nodeID, "eu-west", "catalog test override", time.Now().UTC()); err != nil {
		t.Fatalf("set node region: %v", err)
	}
	statusFromStore, err := env.operator.PublicStatus(env.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("store public status: %v", err)
	}
	tiny = findPublicModel(t, statusFromStore, "thirdshift-tiny-chat-v1")
	if strings.Join(tiny.Regions, ",") != "eu-west" {
		t.Fatalf("node override regions = %#v, want eu-west", tiny.Regions)
	}

	offline := findPublicModel(t, status, "thirdshift-offline-chat-v1")
	if offline.Availability.State != "offline" || offline.Availability.AvailableNodes != 0 || len(offline.Regions) != 0 {
		t.Fatalf("offline model availability = %#v regions=%#v", offline.Availability, offline.Regions)
	}
	if offline.MarketComparison != nil {
		t.Fatalf("offline fixture model has a market comparison: %#v", offline.MarketComparison)
	}

	setListingStatus(t, env, "thirdshift-tiny-chat-v1", "hidden")
	hiddenStatus, err := env.operator.PublicStatus(env.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("store public status after hiding tiny: %v", err)
	}
	for _, model := range hiddenStatus.Models {
		if model.ModelID == "thirdshift-tiny-chat-v1" {
			t.Fatal("hidden model is still published on /v1/status")
		}
	}
}

func TestPublicStatusListsWaitlistModelsAndHidesInternalModel(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	status := publicStatus(t, env, "", "")

	for _, model := range status.Models {
		if model.ModelID == "thirdshift-tiny-chat-v1" {
			t.Fatalf("hidden internal model appears in public status: %#v", model)
		}
	}

	for modelID, expectedSpeed := range map[string]float64{
		"qwen2.5-7b-instruct":       30,
		"qwen2.5-coder-7b-instruct": 30,
		"llama-3.2-3b-instruct":     60,
	} {
		model := findPublicModel(t, status, modelID)
		if model.ListingStatus != "waitlist" {
			t.Fatalf("%s listing_status = %q, want waitlist", modelID, model.ListingStatus)
		}
		if model.Availability.State != "waitlist" {
			t.Fatalf("%s availability state = %q, want waitlist", modelID, model.Availability.State)
		}
		if model.Availability.AvailableNodes != 0 {
			t.Fatalf("%s reports %d available nodes with no supply", modelID, model.Availability.AvailableNodes)
		}
		if len(model.Regions) != 0 {
			t.Fatalf("%s reports regions %#v with no supply", modelID, model.Regions)
		}
		if model.TypicalOutputTokensPerSecond != nil {
			t.Fatalf("%s reports a measured speed with no supply: %v", modelID, *model.TypicalOutputTokensPerSecond)
		}
		if model.ExpectedOutputTokensPerSecond == nil || *model.ExpectedOutputTokensPerSecond != expectedSpeed {
			t.Fatalf("%s expected speed = %v, want %v", modelID, model.ExpectedOutputTokensPerSecond, expectedSpeed)
		}
		if model.Description == "" || model.Description == model.DisplayName {
			t.Fatalf("%s has no listing description: %q", modelID, model.Description)
		}
		if model.MarketComparison == nil {
			t.Fatalf("%s is missing its market comparison", modelID)
		}
		if model.MarketComparison.TypicalInputPerMillionMicrodollars <= model.Price.InputPerMillionMicrodollars {
			t.Fatalf("%s market comparison is not above our price: %#v", modelID, model.MarketComparison)
		}
		if model.MarketComparison.SourceNote == "" {
			t.Fatalf("%s market comparison has no source note", modelID)
		}
	}

	qwen := findPublicModel(t, status, "qwen2.5-7b-instruct")
	if qwen.Price.InputPerMillionMicrodollars != 30_000 || qwen.Price.OutputPerMillionMicrodollars != 80_000 {
		t.Fatalf("qwen price = %#v, want 30000/80000 microdollars", qwen.Price)
	}
	if qwen.MarketComparison.TypicalInputPerMillionMicrodollars != 40_000 ||
		qwen.MarketComparison.TypicalOutputPerMillionMicrodollars != 100_000 {
		t.Fatalf("qwen market comparison = %#v, want 40000/100000 microdollars", qwen.MarketComparison)
	}
}

func TestPublicAccessApplicationRequiresUseCaseAndAcknowledgment(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	postWaitlistRaw(t, env, []byte(`{"email":"dev@example.com","data_ack":true}`), http.StatusBadRequest)
	postWaitlistRaw(t, env, []byte(`{"email":"dev@example.com","use_case":"Batch summaries"}`), http.StatusBadRequest)
	postWaitlistRaw(t, env, []byte(`{"email":"dev@example.com","use_case":"Batch summaries","data_ack":false}`), http.StatusBadRequest)
	postWaitlistRaw(t, env, []byte(`{"email":"not-an-email","use_case":"Batch summaries","data_ack":true}`), http.StatusBadRequest)
	postWaitlistRaw(t, env, []byte(`{"email":"dev@example.com","use_case":"Batch summaries","data_ack":true,"expected_volume":"loads"}`), http.StatusBadRequest)

	postApplication(t, env, accessApplication{
		Email:          "dev@example.com",
		Name:           "Dev Example",
		UseCase:        "Nightly batch summaries for an internal tool",
		ExpectedVolume: "1m_10m",
		DataAck:        true,
		ModelID:        "qwen2.5-7b-instruct",
	}, http.StatusOK)

	signups := listApplications(t, env)
	if len(signups) != 1 {
		t.Fatalf("waitlist rows = %d, want 1", len(signups))
	}
	signup := signups[0]
	if signup.Email != "dev@example.com" || signup.Name != "Dev Example" ||
		signup.UseCase != "Nightly batch summaries for an internal tool" ||
		signup.ExpectedVolume != "1m_10m" || !signup.DataAck || signup.ModelID != "qwen2.5-7b-instruct" {
		t.Fatalf("stored application = %#v", signup)
	}
	if signup.LastAppliedAt.IsZero() {
		t.Fatal("last_applied_at was not recorded")
	}

	exported := operatorGetRaw(t, env, "/internal/v1/waitlist/export", "operator-token")
	if exported.status != http.StatusOK {
		t.Fatalf("waitlist export status=%d body=%s", exported.status, string(exported.body))
	}
	csvBody := string(exported.body)
	if !strings.HasPrefix(csvBody, "id,email,name,use_case,expected_volume,data_ack,model_id,source,created_at,last_applied_at") {
		t.Fatalf("waitlist CSV header = %q", csvBody)
	}
	for _, want := range []string{"Dev Example", "1m_10m", "true", "qwen2.5-7b-instruct"} {
		if !strings.Contains(csvBody, want) {
			t.Fatalf("waitlist CSV missing %q: %s", want, csvBody)
		}
	}
}

// A returning applicant with better answers must overwrite their previous
// application rather than be silently dropped, and applying for a second model
// must not overwrite the first.
func TestPublicApplicationUpsertsPerEmailAndModel(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	postApplication(t, env, accessApplication{
		Email:          "dev@example.com",
		Name:           "Dev",
		UseCase:        "First pass, vague",
		ExpectedVolume: "lt_1m",
		DataAck:        true,
		ModelID:        "qwen2.5-7b-instruct",
	}, http.StatusOK)
	first := requireApplication(t, listApplications(t, env), "dev@example.com", "qwen2.5-7b-instruct")

	// Same email and model, better answers, and a mixed-case address to prove
	// normalization still lands on the same row.
	postApplication(t, env, accessApplication{
		Email:          "DEV@example.com",
		Name:           "Dev Example",
		UseCase:        "Revised: nightly evaluation harness",
		ExpectedVolume: "10m_100m",
		DataAck:        true,
		ModelID:        "qwen2.5-7b-instruct",
	}, http.StatusOK)

	signups := listApplications(t, env)
	if len(signups) != 1 {
		t.Fatalf("re-application created %d rows, want 1: %#v", len(signups), signups)
	}
	updated := requireApplication(t, signups, "dev@example.com", "qwen2.5-7b-instruct")
	if updated.ID != first.ID {
		t.Fatalf("re-application replaced the row id %s with %s", first.ID, updated.ID)
	}
	if updated.Name != "Dev Example" || updated.UseCase != "Revised: nightly evaluation harness" || updated.ExpectedVolume != "10m_100m" {
		t.Fatalf("re-application did not overwrite the answers: %#v", updated)
	}
	if !updated.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at moved from %s to %s", first.CreatedAt, updated.CreatedAt)
	}
	if !updated.LastAppliedAt.After(first.LastAppliedAt) {
		t.Fatalf("last_applied_at did not advance: %s then %s", first.LastAppliedAt, updated.LastAppliedAt)
	}

	// A second model is a separate application, not an overwrite.
	postApplication(t, env, accessApplication{
		Email:   "dev@example.com",
		UseCase: "Coder model for refactors",
		DataAck: true,
		ModelID: "qwen2.5-coder-7b-instruct",
	}, http.StatusOK)
	signups = listApplications(t, env)
	if len(signups) != 2 {
		t.Fatalf("second model produced %d rows, want 2: %#v", len(signups), signups)
	}
	kept := requireApplication(t, signups, "dev@example.com", "qwen2.5-7b-instruct")
	if kept.UseCase != "Revised: nightly evaluation harness" {
		t.Fatalf("applying for a second model overwrote the first: %#v", kept)
	}
	coder := requireApplication(t, signups, "dev@example.com", "qwen2.5-coder-7b-instruct")
	if coder.UseCase != "Coder model for refactors" {
		t.Fatalf("second model application = %#v", coder)
	}
}

// An application with no model named is that applicant's general application.
// Uniqueness is NULLS NOT DISTINCT so resubmitting updates it rather than
// piling up a new row every time.
func TestPublicGeneralApplicationUpsertsWithoutModel(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	postApplication(t, env, accessApplication{
		Email:   "dev@example.com",
		UseCase: "Exploring the platform",
		DataAck: true,
	}, http.StatusOK)
	postApplication(t, env, accessApplication{
		Email:   "dev@example.com",
		UseCase: "Revised: exploring with a real workload",
		DataAck: true,
	}, http.StatusOK)

	signups := listApplications(t, env)
	if len(signups) != 1 {
		t.Fatalf("general re-application created %d rows, want 1: %#v", len(signups), signups)
	}
	if signups[0].ModelID != "" {
		t.Fatalf("general application recorded model %q", signups[0].ModelID)
	}
	if signups[0].UseCase != "Revised: exploring with a real workload" {
		t.Fatalf("general re-application did not overwrite: %#v", signups[0])
	}
}

func listApplications(t *testing.T, env *m4Env) []operatorstore.WaitlistSignup {
	t.Helper()
	listed := operatorGetRaw(t, env, "/internal/v1/waitlist", "operator-token")
	if listed.status != http.StatusOK {
		t.Fatalf("waitlist list status=%d body=%s", listed.status, string(listed.body))
	}
	var listResponse struct {
		Signups []operatorstore.WaitlistSignup `json:"signups"`
	}
	if err := json.Unmarshal(listed.body, &listResponse); err != nil {
		t.Fatalf("decode waitlist list: %v", err)
	}
	return listResponse.Signups
}

func requireApplication(t *testing.T, signups []operatorstore.WaitlistSignup, email, modelID string) operatorstore.WaitlistSignup {
	t.Helper()
	for _, signup := range signups {
		if signup.Email == email && signup.ModelID == modelID {
			return signup
		}
	}
	t.Fatalf("no application for %s / %q in %#v", email, modelID, signups)
	return operatorstore.WaitlistSignup{}
}

func TestPublicApplicationRateLimit(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	for i := 0; i < 6; i++ {
		want := http.StatusOK
		if i >= 5 {
			want = http.StatusTooManyRequests
		}
		postApplication(t, env, accessApplication{
			Email:   "rate" + string(rune('a'+i)) + "@example.com",
			UseCase: "Load testing the application form",
			DataAck: true,
		}, want)
	}
}

func setListingStatus(t *testing.T, env *m4Env, modelID, listingStatus string) {
	t.Helper()
	if _, err := env.pool.Exec(env.ctx, "UPDATE models SET listing_status = $2 WHERE id = $1", modelID, listingStatus); err != nil {
		t.Fatalf("set listing status for %s: %v", modelID, err)
	}
}

func publicStatus(t *testing.T, env *m4Env, header, value string) operatorstore.PublicStatus {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.server.URL+"/v1/status", nil)
	if err != nil {
		t.Fatalf("new status request: %v", err)
	}
	if header != "" {
		req.Header.Set(header, value)
	}
	resp, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET public status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status response=%d body=%s", resp.StatusCode, readAll(resp))
	}
	var status operatorstore.PublicStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode public status: %v", err)
	}
	return status
}

func findPublicModel(t *testing.T, status operatorstore.PublicStatus, modelID string) operatorstore.PublicModelStatus {
	t.Helper()
	for _, model := range status.Models {
		if model.ModelID == modelID {
			return model
		}
	}
	t.Fatalf("model %s not found in public status: %#v", modelID, status.Models)
	return operatorstore.PublicModelStatus{}
}

type accessApplication struct {
	Email          string `json:"email"`
	Name           string `json:"name,omitempty"`
	UseCase        string `json:"use_case"`
	ExpectedVolume string `json:"expected_volume,omitempty"`
	DataAck        bool   `json:"data_ack"`
	ModelID        string `json:"model_id,omitempty"`
}

type accessApplicationResponse struct {
	Status string `json:"status"`
}

func postApplication(t *testing.T, env *m4Env, application accessApplication, wantStatus int) accessApplicationResponse {
	t.Helper()
	body, err := json.Marshal(application)
	if err != nil {
		t.Fatalf("marshal application: %v", err)
	}
	raw := postWaitlistRaw(t, env, body, wantStatus)
	var decoded accessApplicationResponse
	if wantStatus != http.StatusOK {
		return decoded
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode application response: %v", err)
	}
	return decoded
}

func postWaitlistRaw(t *testing.T, env *m4Env, body []byte, wantStatus int) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, env.server.URL+"/v1/waitlist", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new waitlist request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST waitlist: %v", err)
	}
	defer resp.Body.Close()
	raw := readAll(resp)
	if resp.StatusCode != wantStatus {
		t.Fatalf("waitlist status=%d want=%d body=%s", resp.StatusCode, wantStatus, string(raw))
	}
	return raw
}

func seedAcceptedResultForMedian(t *testing.T, env *m4Env, modelID string, completionTokens int, durationMillis int) {
	t.Helper()
	var orgID string
	if err := env.pool.QueryRow(env.ctx, "SELECT id FROM organizations ORDER BY created_at LIMIT 1").Scan(&orgID); err != nil {
		t.Fatalf("load org for median seed: %v", err)
	}
	now := time.Now().UTC()
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO jobs (id, organization_id, model_id, state, priority, request_metadata, deadline_at, created_at, updated_at, completed_at)
VALUES ('job_01K0M000000000000000000001', $1, $2, 'succeeded', 'standard', '{}'::jsonb, $3, $3, $3, $3);
`, orgID, modelID, now); err != nil {
		t.Fatalf("seed accepted result job: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO job_attempts (id, job_id, attempt_number, lease_nonce, lease_expires_at, deadline_at, status, created_at, accepted_at, started_at, finished_at)
VALUES ('attempt_01K0M000000000000000000002', 'job_01K0M000000000000000000001', 1, 'catalog-median-seed', $1, $1, 'succeeded', $1, $1, $1, $1);
`, now); err != nil {
		t.Fatalf("seed accepted result attempt: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO job_results (id, job_id, attempt_id, model_hash, runtime_hash, prompt_tokens, completion_tokens, duration_millis, coordinator_duration_millis, response_metadata, accepted, created_at)
VALUES ('result_01K0M000000000000000000002', 'job_01K0M000000000000000000001', 'attempt_01K0M000000000000000000002', 'sha256:model', 'sha256:runtime', 10, $1, $2, $2, '{}'::jsonb, true, $3);
`, completionTokens, durationMillis, now); err != nil {
		t.Fatalf("seed accepted result: %v", err)
	}
}

func seedOfflineCatalogModel(t *testing.T, env *m4Env) {
	t.Helper()
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO models (id, display_name, description, status, data_class, created_at, updated_at)
VALUES ('thirdshift-offline-chat-v1', 'Thirdshift Offline Chat v1', 'Offline fixture model.', 'alpha', 'public_or_non_sensitive', now(), now());
INSERT INTO runtime_releases (id, engine, version, manifest_url, binary_sha256, signature_key_id, signature_value, status, released_at)
VALUES ('rt_01K0M000000000000000000001', 'llama.cpp', 'offline-test', 'offline.json', 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'test', 'test', 'active', now());
INSERT INTO model_versions (id, model_id, version, repository, revision, license_identifier, manifest_sha256, runtime_release_id, status, created_at)
VALUES ('mv_01K0M000000000000000000001', 'thirdshift-offline-chat-v1', 'offline-test', 'test/offline', 'offline-rev', 'apache-2.0', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'rt_01K0M000000000000000000001', 'alpha', now());
INSERT INTO model_artifacts (id, model_version_id, artifact_type, url, sha256, byte_size, created_at)
VALUES ('artifact_01K0M000000000000000000004', 'mv_01K0M000000000000000000001', 'gguf', 'https://example.invalid/offline.gguf', 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc', 1, now());
INSERT INTO model_hardware_profiles (id, model_version_id, hardware_class, min_vram_mb, min_ram_mb, max_context_tokens, created_at)
VALUES ('mhp_01K0M000000000000000000001', 'mv_01K0M000000000000000000001', 'offline', 0, 2048, 2048, now());
INSERT INTO model_prices (id, model_version_id, price_version, customer_input_per_million_microdollars, customer_output_per_million_microdollars, host_credit_per_million_output_microdollars, effective_from, created_at)
VALUES ('price_01K0M000000000000000000003', 'mv_01K0M000000000000000000001', 'alpha', 3000000, 4000000, 1000000, now(), now());
INSERT INTO model_manifest_limits (model_id, max_input_tokens, max_output_tokens, max_request_bytes, capabilities, content_filter_profile, price_version, duplicate_sample_rate, challenge_rate, updated_at)
VALUES ('thirdshift-offline-chat-v1', 2048, 256, 65536, '{"chat_completions": true}'::jsonb, 'alpha-default', 'alpha', 0, 0, now());
`); err != nil {
		t.Fatalf("seed offline model: %v", err)
	}
}
