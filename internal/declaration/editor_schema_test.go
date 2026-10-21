package declaration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

func TestManifestEditorSchemaArtifact(t *testing.T) {
	generated, err := manifestEditorSchema()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkEditorSchemaVocabulary(generated); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft4
	compiler.LoadURL = func(url string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("external schema load refused: %s", url)
	}
	const resource = "https://daem.invalid/manifest.schema.json"
	if err := compiler.AddResource(resource, bytes.NewReader(generated)); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(resource); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join("..", "..", "schemas", "daem.schema.json")
	if os.Getenv("DAEM_UPDATE_MANIFEST_SCHEMA") == "1" {
		if os.Getenv("CI") != "" {
			t.Fatal("schema regeneration is disabled in CI")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		temporary, err := os.CreateTemp(filepath.Dir(path), ".daem-schema-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(temporary.Name())
		if _, err := temporary.Write(generated); err != nil {
			temporary.Close()
			t.Fatal(err)
		}
		if err := temporary.Chmod(0o644); err != nil {
			temporary.Close()
			t.Fatal(err)
		}
		if err := temporary.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temporary.Name(), path); err != nil {
			t.Fatal(err)
		}
	}

	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, committed) {
		want := strings.Split(string(generated), "\n")
		got := strings.Split(string(committed), "\n")
		for index := 0; index < max(len(want), len(got)); index++ {
			var before, after string
			if index < len(got) {
				before = got[index]
			}
			if index < len(want) {
				after = want[index]
			}
			if before != after {
				t.Fatalf("schema artifact is stale at line %d (-committed +generated):\n-%s\n+%s\nregenerate with DAEM_UPDATE_MANIFEST_SCHEMA=1 and -count=1", index+1, before, after)
			}
		}
		t.Fatal("schema artifact is stale: trailing bytes differ; regenerate with DAEM_UPDATE_MANIFEST_SCHEMA=1 and -count=1")
	}
	second, err := manifestEditorSchema()
	if err != nil || !bytes.Equal(generated, second) {
		t.Fatalf("schema generation is not deterministic: %v", err)
	}
}

func checkEditorSchemaVocabulary(content []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(content, &root); err != nil {
		return err
	}
	var dialect string
	if err := json.Unmarshal(root["$schema"], &dialect); err != nil || dialect != editorSchemaDialect {
		return fmt.Errorf("editor schema must declare Draft 4")
	}
	return checkEditorSchemaNode(root, true)
}

func checkEditorSchemaNode(node map[string]json.RawMessage, root bool) error {
	_, reference := node["$ref"]
	for key, value := range node {
		if reference && key != "$ref" && !(root && (key == "$schema" || key == "definitions")) {
			return fmt.Errorf("ignored sibling keyword %q beside $ref", key)
		}
		switch key {
		case "$schema":
			if !root {
				return fmt.Errorf("nested schema dialect override")
			}
		case "$ref":
			var ref string
			if err := json.Unmarshal(value, &ref); err != nil || !strings.HasPrefix(ref, "#/definitions/") {
				return fmt.Errorf("editor schema reference must be document-local")
			}
		case "properties", "definitions":
			var properties map[string]map[string]json.RawMessage
			if err := json.Unmarshal(value, &properties); err != nil {
				return err
			}
			for _, child := range properties {
				if err := checkEditorSchemaNode(child, false); err != nil {
					return err
				}
			}
		case "oneOf", "anyOf":
			var alternatives []map[string]json.RawMessage
			if err := json.Unmarshal(value, &alternatives); err != nil {
				return err
			}
			for _, child := range alternatives {
				if err := checkEditorSchemaNode(child, false); err != nil {
					return err
				}
			}
		case "additionalProperties", "items":
			if key == "additionalProperties" && (bytes.Equal(value, []byte("false")) || bytes.Equal(value, []byte("true"))) {
				continue
			}
			var child map[string]json.RawMessage
			if err := json.Unmarshal(value, &child); err != nil || child == nil {
				return fmt.Errorf("%s must contain a schema object", key)
			}
			if err := checkEditorSchemaNode(child, false); err != nil {
				return err
			}
		case "type", "required", "enum", "description":
		default:
			return fmt.Errorf("unsupported editor schema keyword %q", key)
		}
	}
	return nil
}

func TestManifestEditorSchemaVocabulary(t *testing.T) {
	valid := []byte(`{"$schema":"http://json-schema.org/draft-04/schema#","type":"object","properties":{"format":{"type":"string"},"not":{"type":"string"},"$schema":{"type":"string"}}}`)
	if err := checkEditorSchemaVocabulary(valid); err != nil {
		t.Fatal(err)
	}
	for _, property := range []string{"pattern", "patternProperties", "format", "dependencies", "not", "const", "minItems", "default", "$defs"} {
		t.Run(property, func(t *testing.T) {
			invalid := fmt.Appendf(nil, `{"$schema":"%s","%s":{}}`, editorSchemaDialect, property)
			if err := checkEditorSchemaVocabulary(invalid); err == nil {
				t.Fatal("unsupported schema keyword was accepted")
			}
		})
	}
	for _, invalid := range []string{
		`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}`,
		`{"$schema":"http://json-schema.org/draft-04/schema#","$ref":"https://example.invalid/schema"}`,
		`{"$schema":"http://json-schema.org/draft-04/schema#","$ref":"#/definitions/A","description":"ignored"}`,
	} {
		if err := checkEditorSchemaVocabulary([]byte(invalid)); err == nil {
			t.Fatalf("invalid schema vocabulary was accepted: %s", invalid)
		}
	}
}

type editorRecursiveFixture struct {
	Next *editorRecursiveFixture `toml:"next"`
}

type editorUnprojectedFixture struct{}

func (*editorUnprojectedFixture) UnmarshalTOML(any) error { return nil }

func TestManifestEditorSchemaRejectsUnsupportedDefinitions(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[struct{ Value string }](),
		reflect.TypeFor[struct {
			Value string `toml:",omitempty"`
		}](),
		reflect.TypeFor[struct{ Source }](),
		reflect.TypeFor[map[int]string](),
		reflect.TypeFor[func()](),
		reflect.TypeFor[any](),
		reflect.TypeFor[editorRecursiveFixture](),
		reflect.TypeFor[editorUnprojectedFixture](),
	} {
		t.Run(typ.String(), func(t *testing.T) {
			builder := editorSchemaBuilder{
				definitions: make(map[string]*editorSchemaNode),
				names:       make(map[string]reflect.Type), active: make(map[reflect.Type]bool),
			}
			if _, err := builder.node(typ); err == nil {
				t.Fatal("unsupported definition produced a schema")
			}
		})
	}
}
