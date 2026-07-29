package models

import "testing"

func TestCatalogManifestParsesPinnedTinyModel(t *testing.T) {
	manifest, _, err := LoadCatalogManifest("../../../models/catalog", "thirdshift-tiny-chat-v1")
	if err != nil {
		t.Fatalf("parse existing model manifest: %v", err)
	}
	if manifest.ModelID != "thirdshift-tiny-chat-v1" {
		t.Fatalf("model id = %q", manifest.ModelID)
	}
	if manifest.Source.SizeBytes != 88201792 {
		t.Fatalf("source size = %d", manifest.Source.SizeBytes)
	}
	if manifest.Runtime.ReleaseManifest == "" {
		t.Fatal("runtime release manifest path is empty")
	}
}
