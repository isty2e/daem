package pi_test

import (
	"bytes"
	"strings"
	"testing"

	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
)

func TestPermutePackageRowsMovesCompleteValuesOnly(t *testing.T) {
	content := []byte("{\r\n" +
		"  \"packages\": [\r\n" +
		"    \"npm:@acme/alpha@1\",\r\n" +
		"    { \"source\": \"../local\", \"extensions\": [\"main.ts\"] },\r\n" +
		"    \"github:acme/tool#v2\"\r\n" +
		"  ],\r\n" +
		"  \"unknown\": {\"label\": \"그대로\"}\r\n" +
		"}\r\n")
	document, err := piconfig.Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	candidate, changed, err := document.PermutePackageRows([]int{2, 1, 0})
	if err != nil {
		t.Fatalf("PermutePackageRows: %v", err)
	}
	if !changed {
		t.Fatal("PermutePackageRows reported no change")
	}
	want := []byte("{\r\n" +
		"  \"packages\": [\r\n" +
		"    \"github:acme/tool#v2\",\r\n" +
		"    { \"source\": \"../local\", \"extensions\": [\"main.ts\"] },\r\n" +
		"    \"npm:@acme/alpha@1\"\r\n" +
		"  ],\r\n" +
		"  \"unknown\": {\"label\": \"그대로\"}\r\n" +
		"}\r\n")
	if !bytes.Equal(candidate, want) {
		t.Fatalf("candidate = %q, want %q", candidate, want)
	}
}

func TestPermutePackageRowsValidatesPermutationAndIdempotentIdentity(t *testing.T) {
	document, err := piconfig.Parse([]byte(`{"packages":["a",{"source":"b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	candidate, changed, err := document.PermutePackageRows([]int{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if changed || string(candidate) != `{"packages":["a",{"source":"b"}]}` {
		t.Fatalf("identity permutation = %q, changed %t", candidate, changed)
	}
	for _, order := range [][]int{{0}, {0, 0}, {0, 2}, {-1, 0}} {
		if _, _, err := document.PermutePackageRows(order); err == nil {
			t.Fatalf("PermutePackageRows accepted %#v", order)
		}
	}
}

func TestParseRejectsNonPiJSONAndUnsupportedRows(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "comment", content: `{"packages":[/* no */"a"]}`, want: "invalid"},
		{name: "duplicate key", content: `{"packages":[],"packages":[]}`, want: "duplicate object key"},
		{name: "nested duplicate key", content: `{"unknown":{"x":1,"x":2},"packages":[]}`, want: "duplicate object key"},
		{name: "null", content: `{"packages":null}`, want: "must be an array"},
		{name: "null row", content: `{"packages":[null]}`, want: "string or object"},
		{name: "null object source", content: `{"packages":[{"source":null}]}`, want: "object source must be a string"},
		{name: "scalar row", content: `{"packages":[1]}`, want: "string or object"},
		{name: "missing source", content: `{"packages":[{"autoload":true}]}`, want: "source is required"},
		{name: "unknown root", content: `[]`, want: "root must be an object"},
		{name: "multiple values", content: `{"packages":[]} []`, want: "multiple JSON values"},
		{name: "trailing comma", content: `{"packages":["a",]}`, want: "invalid"},
		{name: "escaped NUL source", content: `{"packages":["npm:a\u0000b"]}`, want: "control"},
		{name: "UTF-8 BOM", content: "\ufeff" + `{"packages":[]}`, want: "invalid"},
		{name: "bidi source", content: `{"packages":["npm:a\u202eb"]}`, want: "bidirectional"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := piconfig.Parse([]byte(test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestPermutePackageRowsKeepsDocumentWithoutPackagesExact(t *testing.T) {
	content := []byte(`{"theme":"dark","unknown":{"large":1e999}}`)
	document, err := piconfig.Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	candidate, changed, err := document.PermutePackageRows(nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed || !bytes.Equal(candidate, content) {
		t.Fatalf("candidate = %q, changed %t", candidate, changed)
	}
}

func TestPermutePackageRowsPreservesEscapesAndNestedOptionBytes(t *testing.T) {
	content := []byte(`{"packages":[{"source":"npm:\u0061@1","options":{"n":1e999}},"npm:b@1"]}`)
	document, err := piconfig.Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	candidate, changed, err := document.PermutePackageRows([]int{1, 0})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"packages":["npm:b@1",{"source":"npm:\u0061@1","options":{"n":1e999}}]}`)
	if !changed || !bytes.Equal(candidate, want) {
		t.Fatalf("candidate = %q, changed %t", candidate, changed)
	}
}

func TestParseEnforcesPiSettingsDepthLimit(t *testing.T) {
	content := `{"packages":[],"nested":`
	for range 33 {
		content += "["
	}
	content += "0"
	for range 33 {
		content += "]"
	}
	content += "}"
	_, err := piconfig.Parse([]byte(content))
	if err == nil || !strings.Contains(err.Error(), "maximum depth 32") {
		t.Fatalf("Parse error = %v", err)
	}
}
