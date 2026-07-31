package models

import "testing"

func TestCatalogFallsBackToEmbeddedWhenDiskMissing(t *testing.T) {
	manifest, source, err := LoadCatalogManifest(t.TempDir(), "qwen2.5-7b-instruct")
	if err != nil {
		t.Fatalf("embedded catalog load: %v", err)
	}
	if manifest.ModelID != "qwen2.5-7b-instruct" || source != "embedded:qwen2.5-7b-instruct.yaml" {
		t.Fatalf("manifest=%s source=%s", manifest.ModelID, source)
	}
	if _, source, err = ReadCatalogFile(t.TempDir(), "llama-cpp-b10180.runtime.json"); err != nil || source != "embedded:llama-cpp-b10180.runtime.json" {
		t.Fatalf("runtime manifest fallback: source=%s err=%v", source, err)
	}
}
