package apply

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

var narrowingLimits = configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}

func narrowingParse(t *testing.T, syntax configdocument.Syntax, content []byte) configdocument.Document {
	t.Helper()
	document, err := configdocument.Parse(t.Context(), content, syntax, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func narrowingLookup(t *testing.T, document configdocument.Document, segments ...string) configdocument.Value {
	t.Helper()
	key, err := output.NewKeyPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	value, present, err := document.Lookup(key)
	if err != nil || !present {
		t.Fatalf("expected selected value at %v: %v", segments, err)
	}
	return value
}
