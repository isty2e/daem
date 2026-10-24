package apply

import (
	"bytes"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestNativeDocumentNarrowingWithoutHistoricalContent(t *testing.T) {
	for _, test := range []struct {
		name, baseline, current, desired string
		refuse, recordOnly               bool
	}{
		{name: "unchanged file permits update", baseline: `{"model":"balanced","released":1}`, current: `{"model":"balanced","released":1}`, desired: `{"model":"fast"}`},
		{name: "already desired keeps escaped bytes", baseline: `{"model":"balanced"}`, current: `{ "model":"\u0066ast", "released":9 }`, desired: `{"model":"fast"}`, recordOnly: true},
		{name: "release all after outside change", baseline: `{"model":"balanced"}`, current: `{"model":"custom","released":9}`, desired: `{}`, recordOnly: true},
		{name: "outside change needs capture before update", baseline: `{"model":"balanced","released":1}`, current: `{"model":"balanced","released":9}`, desired: `{"model":"fast"}`, refuse: true},
		{name: "retained third value needs capture", baseline: `{"model":"balanced"}`, current: `{"model":"custom"}`, desired: `{"model":"fast"}`, refuse: true},
		{name: "missing retained value needs capture", baseline: `{"model":"balanced"}`, current: `{"released":9}`, desired: `{"model":"fast"}`, refuse: true},
		{name: "formatting drift needs capture before update", baseline: `{"model":"balanced"}`, current: `{ "model":"balanced" }`, desired: `{"model":"fast"}`, refuse: true},
		{name: "capture current value permits narrowing", baseline: `{"model":"balanced","released":1}`, current: `{"model":"balanced","released":9}`, desired: `{"model":"balanced"}`, recordOnly: true},
		{name: "equal desired prefix is not partial permission", baseline: `{"a":1,"z":1}`, current: `{"a":2,"z":1}`, desired: `{"a":2,"z":2}`, refuse: true},
		{name: "different prefix is not partial permission", baseline: `{"a":1,"z":1}`, current: `{"a":1,"z":2}`, desired: `{"a":2,"z":2}`, refuse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := narrowingInput(t, configdocument.JSON, test.baseline, test.current, test.desired)
			result, err := BuildNativeDocumentNarrowing(t.Context(), input)
			if test.refuse {
				if err == nil || result.Validate() == nil {
					t.Fatal("unproved value change yielded usable narrowing")
				}
				return
			}
			if err != nil || result.Validate() != nil || !result.ExpectedDocument().Exists() {
				t.Fatalf("valid narrowing required stored historical content: %v", err)
			}
			if test.recordOnly && !bytes.Equal(result.ExpectedDocument().Content(), input.Current.Content()) {
				t.Fatal("scope-only narrowing rewrote current bytes")
			}
			if !test.recordOnly && !narrowingLookup(t, narrowingParse(t, configdocument.JSON, result.ExpectedDocument().Content()), "model").Equal(
				narrowingLookup(t, narrowingParse(t, configdocument.JSON, []byte(test.desired)), "model"),
			) {
				t.Fatal("retained value did not update")
			}
		})
	}
}

func TestNativeDocumentNarrowingEmptyTOMLIsAVerifiedCurrentBaseline(t *testing.T) {
	input := narrowingInput(t, configdocument.TOML, "", "", "model='fast'\n")
	result, err := BuildNativeDocumentNarrowing(t.Context(), input)
	if err != nil || result.Validate() != nil || result.WholeFileChanged() {
		t.Fatalf("empty existing TOML confused with unavailable historical bytes: %v", err)
	}
	after := narrowingParse(t, configdocument.TOML, result.ExpectedDocument().Content())
	want := narrowingParse(t, configdocument.TOML, []byte("model='fast'\n"))
	if !narrowingLookup(t, after, "model").Equal(narrowingLookup(t, want, "model")) {
		t.Fatal("new retained value was not created in unchanged empty TOML")
	}
}

func TestNativeDocumentNarrowingUnchangedFileUpdatesAllGrammars(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			current, desired := `{"model":"balanced","released":9}`, `{"model":"fast"}`
			if syntax == configdocument.JSONC || syntax == configdocument.JSONLineComments {
				current = "// retained comment\n" + current
			}
			if syntax == configdocument.TOML {
				current, desired = "# retained comment\nmodel='balanced'\nreleased=9\n", "model='fast'\n"
			}
			input := narrowingInput(t, syntax, current, current, desired)
			result, err := BuildNativeDocumentNarrowing(t.Context(), input)
			if err != nil || result.Validate() != nil || result.WholeFileChanged() {
				t.Fatalf("hash-equal current file was not a usable baseline: %v", err)
			}
			before := narrowingParse(t, syntax, []byte(current))
			after := narrowingParse(t, syntax, result.ExpectedDocument().Content())
			want := narrowingParse(t, syntax, []byte(desired))
			if !narrowingLookup(t, after, "model").Equal(narrowingLookup(t, want, "model")) ||
				!narrowingLookup(t, after, "released").Equal(narrowingLookup(t, before, "released")) {
				t.Fatal("hash-equal update lost retained or released values")
			}
			beforeComments, afterComments := before.Comments(), after.Comments()
			slices.Sort(beforeComments)
			slices.Sort(afterComments)
			if !slices.Equal(beforeComments, afterComments) {
				t.Fatal("hash-equal update lost comments")
			}
		})
	}
}

func TestNativeDocumentNarrowingEmptyScopeStillRequiresSafeInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*NativeDocumentNarrowingInput)
	}{
		{name: "invalid ownership", edit: func(i *NativeDocumentNarrowingInput) { i.Baseline = durable.ManagedPathState{} }},
		{name: "missing file", edit: func(i *NativeDocumentNarrowingInput) { i.Current = aggregate.AbsentDocument() }},
		{name: "malformed file", edit: func(i *NativeDocumentNarrowingInput) { i.Current = aggregate.ExistingDocument([]byte("{")) }},
		{name: "non object file", edit: func(i *NativeDocumentNarrowingInput) { i.Current = aggregate.ExistingDocument([]byte("[]")) }},
		{name: "invalid grammar", edit: func(i *NativeDocumentNarrowingInput) { i.CodecID = "unknown" }},
		{name: "document bounds", edit: func(i *NativeDocumentNarrowingInput) { i.DocumentLimits.MaximumBytes = 1 }},
		{name: "missing value policy", edit: func(i *NativeDocumentNarrowingInput) { i.ValueLimits.MaximumBytes = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := narrowingInput(t, configdocument.JSON, `{"old":1}`, `{"local":9}`, `{}`)
			test.edit(&input)
			result, err := BuildNativeDocumentNarrowing(t.Context(), input)
			if err == nil || result.Validate() == nil {
				t.Fatal("empty scope bypassed current-file or ownership validation")
			}
		})
	}
}
