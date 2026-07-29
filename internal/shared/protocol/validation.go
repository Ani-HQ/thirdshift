package protocol

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Validator struct {
	schemasDir string
	cache      map[MessageType]*jsonschema.Schema
}

func NewValidator(schemasDir string) (*Validator, error) {
	if schemasDir == "" {
		var err error
		schemasDir, err = DefaultSchemasDir()
		if err != nil {
			return nil, err
		}
	}
	return &Validator{
		schemasDir: schemasDir,
		cache:      map[MessageType]*jsonschema.Schema{},
	}, nil
}

func DefaultSchemasDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "packages", "protocol", "schemas")
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("protocol schemas directory not found from %q", wd)
		}
	}
}

func (v *Validator) ValidateEnvelope(data []byte) (Envelope, error) {
	envelope, err := Unmarshal(data)
	if err != nil {
		return Envelope{}, err
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return Envelope{}, fmt.Errorf("decode protocol document: %w", err)
	}
	schema, err := v.schemaFor(envelope.Type)
	if err != nil {
		return Envelope{}, err
	}
	if err := schema.Validate(document); err != nil {
		return Envelope{}, fmt.Errorf("validate %s envelope: %w", envelope.Type, err)
	}
	return envelope, nil
}

func (v *Validator) MarshalAndValidate(envelope Envelope) ([]byte, error) {
	data, err := Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if _, err := v.ValidateEnvelope(data); err != nil {
		return nil, err
	}
	return data, nil
}

func (v *Validator) schemaFor(typ MessageType) (*jsonschema.Schema, error) {
	if schema := v.cache[typ]; schema != nil {
		return schema, nil
	}
	path := filepath.Join(v.schemasDir, string(typ)+".schema.json")
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("absolute schema path for %s: %w", typ, err)
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(uri.String())
	if err != nil {
		return nil, fmt.Errorf("compile schema for %s: %w", typ, err)
	}
	v.cache[typ] = schema
	return schema, nil
}
