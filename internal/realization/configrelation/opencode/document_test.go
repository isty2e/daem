package opencode

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndRemoveExactSourcePreservesJSONC(t *testing.T) {
	t.Parallel()

	input := []byte(`{
  // unrelated member
  "theme": "dark",
  "plugin": [
    "alpha@1",
    // selected row
    ["beta@2", {"flag": true}],
    "gamma@3", // retained row
  ],
  "future": {"unknown": [1, 2, 3]},
}
`)
	document, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := document.ExactSourceCount("beta@2"); got != 1 {
		t.Fatalf("ExactSourceCount(beta@2) = %d, want 1", got)
	}

	output, changed, err := document.RemoveExactSource("beta@2")
	if err != nil {
		t.Fatalf("RemoveExactSource: %v", err)
	}
	if !changed {
		t.Fatal("RemoveExactSource changed = false, want true")
	}
	for _, retained := range []string{
		"// unrelated member",
		`"theme": "dark"`,
		`"alpha@1"`,
		`"gamma@3", // retained row`,
		`"future": {"unknown": [1, 2, 3]}`,
	} {
		if !bytes.Contains(output, []byte(retained)) {
			t.Fatalf("output lost retained bytes %q:\n%s", retained, output)
		}
	}
	if bytes.Contains(output, []byte(`"beta@2"`)) {
		t.Fatalf("output retained selected source:\n%s", output)
	}
	if _, err := Parse(output); err != nil {
		t.Fatalf("Parse(output): %v\n%s", err, output)
	}
}

func TestParseAtDerivesOpenCodeLoadIdentityWithoutLosingExactSource(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "opencode.jsonc")
	document, err := ParseAt([]byte(`{
  "plugin": [
    "@acme/tool@1.2.3",
    "@acme/tool-next@beta",
    "./plugins/local.ts"
  ]
}`), configPath)
	if err != nil {
		t.Fatalf("ParseAt: %v", err)
	}
	entries := document.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries = %#v", entries)
	}
	if entries[0].Source() != "@acme/tool@1.2.3" ||
		entries[0].HostLoadIdentity() != "@acme/tool" {
		t.Fatalf("versioned entry = %#v", entries[0])
	}
	if entries[1].Source() != "@acme/tool-next@beta" ||
		entries[1].HostLoadIdentity() != "@acme/tool-next" {
		t.Fatalf("tagged entry = %#v", entries[1])
	}
	wantLocal := "file://" + filepath.ToSlash(
		filepath.Join(filepath.Dir(configPath), "plugins", "local.ts"),
	)
	if entries[2].Source() != "./plugins/local.ts" ||
		entries[2].HostLoadIdentity() != wantLocal {
		t.Fatalf(
			"local entry = (%q, %q), want exact source and load identity %q",
			entries[2].Source(),
			entries[2].HostLoadIdentity(),
			wantLocal,
		)
	}
}

func TestParseAtRejectsUnsafePluginFileURL(t *testing.T) {
	t.Parallel()

	_, err := ParseAt(
		[]byte(`{"plugin":["file://other-host/tmp/plugin.ts"]}`),
		filepath.Join(t.TempDir(), "opencode.json"),
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported authority") {
		t.Fatalf("ParseAt error = %v", err)
	}
}

func TestRemoveExactSourceAbsenceIsByteExactNoop(t *testing.T) {
	t.Parallel()

	input := []byte("{\n  \"plugin\": [\"alpha\"],\n}\n")
	document, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	output, changed, err := document.RemoveExactSource("beta")
	if err != nil {
		t.Fatalf("RemoveExactSource: %v", err)
	}
	if changed {
		t.Fatal("RemoveExactSource changed = true, want false")
	}
	if !bytes.Equal(output, input) {
		t.Fatalf("no-op changed bytes:\n got %q\nwant %q", output, input)
	}
}

func TestRemoveExactSourceRejectsDuplicateExactRows(t *testing.T) {
	t.Parallel()

	document, err := Parse([]byte(`{"plugin":["alpha",["alpha",{}]]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	_, _, err = document.RemoveExactSource("alpha")
	if err == nil || !strings.Contains(err.Error(), "2 exact plugin rows") {
		t.Fatalf("RemoveExactSource error = %v, want duplicate-exact refusal", err)
	}
}

func TestRemoveExactSourcePreservesBytesOutsideSelectedElement(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input    string
		source   string
		expected string
	}{
		"first": {
			input:    `{"plugin":[/* selected */"alpha", /* retained */ "beta",], "x": 1,}`,
			source:   "alpha",
			expected: `{"plugin":[ /* retained */ "beta",], "x": 1,}`,
		},
		"middle": {
			input:    `{"plugin":["alpha", /* selected */ "beta", /* retained */ "gamma",],}`,
			source:   "beta",
			expected: `{"plugin":["alpha", /* retained */ "gamma",],}`,
		},
		"last without trailing comma": {
			input:    `{"plugin":["alpha", /* selected */ "beta"],}`,
			source:   "beta",
			expected: `{"plugin":["alpha"],}`,
		},
		"last with trailing comma": {
			input:    `{"plugin":["alpha", /* selected */ "beta",],}`,
			source:   "beta",
			expected: `{"plugin":["alpha",],}`,
		},
		"only": {
			input:    `{"plugin":[ /* selected */ "alpha",],}`,
			source:   "alpha",
			expected: `{"plugin":[],}`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			document, err := Parse([]byte(test.input))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			output, changed, err := document.RemoveExactSource(test.source)
			if err != nil {
				t.Fatalf("RemoveExactSource: %v", err)
			}
			if !changed {
				t.Fatal("RemoveExactSource changed = false, want true")
			}
			if string(output) != test.expected {
				t.Fatalf("output:\n got %q\nwant %q", output, test.expected)
			}
		})
	}
}

func TestParseRejectsUnsupportedPluginShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"root array":             `[]`,
		"duplicate plugin field": `{"plugin":[],"plugin":[]}`,
		"plugin not array":       `{"plugin":{}}`,
		"number row":             `{"plugin":[1]}`,
		"short tuple":            `{"plugin":[["alpha"]]}`,
		"long tuple":             `{"plugin":[["alpha",{},true]]}`,
		"tuple source":           `{"plugin":[[1,{}]]}`,
		"tuple options":          `{"plugin":[["alpha",true]]}`,
		"blank source":           `{"plugin":[" "]}`,
		"control source":         `{"plugin":["alpha\u0001"]}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatalf("Parse(%s) succeeded, want error", input)
			}
		})
	}
}

func TestParseWithoutPluginFieldIsEmpty(t *testing.T) {
	t.Parallel()

	document, err := Parse([]byte(`{"theme":"dark"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := document.entries; len(got) != 0 {
		t.Fatalf("Entries = %v, want empty", got)
	}
}
