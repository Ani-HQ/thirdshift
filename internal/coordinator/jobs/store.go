package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anianroid/thirdshift/internal/node/models"
	"github.com/anianroid/thirdshift/internal/shared/ids"
	"github.com/anianroid/thirdshift/internal/shared/protocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGStore struct {
	Pool *pgxpool.Pool
}

type IdempotencyRecord struct {
	RequestHash      string
	ResponseStatus   int
	ResponseMetadata json.RawMessage
	JobID            string
}

func (s PGStore) CreateOrg(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("organization name is required")
	}
	orgID, err := ids.New("org")
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO organizations (id, name, created_at, updated_at)
VALUES ($1, $2, now(), now())
`, orgID, name)
	if err != nil {
		return "", fmt.Errorf("create organization: %w", err)
	}
	return orgID, nil
}

func (s PGStore) CreateAPIKey(ctx context.Context, orgID, name, rawKey string, modelIDs []string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("organization id is required")
	}
	if name == "" {
		name = "default"
	}
	keyID, err := ids.New("ak")
	if err != nil {
		return "", err
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin api key create: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
INSERT INTO api_keys (id, organization_id, name, key_hash, status, created_at)
VALUES ($1, $2, $3, $4, 'active', now())
`, keyID, orgID, name, HashAPIKey(rawKey)); err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}
	if len(modelIDs) == 0 {
		rows, err := tx.Query(ctx, "SELECT id FROM models WHERE status IN ('alpha', 'active') ORDER BY id")
		if err != nil {
			return "", fmt.Errorf("list models for api key permissions: %w", err)
		}
		for rows.Next() {
			var modelID string
			if err := rows.Scan(&modelID); err != nil {
				rows.Close()
				return "", fmt.Errorf("scan model permission: %w", err)
			}
			modelIDs = append(modelIDs, modelID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return "", fmt.Errorf("iterate model permissions: %w", err)
		}
		rows.Close()
	}
	for _, modelID := range modelIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO api_key_model_permissions (api_key_id, model_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING
`, keyID, modelID); err != nil {
			return "", fmt.Errorf("grant api key model permission: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit api key create: %w", err)
	}
	return keyID, nil
}

func (s PGStore) APIKeyByHash(ctx context.Context, hash string) (APIKeyPrincipal, error) {
	var principal APIKeyPrincipal
	err := s.Pool.QueryRow(ctx, `
SELECT id, organization_id
FROM api_keys
WHERE key_hash = $1 AND status = 'active'
`, hash).Scan(&principal.ID, &principal.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKeyPrincipal{}, APIError{Code: CodeUnauthorized, Message: "Invalid API key.", Retryable: false, Status: 401}
	}
	if err != nil {
		return APIKeyPrincipal{}, fmt.Errorf("lookup api key: %w", err)
	}
	rows, err := s.Pool.Query(ctx, "SELECT model_id FROM api_key_model_permissions WHERE api_key_id = $1", principal.ID)
	if err != nil {
		return APIKeyPrincipal{}, fmt.Errorf("lookup api key model permissions: %w", err)
	}
	defer rows.Close()
	principal.AllowedModels = map[string]bool{}
	for rows.Next() {
		var modelID string
		if err := rows.Scan(&modelID); err != nil {
			return APIKeyPrincipal{}, fmt.Errorf("scan api key permission: %w", err)
		}
		principal.AllowedModels[modelID] = true
	}
	if err := rows.Err(); err != nil {
		return APIKeyPrincipal{}, fmt.Errorf("iterate api key permissions: %w", err)
	}
	return principal, nil
}

func (s PGStore) SyncCatalog(ctx context.Context, catalogDir string) (int, error) {
	if catalogDir == "" {
		catalogDir = "models/catalog"
	}
	entries, err := os.ReadDir(catalogDir)
	if err != nil {
		return 0, fmt.Errorf("read catalog dir: %w", err)
	}
	count := 0
	var firstSkipped error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		manifestPath := filepath.Join(catalogDir, entry.Name())
		manifest, err := models.ParseManifestFile(manifestPath)
		if err != nil {
			if firstSkipped == nil {
				firstSkipped = err
			}
			continue
		}
		if err := s.upsertManifest(ctx, manifest, manifestPath); err != nil {
			return count, err
		}
		count++
	}
	if count == 0 && firstSkipped != nil {
		return 0, fmt.Errorf("no valid model manifests synced; first skipped manifest: %w", firstSkipped)
	}
	return count, nil
}

func (s PGStore) upsertManifest(ctx context.Context, manifest models.Manifest, manifestPath string) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin catalog sync: %w", err)
	}
	defer tx.Rollback(context.Background())
	dataClass := manifest.Policy.DataClass
	if dataClass == "" {
		dataClass = "public_or_non_sensitive"
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO models (id, display_name, status, data_class, created_at, updated_at)
VALUES ($1, $2, $3, $4, now(), now())
ON CONFLICT (id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    status = EXCLUDED.status,
    data_class = EXCLUDED.data_class,
    updated_at = now()
`, manifest.ModelID, manifest.DisplayName, manifest.Status, dataClass); err != nil {
		return fmt.Errorf("upsert model: %w", err)
	}

	runtimeID, err := ids.New("rt")
	if err != nil {
		return err
	}
	var actualRuntimeID string
	err = tx.QueryRow(ctx, `
INSERT INTO runtime_releases (id, engine, version, manifest_url, binary_sha256, signature_key_id, signature_value, status, released_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', now())
ON CONFLICT (engine, version) DO UPDATE
SET manifest_url = EXCLUDED.manifest_url,
    binary_sha256 = EXCLUDED.binary_sha256,
    signature_key_id = EXCLUDED.signature_key_id,
    signature_value = EXCLUDED.signature_value,
    status = 'active'
RETURNING id
`, runtimeID, manifest.Runtime.Engine, manifest.Runtime.BuildID, manifest.Runtime.ReleaseManifest, rawSHA256(manifest.Runtime.BinarySHA256), "catalog-sync", "catalog-sync").Scan(&actualRuntimeID)
	if err != nil {
		return fmt.Errorf("upsert runtime release: %w", err)
	}

	versionID, err := ids.New("mv")
	if err != nil {
		return err
	}
	version := manifest.Source.Revision
	if version == "" {
		version = manifest.Runtime.BuildID
	}
	var actualVersionID string
	err = tx.QueryRow(ctx, `
INSERT INTO model_versions (id, model_id, version, repository, revision, license_identifier, manifest_sha256, runtime_release_id, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (model_id, version) DO UPDATE
SET repository = EXCLUDED.repository,
    revision = EXCLUDED.revision,
    license_identifier = EXCLUDED.license_identifier,
    manifest_sha256 = EXCLUDED.manifest_sha256,
    runtime_release_id = EXCLUDED.runtime_release_id,
    status = EXCLUDED.status
RETURNING id
`, versionID, manifest.ModelID, version, manifest.Source.Repository, manifest.Source.Revision, manifest.License.Identifier, manifestSHA256(manifestPath), actualRuntimeID, manifest.Status).Scan(&actualVersionID)
	if err != nil {
		return fmt.Errorf("upsert model version: %w", err)
	}

	artifactID, err := ids.New("artifact")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO model_artifacts (id, model_version_id, artifact_type, url, sha256, byte_size, created_at)
VALUES ($1, $2, 'gguf', $3, $4, $5, now())
ON CONFLICT (model_version_id, artifact_type, sha256) DO UPDATE
SET url = EXCLUDED.url,
    byte_size = EXCLUDED.byte_size
`, artifactID, actualVersionID, manifest.Source.URL, rawSHA256(manifest.Source.SHA256), manifest.Source.SizeBytes); err != nil {
		return fmt.Errorf("upsert model artifact: %w", err)
	}

	profileID, err := ids.New("mhp")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO model_hardware_profiles (id, model_version_id, hardware_class, min_vram_mb, min_ram_mb, max_context_tokens, created_at)
VALUES ($1, $2, 'alpha-default', $3, $4, $5, now())
ON CONFLICT (model_version_id, hardware_class) DO UPDATE
SET min_vram_mb = EXCLUDED.min_vram_mb,
    min_ram_mb = EXCLUDED.min_ram_mb,
    max_context_tokens = EXCLUDED.max_context_tokens
`, profileID, actualVersionID, manifest.Hardware.MinVRAMMB, manifest.Hardware.MinRAMMB, manifest.Limits.MaxInputTokens); err != nil {
		return fmt.Errorf("upsert model hardware profile: %w", err)
	}

	priceID, err := ids.New("price")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO model_prices (id, model_version_id, price_version, customer_input_per_million_microdollars, customer_output_per_million_microdollars, host_credit_per_million_output_microdollars, effective_from, created_at)
VALUES ($1, $2, 'alpha', $3, $4, $5, now(), now())
ON CONFLICT (model_version_id, price_version) DO UPDATE
SET customer_input_per_million_microdollars = EXCLUDED.customer_input_per_million_microdollars,
    customer_output_per_million_microdollars = EXCLUDED.customer_output_per_million_microdollars,
    host_credit_per_million_output_microdollars = EXCLUDED.host_credit_per_million_output_microdollars
	`, priceID, actualVersionID, usdToMicrodollars(manifest.Pricing.CustomerInputPerMillionUSD), usdToMicrodollars(manifest.Pricing.CustomerOutputPerMillionUSD), usdToMicrodollars(manifest.Pricing.HostCreditPerMillionAcceptedOutputUSD)); err != nil {
		return fmt.Errorf("upsert model price: %w", err)
	}

	capabilities := map[string]bool{
		"chat_completions": manifest.Capabilities.ChatCompletions,
		"streaming":        manifest.Capabilities.Streaming,
		"tools":            manifest.Capabilities.Tools,
		"embeddings":       manifest.Capabilities.Embeddings,
	}
	capabilitiesBody, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("marshal model capabilities: %w", err)
	}
	maxInputTokens := manifest.Limits.MaxInputTokens
	if maxInputTokens <= 0 {
		maxInputTokens = manifest.Runtime.Arguments.ContextSize
	}
	if maxInputTokens <= 0 {
		maxInputTokens = 4096
	}
	maxOutputTokens := manifest.Limits.MaxOutputTokens
	if maxOutputTokens <= 0 {
		maxOutputTokens = 1024
	}
	maxRequestBytes := manifest.Limits.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = 256 * 1024
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO model_manifest_limits (model_id, max_input_tokens, max_output_tokens, max_request_bytes, capabilities, content_filter_profile, updated_at)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, now())
ON CONFLICT (model_id) DO UPDATE
SET max_input_tokens = EXCLUDED.max_input_tokens,
    max_output_tokens = EXCLUDED.max_output_tokens,
    max_request_bytes = EXCLUDED.max_request_bytes,
    capabilities = EXCLUDED.capabilities,
    content_filter_profile = EXCLUDED.content_filter_profile,
    updated_at = now()
`, manifest.ModelID, maxInputTokens, maxOutputTokens, maxRequestBytes, string(capabilitiesBody), manifest.Policy.ContentFilterProfile); err != nil {
		return fmt.Errorf("upsert model manifest limits: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit catalog sync: %w", err)
	}
	return nil
}

func (s PGStore) ListModels(ctx context.Context, freshnessCutoff time.Time) ([]ModelInfo, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT m.id, m.display_name, m.data_class, mv.version,
       COALESCE(mp.customer_input_per_million_microdollars, 0),
       COALESCE(mp.customer_output_per_million_microdollars, 0),
       COALESCE(mp.host_credit_per_million_output_microdollars, 0),
       COALESCE(hp.max_context_tokens, 4096),
       COALESCE(ml.max_output_tokens, 1024),
       COALESCE(ml.max_request_bytes, 262144),
       COALESCE(ml.capabilities, '{}'::jsonb)
FROM models m
JOIN LATERAL (
  SELECT * FROM model_versions WHERE model_id = m.id ORDER BY created_at DESC LIMIT 1
) mv ON true
LEFT JOIN LATERAL (
  SELECT * FROM model_prices WHERE model_version_id = mv.id ORDER BY effective_from DESC LIMIT 1
) mp ON true
LEFT JOIN LATERAL (
  SELECT * FROM model_hardware_profiles WHERE model_version_id = mv.id ORDER BY created_at DESC LIMIT 1
) hp ON true
LEFT JOIN model_manifest_limits ml ON ml.model_id = m.id
WHERE m.status IN ('alpha', 'active')
ORDER BY m.id
`)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()
	var modelsOut []ModelInfo
	for rows.Next() {
		var model ModelInfo
		var capabilitiesRaw []byte
		if err := rows.Scan(&model.ID, &model.DisplayName, &model.DataClass, &model.Version, &model.Pricing.CustomerInputPerMillionMicrodollars, &model.Pricing.CustomerOutputPerMillionMicrodollars, &model.Pricing.HostCreditPerMillionAcceptedOutputMicrodollars, &model.Limits.MaxInputTokens, &model.Limits.MaxOutputTokens, &model.Limits.MaxRequestBytes, &capabilitiesRaw); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		model.Capabilities = capabilitiesFromJSON(capabilitiesRaw)
		availability, err := s.Availability(ctx, model.ID, freshnessCutoff)
		if err != nil {
			return nil, err
		}
		model.Availability.AvailableNodes = availability
		modelsOut = append(modelsOut, model)
	}
	return modelsOut, rows.Err()
}

func (s PGStore) ModelInfo(ctx context.Context, modelID string, freshnessCutoff time.Time) (ModelInfo, error) {
	modelsOut, err := s.ListModels(ctx, freshnessCutoff)
	if err != nil {
		return ModelInfo{}, err
	}
	for _, model := range modelsOut {
		if model.ID == modelID {
			hashes, err := s.ModelHashes(ctx, modelID)
			if err != nil {
				return ModelInfo{}, err
			}
			model.ModelHash = hashes.ModelHash
			model.RuntimeHash = hashes.RuntimeHash
			return model, nil
		}
	}
	return ModelInfo{}, APIError{Code: CodeModelNotFound, Message: "Model not found.", Retryable: false, Status: 404}
}

type modelHashes struct {
	ModelHash   string
	RuntimeHash string
}

func (s PGStore) ModelHashes(ctx context.Context, modelID string) (modelHashes, error) {
	var hashes modelHashes
	err := s.Pool.QueryRow(ctx, `
SELECT 'sha256:' || ma.sha256, 'sha256:' || rr.binary_sha256
FROM model_versions mv
JOIN model_artifacts ma ON ma.model_version_id = mv.id AND ma.artifact_type = 'gguf'
LEFT JOIN runtime_releases rr ON rr.id = mv.runtime_release_id
WHERE mv.model_id = $1
ORDER BY mv.created_at DESC
LIMIT 1
`, modelID).Scan(&hashes.ModelHash, &hashes.RuntimeHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelHashes{}, APIError{Code: CodeModelNotFound, Message: "Model not found.", Retryable: false, Status: 404}
	}
	if err != nil {
		return modelHashes{}, fmt.Errorf("lookup model hashes: %w", err)
	}
	return hashes, nil
}

func (s PGStore) Availability(ctx context.Context, modelID string, freshnessCutoff time.Time) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx, `
SELECT count(*)
FROM nodes n
JOIN LATERAL (
  SELECT id, COALESCE(last_heartbeat_at, connected_at) AS freshness
  FROM node_sessions
  WHERE node_id = n.id AND state = 'connected'
  ORDER BY connected_at DESC
  LIMIT 1
) ns ON true
WHERE n.state = 'AVAILABLE'
  AND n.current_model_id = $1
  AND n.quarantined_at IS NULL
  AND ns.freshness >= $2
  AND NOT EXISTS (
    SELECT 1 FROM job_attempts ja
    WHERE ja.node_id = n.id AND ja.status IN ('offered', 'accepted', 'running')
  )
`, modelID, freshnessCutoff).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count model availability: %w", err)
	}
	return count, nil
}

func (s PGStore) EligibleNodes(ctx context.Context, modelID string, freshnessCutoff time.Time) ([]Candidate, error) {
	// TODO(M4/M5): add host schedule, data-class, and richer reputation
	// filters once those inputs are persisted.
	rows, err := s.Pool.Query(ctx, `
SELECT n.id, ns.id, hb.model_hash, hb.runtime_hash,
       COALESCE(rep.rolling_success_rate::float8, 1.0)
FROM nodes n
JOIN LATERAL (
  SELECT id, COALESCE(last_heartbeat_at, connected_at) AS freshness
  FROM node_sessions
  WHERE node_id = n.id AND state = 'connected'
  ORDER BY connected_at DESC
  LIMIT 1
) ns ON true
JOIN LATERAL (
  SELECT model_hash, runtime_hash
  FROM node_heartbeats
  WHERE node_id = n.id AND session_id = ns.id
  ORDER BY received_at DESC
  LIMIT 1
) hb ON true
JOIN model_versions mv ON mv.model_id = $1
JOIN model_artifacts ma ON ma.model_version_id = mv.id AND ma.artifact_type = 'gguf'
JOIN runtime_releases rr ON rr.id = mv.runtime_release_id
LEFT JOIN node_reputation rep ON rep.node_id = n.id
WHERE n.state = 'AVAILABLE'
  AND n.current_model_id = $1
  AND n.quarantined_at IS NULL
  AND ns.freshness >= $2
  AND hb.model_hash = 'sha256:' || ma.sha256
  AND hb.runtime_hash = 'sha256:' || rr.binary_sha256
  AND NOT EXISTS (
    SELECT 1 FROM job_attempts ja
    WHERE ja.node_id = n.id AND ja.status IN ('offered', 'accepted', 'running')
  )
ORDER BY n.id
`, modelID, freshnessCutoff)
	if err != nil {
		return nil, fmt.Errorf("eligible nodes: %w", err)
	}
	defer rows.Close()
	var candidates []Candidate
	for rows.Next() {
		var candidate Candidate
		if err := rows.Scan(&candidate.NodeID, &candidate.SessionID, &candidate.ModelHash, &candidate.RuntimeHash, &candidate.RollingSuccessRate); err != nil {
			return nil, fmt.Errorf("scan eligible node: %w", err)
		}
		candidate.TokensPerSecond = 1
		candidate.RecentFailureBonus = 1
		candidate.ThermalHeadroom = 1
		candidate.HostFairness = 1
		candidate.RegionalPreference = 1
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (s PGStore) CreateJob(ctx context.Context, orgID, apiKeyID, modelID, priority string, request any, deadlineAt time.Time) (string, error) {
	jobID, err := ids.New("job")
	if err != nil {
		return "", err
	}
	if priority == "" {
		priority = "standard"
	}
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO jobs (id, organization_id, api_key_id, model_id, state, priority, request_metadata, deadline_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'queued', $5, $6::jsonb, $7, now(), now())
`, jobID, orgID, apiKeyID, modelID, priority, string(body), deadlineAt)
	if err != nil {
		return "", fmt.Errorf("create job: %w", err)
	}
	return jobID, nil
}

func (s PGStore) CreateAttempt(ctx context.Context, jobID, nodeID string, leaseExpiresAt, deadlineAt time.Time) (ScheduledAttempt, error) {
	attemptID, err := ids.New("att")
	if err != nil {
		return ScheduledAttempt{}, err
	}
	leaseNonce, err := ids.New("lease")
	if err != nil {
		return ScheduledAttempt{}, err
	}
	var attemptNumber int
	if err := s.Pool.QueryRow(ctx, "SELECT COALESCE(max(attempt_number), 0) + 1 FROM job_attempts WHERE job_id = $1", jobID).Scan(&attemptNumber); err != nil {
		return ScheduledAttempt{}, fmt.Errorf("next attempt number: %w", err)
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ScheduledAttempt{}, fmt.Errorf("begin create attempt: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
INSERT INTO job_attempts (id, job_id, node_id, attempt_number, lease_nonce, lease_expires_at, deadline_at, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'offered', now())
`, attemptID, jobID, nodeID, attemptNumber, leaseNonce, leaseExpiresAt, deadlineAt); err != nil {
		return ScheduledAttempt{}, fmt.Errorf("insert job attempt: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE jobs SET state = 'leased', updated_at = now() WHERE id = $1", jobID); err != nil {
		return ScheduledAttempt{}, fmt.Errorf("mark job leased: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ScheduledAttempt{}, fmt.Errorf("commit create attempt: %w", err)
	}
	return ScheduledAttempt{JobID: jobID, AttemptID: attemptID, NodeID: nodeID, LeaseExpiresAt: leaseExpiresAt, DeadlineAt: deadlineAt}, nil
}

func (s PGStore) MarkAccepted(ctx context.Context, jobID, attemptID string, acceptedAt time.Time) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(ctx, `
UPDATE job_attempts
SET status = 'accepted', accepted_at = $3
WHERE id = $1
  AND job_id = $2
  AND status = 'offered'
  AND $3 <= lease_expires_at
  AND $3 <= created_at + interval '2 seconds'
`, attemptID, jobID, acceptedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job attempt was not accepted within the offer window")
	}
	if _, err := tx.Exec(ctx, "UPDATE jobs SET state = 'running', updated_at = $2 WHERE id = $1", jobID, acceptedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s PGStore) MarkStarted(ctx context.Context, jobID, attemptID string, startedAt time.Time) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(ctx, `
UPDATE job_attempts
SET status = 'running', started_at = $3
WHERE id = $1 AND job_id = $2 AND status IN ('accepted', 'running')
`, attemptID, jobID, startedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("job attempt cannot be marked running from its current state")
	}
	if _, err := tx.Exec(ctx, "UPDATE jobs SET state = 'running', updated_at = $2 WHERE id = $1", jobID, startedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s PGStore) CompleteJob(ctx context.Context, payload protocol.JobCompletedPayload) (OpenAIResponse, error) {
	resultID, err := ids.New("res")
	if err != nil {
		return OpenAIResponse{}, err
	}
	dataClass := "public_or_non_sensitive"
	_ = s.Pool.QueryRow(ctx, "SELECT data_class FROM models WHERE id = $1", payload.ModelID).Scan(&dataClass)
	response := OpenAIResponse{
		ID:      strings.Replace(resultID, "res_", "chatcmpl_", 1),
		Object:  "chat.completion",
		Created: payload.CompletedAt.Unix(),
		Model:   payload.ModelID,
		Choices: []OpenAIChoice{{
			Index:        0,
			Message:      protocol.ChatMessage{Role: "assistant", Content: ""},
			FinishReason: payload.FinishReason,
		}},
		Usage: payload.Usage,
		Thirdshift: ThirdshiftResponseMeta{
			JobID:        payload.JobID,
			Attempts:     1,
			DataClass:    dataClass,
			ServedRegion: "local",
		},
	}
	if payload.Message != nil {
		response.Choices[0].Message = *payload.Message
	}
	responseBody, err := json.Marshal(response)
	if err != nil {
		return OpenAIResponse{}, err
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return OpenAIResponse{}, fmt.Errorf("begin complete job: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
INSERT INTO job_results (id, job_id, attempt_id, model_hash, runtime_hash, prompt_tokens, completion_tokens, duration_millis, response_metadata, accepted, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, true, $10)
`, resultID, payload.JobID, payload.AttemptID, payload.ModelHash, payload.RuntimeHash, payload.Usage.PromptTokens, payload.Usage.CompletionTokens, payload.DurationMillis, string(responseBody), payload.CompletedAt); err != nil {
		return OpenAIResponse{}, fmt.Errorf("insert job result: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE job_attempts SET status = 'succeeded', finished_at = $3 WHERE id = $1 AND job_id = $2", payload.AttemptID, payload.JobID, payload.CompletedAt); err != nil {
		return OpenAIResponse{}, fmt.Errorf("mark job succeeded: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE jobs SET state = 'succeeded', updated_at = $2, completed_at = $2 WHERE id = $1", payload.JobID, payload.CompletedAt); err != nil {
		return OpenAIResponse{}, fmt.Errorf("mark job completed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return OpenAIResponse{}, fmt.Errorf("commit complete job: %w", err)
	}
	return response, nil
}

func (s PGStore) FailJob(ctx context.Context, jobID, attemptID, code, message string, retryable bool, transient bool, now time.Time) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin fail job: %w", err)
	}
	defer tx.Rollback(context.Background())
	if attemptID != "" {
		if _, err := tx.Exec(ctx, `
UPDATE job_attempts
SET status = 'failed', transient_failure = $4, error_code = $5, finished_at = $3
WHERE id = $1 AND job_id = $2
`, attemptID, jobID, now, transient, code); err != nil {
			return fmt.Errorf("mark attempt failed: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE jobs
SET state = 'failed', updated_at = $2, completed_at = $2
WHERE id = $1
`, jobID, now); err != nil {
		return fmt.Errorf("mark job failed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit fail job: %w", err)
	}
	return nil
}

func (s PGStore) CancelJob(ctx context.Context, orgID, jobID string, now time.Time) (JobStatus, error) {
	tag, err := s.Pool.Exec(ctx, `
UPDATE jobs
SET state = 'cancelled', updated_at = $3, completed_at = $3
WHERE id = $1 AND organization_id = $2 AND state IN ('queued', 'leased')
`, jobID, orgID, now)
	if err != nil {
		return JobStatus{}, fmt.Errorf("cancel job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return JobStatus{}, APIError{Code: CodeInvalidRequest, Message: "Job cannot be cancelled in its current state.", Retryable: false, Status: 409}
	}
	return s.GetJob(ctx, orgID, jobID)
}

func (s PGStore) GetJob(ctx context.Context, orgID, jobID string) (JobStatus, error) {
	var status JobStatus
	var resultRaw []byte
	err := s.Pool.QueryRow(ctx, `
SELECT j.id, j.state, j.model_id, j.created_at, j.updated_at, COALESCE(r.response_metadata, '{}'::jsonb)
FROM jobs j
LEFT JOIN LATERAL (
  SELECT response_metadata
  FROM job_results
  WHERE job_id = j.id AND accepted
  ORDER BY created_at DESC
  LIMIT 1
) r ON true
WHERE j.id = $1 AND j.organization_id = $2
`, jobID, orgID).Scan(&status.ID, &status.State, &status.Model, &status.CreatedAt, &status.UpdatedAt, &resultRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return JobStatus{}, APIError{Code: CodeInvalidRequest, Message: "Job not found.", Retryable: false, Status: 404}
	}
	if err != nil {
		return JobStatus{}, fmt.Errorf("get job: %w", err)
	}
	if status.State == "succeeded" && len(resultRaw) > 2 {
		var response OpenAIResponse
		if err := json.Unmarshal(resultRaw, &response); err == nil {
			status.Result = &response
		}
	}
	return status, nil
}

func (s PGStore) ExpireLeases(ctx context.Context, now time.Time) (int64, error) {
	rows, err := s.Pool.Query(ctx, `
UPDATE job_attempts
SET status = 'expired', transient_failure = true, error_code = $2, finished_at = $1
WHERE status = 'offered' AND lease_expires_at <= $1
RETURNING job_id
`, now, CodeJobTimeout)
	if err != nil {
		return 0, fmt.Errorf("expire leases: %w", err)
	}
	defer rows.Close()
	var jobIDs []string
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			return 0, fmt.Errorf("scan expired lease: %w", err)
		}
		jobIDs = append(jobIDs, jobID)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate expired leases: %w", err)
	}
	for _, jobID := range jobIDs {
		if _, err := s.Pool.Exec(ctx, `
UPDATE jobs
SET state = 'failed', updated_at = $2, completed_at = $2
WHERE id = $1 AND state = 'leased'
`, jobID, now); err != nil {
			return 0, fmt.Errorf("mark expired job failed: %w", err)
		}
	}
	return int64(len(jobIDs)), nil
}

func (s PGStore) IdempotencyRecord(ctx context.Context, apiKeyID, endpoint, key string) (IdempotencyRecord, bool, error) {
	var record IdempotencyRecord
	var responseMetadata []byte
	var jobID sql.NullString
	err := s.Pool.QueryRow(ctx, `
SELECT request_hash, COALESCE(response_status, 0), COALESCE(response_metadata, '{}'::jsonb), job_id
FROM idempotency_records
WHERE api_key_id = $1 AND endpoint = $2 AND idempotency_key = $3 AND expires_at > now()
`, apiKeyID, endpoint, key).Scan(&record.RequestHash, &record.ResponseStatus, &responseMetadata, &jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return IdempotencyRecord{}, false, fmt.Errorf("lookup idempotency record: %w", err)
	}
	record.ResponseMetadata = responseMetadata
	record.JobID = jobID.String
	return record, true, nil
}

func (s PGStore) StoreIdempotencyResponse(ctx context.Context, apiKeyID, endpoint, key, requestHash, jobID string, status int, response any, expiresAt time.Time) error {
	recordID, err := ids.New("idem")
	if err != nil {
		return err
	}
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO idempotency_records (id, api_key_id, endpoint, idempotency_key, request_hash, response_status, response_metadata, job_id, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, now(), $9)
ON CONFLICT (api_key_id, endpoint, idempotency_key) DO UPDATE
SET response_status = EXCLUDED.response_status,
    response_metadata = EXCLUDED.response_metadata,
    job_id = EXCLUDED.job_id
`, recordID, apiKeyID, endpoint, key, requestHash, status, string(body), jobID, expiresAt)
	if err != nil {
		return fmt.Errorf("store idempotency response: %w", err)
	}
	return nil
}

func (s PGStore) PublicKeyForNode(ctx context.Context, nodeID string) (string, error) {
	var publicKey string
	err := s.Pool.QueryRow(ctx, `
SELECT public_key FROM node_keys WHERE node_id = $1 AND status = 'active' ORDER BY created_at DESC LIMIT 1
`, nodeID).Scan(&publicKey)
	if err != nil {
		return "", err
	}
	return publicKey, nil
}

func rawSHA256(value string) string {
	return strings.TrimPrefix(value, "sha256:")
}

func usdToMicrodollars(value float64) int64 {
	return int64(math.Round(value * 1_000_000))
}

func manifestSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return path
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func capabilitiesFromJSON(raw []byte) []string {
	var decoded map[string]bool
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return []string{"chat_completions"}
	}
	capabilities := make([]string, 0, len(decoded))
	for _, name := range []string{"chat_completions", "streaming", "tools", "embeddings"} {
		if decoded[name] {
			capabilities = append(capabilities, name)
		}
	}
	if len(capabilities) == 0 {
		return []string{"chat_completions"}
	}
	return capabilities
}
