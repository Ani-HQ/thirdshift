//go:build integration

package integration

import (
	"strings"
	"testing"

	noderuntime "github.com/Ani-HQ/thirdshift/internal/node/runtime"
)

// The scheduler treats runtime hashes as a set per release, so an AMD host
// running the Vulkan build should already be eligible without any scheduler
// change. That is an assumption worth proving rather than asserting in a doc.
func TestVulkanRuntimeArtifactIsEligibleForScheduling(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	manifest, err := noderuntime.LoadReleaseManifest("../../models/catalog/llama-cpp-b10182.runtime.json")
	if err != nil {
		t.Fatalf("load runtime manifest: %v", err)
	}
	vulkan, ok := manifest.Artifacts["windows/amd64/vulkan"]
	if !ok {
		t.Fatal("runtime manifest has no windows/amd64/vulkan artifact")
	}
	cuda, ok := manifest.Artifacts["windows/amd64/cuda"]
	if !ok {
		t.Fatal("runtime manifest has no windows/amd64/cuda artifact")
	}

	// Catalog sync happens in newM4Env; every artifact key in the release
	// should have landed as its own row.
	var storedKeys []string
	// Resolved through the model rather than by release name, so the test does
	// not depend on how build ids map onto runtime_releases.version.
	rows, err := env.pool.Query(env.ctx, `
SELECT DISTINCT rra.platform_key
FROM runtime_release_artifacts rra
JOIN model_versions mv ON mv.runtime_release_id = rra.runtime_release_id
WHERE mv.model_id = $1
ORDER BY rra.platform_key
`, "thirdshift-tiny-chat-v1")
	if err != nil {
		t.Fatalf("query runtime artifacts: %v", err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			t.Fatalf("scan platform key: %v", err)
		}
		storedKeys = append(storedKeys, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime artifacts: %v", err)
	}

	for _, want := range []string{"windows/amd64", "windows/amd64/cuda", "windows/amd64/vulkan"} {
		found := false
		for _, key := range storedKeys {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("runtime_release_artifacts is missing %q; stored: %v", want, storedKeys)
		}
	}

	// Both vendor builds must be accepted as the runtime hash for a model on
	// this release: that set membership is exactly what eligibility checks.
	hashes, err := env.jobStore.ModelHashes(env.ctx, "thirdshift-tiny-chat-v1")
	if err != nil {
		t.Fatalf("model hashes: %v", err)
	}
	for name, sha := range map[string]string{"vulkan": vulkan.SHA256, "cuda": cuda.SHA256} {
		want := "sha256:" + strings.TrimPrefix(sha, "sha256:")
		if !hashes.RuntimeHashValid(want) {
			t.Fatalf("%s runtime hash %q is not accepted for scheduling; accepted: %v", name, want, hashes.RuntimeHashes)
		}
	}
}
