package protocol

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sharedprotocol "github.com/anianroid/thirdshift/internal/shared/protocol"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type envelopeHeader struct {
	Type sharedprotocol.MessageType `json:"type"`
}

func TestExamplesValidateAgainstMessageSchemas(t *testing.T) {
	examplesDir := "examples"
	schemasDir := "schemas"

	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}

	seen := map[sharedprotocol.MessageType]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(examplesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		var header envelopeHeader
		if err := json.Unmarshal(data, &header); err != nil {
			t.Fatalf("unmarshal %s: %v", path, err)
		}
		if !sharedprotocol.IsKnownMessageType(header.Type) {
			t.Fatalf("%s has unknown message type %q", path, header.Type)
		}

		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("unmarshal %s as document: %v", path, err)
		}

		schemaPath := filepath.Join(schemasDir, string(header.Type)+".schema.json")
		schema := compileSchema(t, schemaPath)
		if err := schema.Validate(document); err != nil {
			t.Fatalf("%s does not validate against %s: %v", path, schemaPath, err)
		}
		seen[header.Type] = true
	}

	for _, typ := range sharedprotocol.RequiredMessageTypes {
		if !seen[typ] {
			t.Fatalf("missing example for %s", typ)
		}
	}
}

func TestEveryRequiredMessageTypeHasSchema(t *testing.T) {
	for _, typ := range sharedprotocol.RequiredMessageTypes {
		path := filepath.Join("schemas", string(typ)+".schema.json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("schema for %s: %v", typ, err)
		}
		compileSchema(t, path)
	}
	compileSchema(t, filepath.Join("schemas", "envelope.schema.json"))
	compileSchema(t, filepath.Join("schemas", "job.offer.payload.schema.json"))
}

func TestEnvelopeSchemaListsEveryRequiredMessageType(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("schemas", "envelope.schema.json"))
	if err != nil {
		t.Fatalf("read envelope schema: %v", err)
	}

	var schema struct {
		Properties struct {
			Type struct {
				Enum []sharedprotocol.MessageType `json:"enum"`
			} `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("unmarshal envelope schema: %v", err)
	}

	for _, typ := range sharedprotocol.RequiredMessageTypes {
		if !slices.Contains(schema.Properties.Type.Enum, typ) {
			t.Fatalf("envelope schema is missing message type %s", typ)
		}
	}
}

func compileSchema(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("absolute path for %s: %v", path, err)
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(uri.String())
	if err != nil {
		t.Fatalf("compile schema %s: %v", path, err)
	}
	return schema
}
