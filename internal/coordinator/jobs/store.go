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

	"github.com/Ani-HQ/thirdshift/internal/coordinator/ledger"
	"github.com/Ani-HQ/thirdshift/internal/node/models"
	"github.com/Ani-HQ/thirdshift/internal/shared/ids"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
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
	var marketInput, marketOutput *int64
	var marketSourceNote string
	if comparison := manifest.Listing.MarketComparison; comparison != nil {
		input := usdToMicrodollars(comparison.TypicalInputPerMillionUSD)
		output := usdToMicrodollars(comparison.TypicalOutputPerMillionUSD)
		marketInput = &input
		marketOutput = &output
		marketSourceNote = comparison.SourceNote
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO models (id, display_name, description, status, data_class, listing_status,
                    expected_output_tokens_per_second,
                    market_typical_input_per_million_microdollars,
                    market_typical_output_per_million_microdollars,
                    market_comparison_source_note,
                    created_at, updated_at)
VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9, NULLIF($10, ''), now(), now())
ON CONFLICT (id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    description = EXCLUDED.description,
    status = EXCLUDED.status,
    data_class = EXCLUDED.data_class,
    listing_status = EXCLUDED.listing_status,
    expected_output_tokens_per_second = EXCLUDED.expected_output_tokens_per_second,
    market_typical_input_per_million_microdollars = EXCLUDED.market_typical_input_per_million_microdollars,
    market_typical_output_per_million_microdollars = EXCLUDED.market_typical_output_per_million_microdollars,
    market_comparison_source_note = EXCLUDED.market_comparison_source_note,
    updated_at = now()
`, manifest.ModelID, manifest.DisplayName, manifest.CatalogDescription(), manifest.Status, dataClass,
		manifest.ListingStatus(), manifest.Listing.ExpectedOutputTokensPerSecond,
		marketInput, marketOutput, marketSourceNote); err != nil {
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
INSERT INTO model_manifest_limits (model_id, max_input_tokens, max_output_tokens, max_request_bytes, capabilities, content_filter_profile, price_version, duplicate_sample_rate, challenge_rate, updated_at)
VALUES ($1, $2, $3, $4, $5::jsonb, $6, 'alpha', $7, $8, now())
ON CONFLICT (model_id) DO UPDATE
SET max_input_tokens = EXCLUDED.max_input_tokens,
    max_output_tokens = EXCLUDED.max_output_tokens,
    max_request_bytes = EXCLUDED.max_request_bytes,
    capabilities = EXCLUDED.capabilities,
    content_filter_profile = EXCLUDED.content_filter_profile,
    price_version = EXCLUDED.price_version,
    duplicate_sample_rate = EXCLUDED.duplicate_sample_rate,
    challenge_rate = EXCLUDED.challenge_rate,
    updated_at = now()
`, manifest.ModelID, maxInputTokens, maxOutputTokens, maxRequestBytes, string(capabilitiesBody), manifest.Policy.ContentFilterProfile, clampRate(manifest.Verification.DuplicateSampleRate), clampRate(manifest.Verification.ChallengeRate)); err != nil {
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
       COALESCE(ml.capabilities, '{}'::jsonb),
       COALESCE(ml.price_version, 'alpha'),
       COALESCE(ml.duplicate_sample_rate::float8, 0),
       COALESCE(ml.challenge_rate::float8, 0)
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
		if err := rows.Scan(&model.ID, &model.DisplayName, &model.DataClass, &model.Version, &model.Pricing.CustomerInputPerMillionMicrodollars, &model.Pricing.CustomerOutputPerMillionMicrodollars, &model.Pricing.HostCreditPerMillionAcceptedOutputMicrodollars, &model.Limits.MaxInputTokens, &model.Limits.MaxOutputTokens, &model.Limits.MaxRequestBytes, &capabilitiesRaw, &model.Verification.PriceVersion, &model.Verification.DuplicateSampleRate, &model.Verification.ChallengeRate); err != nil {
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
JOIN LATERAL (
  SELECT schedule_state, thermal_state, paused, draining
  FROM node_heartbeats
  WHERE node_id = n.id AND session_id = ns.id
  ORDER BY received_at DESC
  LIMIT 1
) hb ON true
WHERE n.state = 'AVAILABLE'
  AND n.current_model_id = $1
  AND n.quarantined_at IS NULL
  AND ns.freshness >= $2
  AND hb.schedule_state = 'in_window'
  AND hb.thermal_state = 'normal'
  AND hb.paused = false
  AND hb.draining = false
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
	rows, err := s.Pool.Query(ctx, `
SELECT n.id, ns.id, hb.model_hash, hb.runtime_hash,
       COALESCE(rep.rolling_success_rate::float8, 0.6)
FROM nodes n
JOIN LATERAL (
  SELECT id, COALESCE(last_heartbeat_at, connected_at) AS freshness
  FROM node_sessions
  WHERE node_id = n.id AND state = 'connected'
  ORDER BY connected_at DESC
  LIMIT 1
) ns ON true
JOIN LATERAL (
  SELECT model_hash, runtime_hash, schedule_state, thermal_state, paused, draining
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
  AND hb.schedule_state = 'in_window'
  AND hb.thermal_state = 'normal'
  AND hb.paused = false
  AND hb.draining = false
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
	if _, err := tx.Exec(ctx, "UPDATE jobs SET state = 'running', updated_at = $2 WHERE id = $1 AND state <> 'succeeded'", jobID, acceptedAt); err != nil {
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
	if _, err := tx.Exec(ctx, "UPDATE jobs SET state = 'running', updated_at = $2 WHERE id = $1 AND state <> 'succeeded'", jobID, startedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s PGStore) CompleteJob(ctx context.Context, payload protocol.JobCompletedPayload, receivedAt time.Time, creditHold time.Duration, meteringStatus string) (OpenAIResponse, error) {
	resultID, err := ids.New("res")
	if err != nil {
		return OpenAIResponse{}, err
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	if meteringStatus == "" {
		meteringStatus = "accepted"
	}
	dataClass := "public_or_non_sensitive"
	_ = s.Pool.QueryRow(ctx, "SELECT data_class FROM models WHERE id = $1", payload.ModelID).Scan(&dataClass)
	attempts := 1
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM job_attempts WHERE job_id = $1", payload.JobID).Scan(&attempts)
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
			Attempts:     attempts,
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
	var status string
	var acceptedAt sql.NullTime
	var leaseExpiresAt time.Time
	var deadlineAt time.Time
	var completedByAnother bool
	if err := tx.QueryRow(ctx, `
SELECT ja.status, ja.accepted_at, ja.lease_expires_at, ja.deadline_at,
       EXISTS (
         SELECT 1 FROM job_attempts other
         WHERE other.job_id = ja.job_id AND other.id <> ja.id AND other.status = 'succeeded'
       )
FROM job_attempts ja
WHERE ja.id = $1 AND ja.job_id = $2
FOR UPDATE
`, payload.AttemptID, payload.JobID).Scan(&status, &acceptedAt, &leaseExpiresAt, &deadlineAt, &completedByAnother); err != nil {
		return OpenAIResponse{}, fmt.Errorf("load attempt for completion: %w", err)
	}
	if completedByAnother || status == "succeeded" {
		return OpenAIResponse{}, fmt.Errorf("job already has an accepted result")
	}
	if status != "accepted" && status != "running" {
		return OpenAIResponse{}, fmt.Errorf("job attempt cannot complete from status %s", status)
	}
	if !acceptedAt.Valid {
		return OpenAIResponse{}, fmt.Errorf("job attempt was never accepted")
	}
	if acceptedAt.Time.After(leaseExpiresAt) {
		return OpenAIResponse{}, fmt.Errorf("job attempt was accepted after lease expiry")
	}
	if receivedAt.After(deadlineAt) {
		return OpenAIResponse{}, fmt.Errorf("job result arrived after deadline")
	}
	var coordinatorDurationMillis int64
	if err := tx.QueryRow(ctx, `
SELECT GREATEST(0, FLOOR(EXTRACT(EPOCH FROM ($3 - COALESCE(started_at, accepted_at, created_at))) * 1000))::bigint
FROM job_attempts
WHERE id = $1 AND job_id = $2
`, payload.AttemptID, payload.JobID, receivedAt).Scan(&coordinatorDurationMillis); err != nil {
		return OpenAIResponse{}, fmt.Errorf("compute coordinator duration: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO job_results (id, job_id, attempt_id, model_hash, runtime_hash, prompt_tokens, completion_tokens, duration_millis, coordinator_duration_millis, response_metadata, accepted, metering_status, verification_status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, true, $11, 'accepted', $12)
`, resultID, payload.JobID, payload.AttemptID, payload.ModelHash, payload.RuntimeHash, payload.Usage.PromptTokens, payload.Usage.CompletionTokens, payload.DurationMillis, coordinatorDurationMillis, string(responseBody), meteringStatus, receivedAt); err != nil {
		return OpenAIResponse{}, fmt.Errorf("insert job result: %w", err)
	}
	ledgerResult, err := (ledger.Store{}).PostAcceptedJobTx(ctx, tx, ledger.AcceptedJobPosting{
		JobID:                     payload.JobID,
		AttemptID:                 payload.AttemptID,
		ReceivedAt:                receivedAt,
		CreditHold:                creditHold,
		PromptTokens:              payload.Usage.PromptTokens,
		CompletionTokens:          payload.Usage.CompletionTokens,
		CoordinatorDurationMillis: coordinatorDurationMillis,
	})
	if err != nil {
		return OpenAIResponse{}, fmt.Errorf("post accepted job ledger: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE job_results SET price_version = $2 WHERE id = $1", resultID, ledgerResult.PriceVersion); err != nil {
		return OpenAIResponse{}, fmt.Errorf("update result price version: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE job_attempts SET status = 'succeeded', finished_at = $3 WHERE id = $1 AND job_id = $2", payload.AttemptID, payload.JobID, receivedAt); err != nil {
		return OpenAIResponse{}, fmt.Errorf("mark job succeeded: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE jobs SET state = 'succeeded', updated_at = $2, completed_at = $2 WHERE id = $1", payload.JobID, receivedAt); err != nil {
		return OpenAIResponse{}, fmt.Errorf("mark job completed: %w", err)
	}
	if err := s.updateAcceptedReputationTx(ctx, tx, payload.AttemptID, receivedAt); err != nil {
		return OpenAIResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OpenAIResponse{}, fmt.Errorf("commit complete job: %w", err)
	}
	return response, nil
}

func (s PGStore) FailAttempt(ctx context.Context, jobID, attemptID, code string, transient bool, now time.Time) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin fail attempt: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
UPDATE job_attempts
SET status = 'failed', transient_failure = $4, error_code = $5, finished_at = $3
WHERE id = $1 AND job_id = $2 AND status IN ('offered', 'accepted', 'running')
`, attemptID, jobID, now, transient, code); err != nil {
		return fmt.Errorf("mark attempt failed: %w", err)
	}
	if err := s.updateFailureReputationTx(ctx, tx, attemptID, code, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE jobs
SET state = 'queued', updated_at = $2
WHERE id = $1 AND state IN ('leased', 'running')
`, jobID, now); err != nil {
		return fmt.Errorf("mark job queued after attempt failure: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit fail attempt: %w", err)
	}
	return nil
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
		if err := s.updateFailureReputationTx(ctx, tx, attemptID, code, now); err != nil {
			return err
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

func (s PGStore) CreateVerificationAttempt(ctx context.Context, jobID, nodeID string, leaseExpiresAt, deadlineAt time.Time) (ScheduledAttempt, error) {
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
		return ScheduledAttempt{}, fmt.Errorf("next verification attempt number: %w", err)
	}
	if _, err := s.Pool.Exec(ctx, `
INSERT INTO job_attempts (id, job_id, node_id, attempt_number, lease_nonce, lease_expires_at, deadline_at, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'offered', now())
`, attemptID, jobID, nodeID, attemptNumber, leaseNonce, leaseExpiresAt, deadlineAt); err != nil {
		return ScheduledAttempt{}, fmt.Errorf("insert verification attempt: %w", err)
	}
	return ScheduledAttempt{JobID: jobID, AttemptID: attemptID, NodeID: nodeID, LeaseExpiresAt: leaseExpiresAt, DeadlineAt: deadlineAt}, nil
}

func (s PGStore) MarkVerificationAttemptCompleted(ctx context.Context, jobID, attemptID string, now time.Time) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin mark verification completed: %w", err)
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `
UPDATE job_attempts
SET status = 'verified', finished_at = $3
WHERE id = $1 AND job_id = $2 AND status IN ('accepted', 'running')
`, attemptID, jobID, now)
	if err != nil {
		return fmt.Errorf("mark verification attempt completed: %w", err)
	}
	if err := s.updateAcceptedReputationTx(ctx, tx, attemptID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s PGStore) MarkVerificationAttemptFailed(ctx context.Context, jobID, attemptID, code string, now time.Time) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin mark verification failed: %w", err)
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `
UPDATE job_attempts
SET status = 'failed', transient_failure = true, error_code = $4, finished_at = $3
WHERE id = $1 AND job_id = $2 AND status IN ('offered', 'accepted', 'running')
`, attemptID, jobID, now, code)
	if err != nil {
		return fmt.Errorf("mark verification attempt failed: %w", err)
	}
	if err := s.updateFailureReputationTx(ctx, tx, attemptID, code, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s PGStore) PostVerificationOverhead(ctx context.Context, payload protocol.JobCompletedPayload, creditHold time.Duration, receivedAt time.Time) error {
	if s.Pool == nil {
		return fmt.Errorf("job store is not configured")
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	_, err := (ledger.Store{Pool: s.Pool}).PostVerificationOverhead(ctx, ledger.VerificationOverheadPosting{
		JobID:                     payload.JobID,
		AttemptID:                 payload.AttemptID,
		ReceivedAt:                receivedAt,
		CreditHold:                creditHold,
		CompletionTokens:          payload.Usage.CompletionTokens,
		CoordinatorDurationMillis: payload.DurationMillis,
	})
	if err != nil {
		return fmt.Errorf("post verification overhead: %w", err)
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

func (s PGStore) ExpireLeases(ctx context.Context, now time.Time) ([]ExpiredAttempt, error) {
	rows, err := s.Pool.Query(ctx, `
UPDATE job_attempts
SET status = 'expired', transient_failure = true, error_code = $2, finished_at = $1
WHERE status = 'offered' AND lease_expires_at <= $1
RETURNING job_id, id, node_id
`, now, CodeJobTimeout)
	if err != nil {
		return nil, fmt.Errorf("expire leases: %w", err)
	}
	defer rows.Close()
	var expired []ExpiredAttempt
	for rows.Next() {
		var attempt ExpiredAttempt
		if err := rows.Scan(&attempt.JobID, &attempt.AttemptID, &attempt.NodeID); err != nil {
			return nil, fmt.Errorf("scan expired lease: %w", err)
		}
		expired = append(expired, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired leases: %w", err)
	}
	return expired, nil
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

func clampRate(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
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

func (s PGStore) updateAcceptedReputationTx(ctx context.Context, tx pgx.Tx, attemptID string, now time.Time) error {
	nodeID, err := nodeIDForAttemptTx(ctx, tx, attemptID)
	if err != nil {
		return err
	}
	return updateAttemptReputationTx(ctx, tx, nodeID, true, "", now)
}

func (s PGStore) updateFailureReputationTx(ctx context.Context, tx pgx.Tx, attemptID, code string, now time.Time) error {
	nodeID, err := nodeIDForAttemptTx(ctx, tx, attemptID)
	if err != nil {
		return err
	}
	return updateAttemptReputationTx(ctx, tx, nodeID, false, code, now)
}

func nodeIDForAttemptTx(ctx context.Context, tx pgx.Tx, attemptID string) (string, error) {
	var nodeID string
	if err := tx.QueryRow(ctx, "SELECT node_id FROM job_attempts WHERE id = $1 AND node_id IS NOT NULL", attemptID).Scan(&nodeID); err != nil {
		return "", fmt.Errorf("load attempt node for reputation: %w", err)
	}
	return nodeID, nil
}

func updateAttemptReputationTx(ctx context.Context, tx pgx.Tx, nodeID string, accepted bool, code string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO node_reputation (node_id, rolling_success_rate, challenge_pass_rate, session_stability, updated_at)
VALUES ($1, 0.6000, 1.0000, 0.6000, $2)
ON CONFLICT (node_id) DO NOTHING
`, nodeID, now); err != nil {
		return fmt.Errorf("ensure node reputation: %w", err)
	}
	var totalAccepted, attemptTotal, attemptFailed, hashMismatch int64
	var timeoutRate float64
	if err := tx.QueryRow(ctx, `
SELECT total_accepted_jobs, attempt_total, attempt_failed, hash_mismatch_count, timeout_rate::float8
FROM node_reputation
WHERE node_id = $1
FOR UPDATE
`, nodeID).Scan(&totalAccepted, &attemptTotal, &attemptFailed, &hashMismatch, &timeoutRate); err != nil {
		return fmt.Errorf("lock node reputation: %w", err)
	}
	previousAttempts := attemptTotal
	attemptTotal++
	if accepted {
		totalAccepted++
	} else {
		attemptFailed++
		if strings.Contains(code, "hash_mismatch") || strings.Contains(code, "hash mismatch") {
			hashMismatch++
		}
	}
	timeoutFailures := int64(math.Round(timeoutRate * float64(previousAttempts)))
	if !accepted && code == CodeJobTimeout {
		timeoutFailures++
	}
	rollingSuccessRate := 0.6
	timeoutRateNext := 0.0
	if attemptTotal > 0 {
		rollingSuccessRate = float64(attemptTotal-attemptFailed) / float64(attemptTotal)
		timeoutRateNext = float64(timeoutFailures) / float64(attemptTotal)
	}
	if _, err := tx.Exec(ctx, `
UPDATE node_reputation
SET total_accepted_jobs = $2,
    attempt_total = $3,
    attempt_failed = $4,
    rolling_success_rate = $5,
    timeout_rate = $6,
    hash_mismatch_count = $7,
    updated_at = $8
WHERE node_id = $1
`, nodeID, totalAccepted, attemptTotal, attemptFailed, rollingSuccessRate, timeoutRateNext, hashMismatch, now); err != nil {
		return fmt.Errorf("update node reputation: %w", err)
	}
	return nil
}

func (s PGStore) RecordMeteringIssues(ctx context.Context, payload protocol.JobCompletedPayload, issues []MeteringIssue, status string, now time.Time) error {
	if len(issues) == 0 {
		return nil
	}
	if status == "flagged" {
		status = "accepted"
	}
	if status == "rejected" {
		status = "rejected"
	}
	if status == "" {
		status = "accepted"
	}
	body, err := json.Marshal(map[string]any{
		"issues": issues,
		"usage":  payload.Usage,
	})
	if err != nil {
		return fmt.Errorf("marshal metering issues: %w", err)
	}
	return s.insertVerificationEvent(ctx, payload.JobID, payload.AttemptID, "", "metering_plausibility", status, body, now)
}

type DuplicateOutcome struct {
	JobID             string
	AttemptID         string
	NodeID            string
	OriginalAttemptID string
	OriginalNodeID    string
	Agreement         bool
	Reason            string
	OccurredAt        time.Time
}

func (s PGStore) RecordDuplicateOutcome(ctx context.Context, outcome DuplicateOutcome) error {
	if outcome.OccurredAt.IsZero() {
		outcome.OccurredAt = time.Now().UTC()
	}
	status := "accepted"
	if !outcome.Agreement {
		status = "rejected"
	}
	body, err := json.Marshal(map[string]any{
		"agreement":           outcome.Agreement,
		"reason":              outcome.Reason,
		"original_attempt_id": outcome.OriginalAttemptID,
		"original_node_id":    outcome.OriginalNodeID,
	})
	if err != nil {
		return fmt.Errorf("marshal duplicate outcome: %w", err)
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin duplicate outcome: %w", err)
	}
	defer tx.Rollback(context.Background())
	if err := insertVerificationEventTx(ctx, tx, outcome.JobID, outcome.AttemptID, outcome.NodeID, "duplicate", status, body, outcome.OccurredAt); err != nil {
		return err
	}
	if err := updateDuplicateReputationTx(ctx, tx, outcome.NodeID, !outcome.Agreement, outcome.OccurredAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type ChallengeOutcome struct {
	JobID      string
	AttemptID  string
	NodeID     string
	ModelID    string
	Passed     bool
	Reason     string
	OccurredAt time.Time
}

func (s PGStore) RecordChallengeOutcome(ctx context.Context, outcome ChallengeOutcome, failureThreshold int) error {
	if outcome.OccurredAt.IsZero() {
		outcome.OccurredAt = time.Now().UTC()
	}
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	status := "accepted"
	if !outcome.Passed {
		status = "rejected"
	}
	body, err := json.Marshal(map[string]any{
		"passed":   outcome.Passed,
		"reason":   outcome.Reason,
		"model_id": outcome.ModelID,
	})
	if err != nil {
		return fmt.Errorf("marshal challenge outcome: %w", err)
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin challenge outcome: %w", err)
	}
	defer tx.Rollback(context.Background())
	if err := insertVerificationEventTx(ctx, tx, outcome.JobID, outcome.AttemptID, outcome.NodeID, "challenge", status, body, outcome.OccurredAt); err != nil {
		return err
	}
	quarantined, err := updateChallengeReputationTx(ctx, tx, outcome.NodeID, !outcome.Passed, failureThreshold, outcome.OccurredAt)
	if err != nil {
		return err
	}
	if quarantined {
		eventBody, err := json.Marshal(map[string]any{
			"reason":   "challenge_failure_threshold",
			"model_id": outcome.ModelID,
		})
		if err != nil {
			return fmt.Errorf("marshal challenge quarantine event: %w", err)
		}
		if err := insertVerificationEventTx(ctx, tx, outcome.JobID, outcome.AttemptID, outcome.NodeID, "challenge", "quarantined", eventBody, outcome.OccurredAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s PGStore) insertVerificationEvent(ctx context.Context, jobID, attemptID, nodeID, eventType, status string, details []byte, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	eventID, err := ids.New("ver")
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO verification_events (id, job_id, attempt_id, node_id, event_type, status, details, created_at)
VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7::jsonb, $8)
`, eventID, jobID, attemptID, nodeID, eventType, status, string(details), now)
	if err != nil {
		return fmt.Errorf("insert verification event: %w", err)
	}
	return nil
}

func insertVerificationEventTx(ctx context.Context, tx pgx.Tx, jobID, attemptID, nodeID, eventType, status string, details []byte, now time.Time) error {
	eventID, err := ids.New("ver")
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO verification_events (id, job_id, attempt_id, node_id, event_type, status, details, created_at)
VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7::jsonb, $8)
`, eventID, jobID, attemptID, nodeID, eventType, status, string(details), now)
	if err != nil {
		return fmt.Errorf("insert verification event: %w", err)
	}
	return nil
}

func updateDuplicateReputationTx(ctx context.Context, tx pgx.Tx, nodeID string, disagreement bool, now time.Time) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO node_reputation (node_id, rolling_success_rate, challenge_pass_rate, session_stability, updated_at)
VALUES ($1, 0.6000, 1.0000, 0.6000, $2)
ON CONFLICT (node_id) DO NOTHING
`, nodeID, now); err != nil {
		return fmt.Errorf("ensure node reputation: %w", err)
	}
	var total, disagreements int64
	if err := tx.QueryRow(ctx, `
SELECT duplicate_total, duplicate_disagreements
FROM node_reputation
WHERE node_id = $1
FOR UPDATE
`, nodeID).Scan(&total, &disagreements); err != nil {
		return fmt.Errorf("lock duplicate reputation: %w", err)
	}
	total++
	if disagreement {
		disagreements++
	}
	rate := 0.0
	if total > 0 {
		rate = float64(disagreements) / float64(total)
	}
	if _, err := tx.Exec(ctx, `
UPDATE node_reputation
SET duplicate_total = $2,
    duplicate_disagreements = $3,
    duplicate_disagreement_rate = $4,
    updated_at = $5
WHERE node_id = $1
`, nodeID, total, disagreements, rate, now); err != nil {
		return fmt.Errorf("update duplicate reputation: %w", err)
	}
	return nil
}

func updateChallengeReputationTx(ctx context.Context, tx pgx.Tx, nodeID string, failure bool, failureThreshold int, now time.Time) (bool, error) {
	if _, err := tx.Exec(ctx, `
INSERT INTO node_reputation (node_id, rolling_success_rate, challenge_pass_rate, session_stability, updated_at)
VALUES ($1, 0.6000, 1.0000, 0.6000, $2)
ON CONFLICT (node_id) DO NOTHING
`, nodeID, now); err != nil {
		return false, fmt.Errorf("ensure node reputation: %w", err)
	}
	var total, failed int64
	if err := tx.QueryRow(ctx, `
SELECT challenge_total, challenge_failed
FROM node_reputation
WHERE node_id = $1
FOR UPDATE
`, nodeID).Scan(&total, &failed); err != nil {
		return false, fmt.Errorf("lock challenge reputation: %w", err)
	}
	total++
	if failure {
		failed++
	}
	passRate := 1.0
	if total > 0 {
		passRate = float64(total-failed) / float64(total)
	}
	quarantine := failure && total > 1 && failed >= int64(failureThreshold)
	var quarantinedAt any
	var reason any
	if quarantine {
		quarantinedAt = now
		reason = "challenge_failure_threshold"
	}
	if _, err := tx.Exec(ctx, `
UPDATE node_reputation
SET challenge_total = $2,
    challenge_failed = $3,
    challenge_pass_rate = $4,
    quarantined_at = COALESCE($5, quarantined_at),
    last_quarantine_reason = COALESCE($6, last_quarantine_reason),
    updated_at = $7
WHERE node_id = $1
`, nodeID, total, failed, passRate, quarantinedAt, reason, now); err != nil {
		return false, fmt.Errorf("update challenge reputation: %w", err)
	}
	if quarantine {
		if _, err := tx.Exec(ctx, "UPDATE nodes SET quarantined_at = COALESCE(quarantined_at, $2), updated_at = $2 WHERE id = $1", nodeID, now); err != nil {
			return false, fmt.Errorf("quarantine node: %w", err)
		}
	}
	return quarantine, nil
}
