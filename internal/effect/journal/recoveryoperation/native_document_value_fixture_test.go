package recoveryoperation

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func nativeRecordJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func nativeRecordCanonical(t *testing.T, raw string) string {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	document, err := configdocument.Parse(t.Context(), []byte(`{"value":`+raw+`}`), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("value")
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := document.Lookup(key)
	if err != nil || !found {
		t.Fatalf("canonical fixture value: %v", err)
	}
	canonical, err := value.CanonicalContext(t.Context(), limits)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
