package declaration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/encoding/toml"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

const editorSchemaResource = "https://daem.invalid/manifest.schema.json"

func loadEditorSchema(t *testing.T) (map[string]any, *jsonschema.Compiler, *jsonschema.Schema) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "schemas", "daem.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	if document["$schema"] != "http://json-schema.org/draft-04/schema#" {
		t.Fatal("the artifact does not declare Draft 4")
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft4
	compiler.LoadURL = func(url string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("external schema loading refused: %s", url)
	}
	if err := compiler.AddResource(editorSchemaResource, bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(editorSchemaResource)
	if err != nil {
		t.Fatal(err)
	}
	return document, compiler, schema
}

func editorSchemaVerdict(schema *jsonschema.Schema, value any) (bool, error) {
	err := schema.Validate(value)
	if err == nil {
		return true, nil
	}
	var refusal *jsonschema.ValidationError
	if errors.As(err, &refusal) {
		return false, nil
	}
	return false, err
}

func editorSchemaAccepts(t *testing.T, schema *jsonschema.Schema, value any) bool {
	t.Helper()
	accepted, err := editorSchemaVerdict(schema, value)
	if err != nil {
		t.Fatalf("schema validator failed without a verdict: %v", err)
	}
	return accepted
}

func editorTOMLValues(content []byte) (map[string]any, error) {
	return tomlsyntax.Native(context.Background(), content)
}

func editorJSONValue(value any) (any, error) {
	switch typed := value.(type) {
	case string, bool, int64:
		return typed, nil
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return nil, fmt.Errorf("non-finite TOML number is outside the JSON instance model")
		}
		return typed, nil
	case time.Time:
		return typed.Format(time.RFC3339Nano), nil
	case tomlsyntax.Temporal:
		layouts := map[string]string{
			"offset-date-time": time.RFC3339Nano,
			"local-date-time":  "2006-01-02T15:04:05.999999999",
			"local-date":       "2006-01-02",
			"local-time":       "15:04:05.999999999",
		}
		parsed, err := time.Parse(layouts[typed.Kind], typed.Text)
		if err != nil {
			return nil, err
		}
		return parsed.Format(time.RFC3339Nano), nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			projected, err := editorJSONValue(child)
			if err != nil {
				return nil, err
			}
			result[key] = projected
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			projected, err := editorJSONValue(child)
			if err != nil {
				return nil, err
			}
			result[index] = projected
		}
		return result, nil
	case []map[string]any:
		result := make([]any, len(typed))
		for index, child := range typed {
			projected, err := editorJSONValue(child)
			if err != nil {
				return nil, err
			}
			result[index] = projected
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported TOML projection value %T", value)
	}
}

func editorSchemaInstance(t *testing.T, content []byte) any {
	t.Helper()
	parsed, err := editorTOMLValues(content)
	if err != nil {
		t.Fatalf("invalid TOML comparison fixture: %v", err)
	}
	projected, err := editorJSONValue(parsed)
	if err != nil {
		t.Fatalf("TOML projection failed: %v", err)
	}
	return projected
}

func TestManifestEditorSchemaValueProjection(t *testing.T) {
	content := []byte(`integer = 9007199254740993
fraction = 1.5
dates = [1979-05-27T07:32:00Z, 1979-05-27T07:32:00, 1979-05-27, 07:32:00]
[[rows]]
when = 1979-05-27T07:32:00-07:00
`)
	projected := editorSchemaInstance(t, content).(map[string]any)
	if integer, ok := projected["integer"].(int64); !ok || integer != 9007199254740993 {
		t.Fatalf("integer precision/type was lost: %#v", projected["integer"])
	}
	if fraction, ok := projected["fraction"].(float64); !ok || fraction != 1.5 {
		t.Fatalf("fraction type was lost: %#v", projected["fraction"])
	}
	for _, value := range projected["dates"].([]any) {
		if _, ok := value.(string); !ok {
			t.Fatalf("temporal value was not projected as string: %T", value)
		}
	}
	rows := projected["rows"].([]any)
	if rows[0].(map[string]any)["when"] != "1979-05-27T07:32:00-07:00" {
		t.Fatalf("offset datetime changed: %#v", rows[0])
	}
	if _, err := editorJSONValue(struct{}{}); err == nil {
		t.Fatal("unsupported native value did not produce a setup failure")
	}
	if _, err := editorJSONValue(math.Inf(1)); err == nil {
		t.Fatal("non-JSON number did not produce a setup failure")
	}
}

func TestManifestEditorSchemaVerdictClassification(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft4
	if err := compiler.AddResource(editorSchemaResource, bytes.NewBufferString(`{"type":"integer"}`)); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(editorSchemaResource)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		value any
		want  bool
	}{
		{int64(1), true}, {"not an integer", false},
	} {
		accepted, err := editorSchemaVerdict(schema, test.value)
		if err != nil || accepted != test.want {
			t.Fatalf("value=%#v accepted=%t error=%v", test.value, accepted, err)
		}
	}
	if _, err := editorSchemaVerdict(schema, time.Time{}); err == nil {
		t.Fatal("native input-type error was counted as a schema verdict")
	}
}
