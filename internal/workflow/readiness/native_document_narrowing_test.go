package readiness

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

var narrowingLimits = configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}

func TestNativeDocumentNarrowingPreservesReleasedDriftAndComments(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.JSONC, configdocument.JSONLineComments, configdocument.TOML} {
		t.Run(syntax.String(), func(t *testing.T) {
			baseline := `{"model":"balanced","temperature":0.2,"custom":{"keep":1}}`
			current := `{"model":"fast","temperature":0.9,"custom":{"keep":4},"local":[3]}`
			desired := `{"model":"fast"}`
			if syntax == configdocument.JSONC || syntax == configdocument.JSONLineComments {
				current = "// same comment\n// same comment\n" + current
			}
			if syntax == configdocument.TOML {
				baseline = "model='balanced'\ntemperature=0.2\n[custom]\nkeep=1\n"
				current = "# same comment\n# same comment\nmodel='fast'\ntemperature=0.9\nlocal=[3]\n[custom]\nkeep=4\n"
				desired = "model='fast'\n"
			}
			input := narrowingInput(t, syntax, baseline, current, desired)
			result, err := BuildNativeDocumentNarrowing(t.Context(), input)
			if err != nil || result.Validate() != nil || !result.WholeFileChanged() {
				t.Fatalf("released drift blocked narrowing: %v", err)
			}
			before := narrowingParse(t, syntax, input.Current.Content())
			after := narrowingParse(t, syntax, result.ExpectedDocument().Content())
			want := narrowingParse(t, syntax, []byte(desired))
			for _, segments := range [][]string{{"temperature"}, {"custom", "keep"}, {"local"}} {
				if !narrowingLookup(t, before, segments...).Equal(narrowingLookup(t, after, segments...)) {
					t.Fatalf("released current value changed at %v", segments)
				}
			}
			if !narrowingLookup(t, after, "model").Equal(narrowingLookup(t, want, "model")) {
				t.Fatal("retained model did not update to desired value")
			}
			beforeComments, afterComments := before.Comments(), after.Comments()
			slices.Sort(beforeComments)
			slices.Sort(afterComments)
			if !slices.Equal(beforeComments, afterComments) {
				t.Fatal("narrowing lost current comments or repeated comment occurrences")
			}
			plan, present := result.KeyPlan()
			if !present || len(plan.Intents()) != 1 {
				t.Fatal("narrowing created released-key removal work")
			}
			if _, desired := plan.Intents()[0].Desired(); !desired {
				t.Fatal("retained key became a deletion intent")
			}
			if !bytes.Equal(input.Current.Content(), []byte(current)) {
				t.Fatal("pure narrowing changed its current input")
			}
		})
	}
}

func TestNativeDocumentNarrowingUsesWholeHashOrDesiredValues(t *testing.T) {
	for _, test := range []struct {
		name, current string
		refuse        bool
	}{
		{name: "unchanged", current: `{"model":"balanced","released":1}`},
		{name: "already desired", current: `{"model":"fast","released":9}`},
		{name: "formatting only", current: "{\n  \"released\": 1, \"model\": \"balanced\"\n}", refuse: true},
		{name: "retained drift", current: `{"model":"custom","released":9}`, refuse: true},
		{name: "retained missing", current: `{"released":9}`, refuse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := narrowingInput(t, configdocument.JSON, `{"model":"balanced","released":1}`, test.current, `{"model":"fast"}`)
			result, err := BuildNativeDocumentNarrowing(t.Context(), input)
			if test.refuse {
				if !errors.Is(err, reconcileprojection.ErrNativeNarrowingNeedsCapture) || result.Validate() == nil {
					t.Fatalf("retained drift yielded usable narrowing: %v", err)
				}
			} else if err != nil || result.Validate() != nil {
				t.Fatalf("valid retained state refused narrowing: %v", err)
			}
		})
	}
}

func TestNativeDocumentNarrowingEmptyScopeKeepsTheFile(t *testing.T) {
	input := narrowingInput(t, configdocument.JSONC, `{"model":"balanced"}`, "// local\n{ \"model\":\"custom\", \"released\":9 }", `{}`)
	result, err := BuildNativeDocumentNarrowing(t.Context(), input)
	if err != nil || !result.ExpectedDocument().Exists() || !bytes.Equal(result.ExpectedDocument().Content(), input.Current.Content()) {
		t.Fatalf("empty-scope release changed the current file: %v", err)
	}
	if _, present := result.KeyPlan(); present {
		t.Fatal("release-only narrowing manufactured an empty key plan")
	}
	var zero NativeDocumentNarrowing
	if zero.Validate() == nil {
		t.Fatal("zero narrowing is valid")
	}
	if _, present := zero.KeyPlan(); present {
		t.Fatal("zero narrowing exposed a plan")
	}
}

func TestNativeDocumentNarrowingDoesNotTurnLaterKeyRemovalIntoFileRemoval(t *testing.T) {
	input := narrowingInput(t, configdocument.JSON, `{"model":"balanced"}`, `{"model":"balanced"}`, `{"model":"fast"}`)
	result, err := BuildNativeDocumentNarrowing(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := nativecodec.New(t.Context(), input.CodecID, input.DocumentLimits, input.ValueLimits)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := result.KeyPlan()
	selection, err := plan.Before().Selection()
	if err != nil {
		t.Fatal(err)
	}
	current, failure := codec.Read(result.ExpectedDocument(), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	remove, err := aggregate.NewProjectionIntent(current.States()[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	retirement, err := aggregate.NewPlan(current, []aggregate.ProjectionIntent{remove})
	if err != nil {
		t.Fatal(err)
	}
	after, failure := codec.Render(result.ExpectedDocument(), retirement)
	if failure != nil || !after.Document().Exists() {
		t.Fatalf("last managed-key retirement removed the file: %v", failure)
	}
	leaves, err := narrowingParse(t, configdocument.JSON, after.Document().Content()).Leaves()
	if err != nil || len(leaves) != 0 {
		t.Fatalf("last managed key was not deleted: %v", err)
	}
}

func TestNativeDocumentNarrowingRefusesInvalidOwnershipOrCurrent(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*NativeDocumentNarrowingInput)
	}{
		{name: "zero record", edit: func(i *NativeDocumentNarrowingInput) { i.Baseline = durable.ManagedPathState{} }},
		{name: "missing current", edit: func(i *NativeDocumentNarrowingInput) { i.Current = aggregate.AbsentDocument() }},
		{name: "malformed current", edit: func(i *NativeDocumentNarrowingInput) { i.Current = aggregate.ExistingDocument([]byte("{")) }},
		{name: "array current", edit: func(i *NativeDocumentNarrowingInput) { i.Current = aggregate.ExistingDocument([]byte("[]")) }},
		{name: "document limit", edit: func(i *NativeDocumentNarrowingInput) { i.DocumentLimits.MaximumBytes = 1 }},
		{name: "value limit", edit: func(i *NativeDocumentNarrowingInput) { i.ValueLimits.MaximumBytes = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := narrowingInput(t, configdocument.JSON, `{"model":"balanced"}`, `{"model":"fast","external":1}`, `{"model":"fast"}`)
			test.edit(&input)
			result, err := BuildNativeDocumentNarrowing(t.Context(), input)
			if err == nil || result.Validate() == nil {
				t.Fatal("invalid evidence exposed a usable narrowing result")
			}
		})
	}
	input := narrowingInput(t, configdocument.JSON, `{"model":"balanced"}`, `{"model":"fast"}`, `{"model":"fast"}`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := BuildNativeDocumentNarrowing(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost its error classification: %v", err)
	}
	if result, err := BuildNativeDocumentNarrowing(nil, input); err == nil || result.Validate() == nil {
		t.Fatal("missing operation context was inferred")
	}
}

func TestNativeDocumentNarrowingHandlesGeneralAndAtomicKeys(t *testing.T) {
	for _, syntax := range []configdocument.Syntax{configdocument.JSON, configdocument.TOML} {
		baseline := `{"":"old","a/b":[1],"~":{},"settings":{"model":"balanced"},"released":1}`
		current := baseline
		desired := `{"":"new","a/b":[2],"~":{},"settings":{"model":"fast"}}`
		if syntax == configdocument.TOML {
			baseline = "''='old'\n'a/b'=[1]\n'~'={}\nreleased=1\n[settings]\nmodel='balanced'"
			current = baseline
			desired = "''='new'\n'a/b'=[2]\n'~'={}\n[settings]\nmodel='fast'"
		}
		input := narrowingInput(t, syntax, baseline, current, desired)
		result, err := BuildNativeDocumentNarrowing(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		after := narrowingParse(t, syntax, result.ExpectedDocument().Content())
		want := narrowingParse(t, syntax, []byte(desired))
		for _, segments := range [][]string{{""}, {"a/b"}, {"~"}, {"settings", "model"}} {
			if !narrowingLookup(t, after, segments...).Equal(narrowingLookup(t, want, segments...)) {
				t.Fatalf("native key identity/value changed at %v", segments)
			}
		}
	}
	input := narrowingInput(t, configdocument.JSON, `{"settings":{"model":"balanced"}}`, `{"settings":"local"}`, `{"settings":{"model":"fast"}}`)
	if result, err := BuildNativeDocumentNarrowing(t.Context(), input); err == nil || result.Validate() == nil {
		t.Fatalf("scalar ancestor was silently displaced: %v", err)
	}
}

func narrowingInput(t *testing.T, syntax configdocument.Syntax, baseline, current, desired string) NativeDocumentNarrowingInput {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	placement, err := aggregate.HostConfigPlacementID(target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, placement)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@pi-agent/extension-settings/test.json")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(subject, []target.Target{target.TargetPi}, target.ScopeGlobal, destination,
		artifact.HashFileContent([]byte(baseline)), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	codecID, ok := aggregate.HostConfigCodecForSyntax(syntax.String())
	if !ok {
		t.Fatal("fixture syntax lacks native codec")
	}
	leaves, err := narrowingParse(t, syntax, []byte(desired)).Leaves()
	if err != nil {
		t.Fatal(err)
	}
	sets := make([]aggregate.ContributionSet, 0, len(leaves))
	for _, leaf := range leaves {
		footprint, err := output.NewKeyFootprint(leaf.Path)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, destination, footprint, codecID)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := leaf.Value.CanonicalContext(t.Context(), narrowingLimits)
		if err != nil {
			t.Fatal(err)
		}
		body, err := aggregate.NewManagedContribution(contract, canonical)
		if err != nil {
			t.Fatal(err)
		}
		keySubject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, leaf.Path)
		if err != nil {
			t.Fatal(err)
		}
		item, err := aggregate.NewSubjectContribution(keySubject, body)
		if err != nil {
			t.Fatal(err)
		}
		set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
		if err != nil {
			t.Fatal(err)
		}
		sets = append(sets, set)
	}
	return NativeDocumentNarrowingInput{
		Baseline: state, Current: aggregate.ExistingDocument([]byte(current)),
		Desired: sets, CodecID: codecID, DocumentLimits: narrowingLimits, ValueLimits: narrowingLimits,
	}
}

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

func TestNativeDocumentNarrowingAlreadyDesiredIsRecordOnly(t *testing.T) {
	input := narrowingInput(t, configdocument.JSON, `{"model":"balanced","released":1}`,
		"{ \"model\": \"\\u0066ast\", \"released\": 9 }", `{"model":"fast"}`)
	result, err := BuildNativeDocumentNarrowing(t.Context(), input)
	if err != nil || !bytes.Equal(result.ExpectedDocument().Content(), input.Current.Content()) {
		t.Fatalf("already-desired narrowing scheduled a formatting write: %v", err)
	}
}

func TestNativeDocumentNarrowingRefusesForeignDeclarationAndPlacement(t *testing.T) {
	for _, misplaced := range []bool{false, true} {
		input := narrowingInput(t, configdocument.JSON, `{"model":"balanced"}`, `{"model":"balanced"}`, `{"model":"fast"}`)
		item := input.Desired[0].Contributions()[0]
		id, _, _ := topologyprojection.HostConfigKeyIdentity(item.SubjectID())
		body := item.Contribution()
		if misplaced {
			other, err := output.Parse("@pi-agent/extension-settings/other.json")
			if err != nil {
				t.Fatal(err)
			}
			contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, other,
				body.Address().Footprint(), input.CodecID)
			if err != nil {
				t.Fatal(err)
			}
			body, err = aggregate.NewManagedContribution(contract, body.CanonicalContribution())
			if err != nil {
				t.Fatal(err)
			}
		} else {
			var err error
			id, err = entity.New(entity.KindHostConfig, "different-declaration")
			if err != nil {
				t.Fatal(err)
			}
		}
		subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, body.Address().KeyPath())
		if err != nil {
			t.Fatal(err)
		}
		replacement, err := aggregate.NewSubjectContribution(subject, body)
		if err != nil {
			t.Fatal(err)
		}
		set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{replacement})
		if err != nil {
			t.Fatal(err)
		}
		input.Desired = []aggregate.ContributionSet{set}
		if result, err := BuildNativeDocumentNarrowing(t.Context(), input); err == nil || result.Validate() == nil {
			t.Fatal("narrowing transferred another declaration or destination")
		}
	}
}
