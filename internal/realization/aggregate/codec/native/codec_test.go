package nativecodec_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

var fixtureLimits = configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}

func TestNativeCodecRendersAndRestoresActualValuesAcrossGrammars(t *testing.T) {
	for _, test := range []struct {
		syntax configdocument.Syntax
		source string
	}{
		{configdocument.JSON, `{"a":"old","outside":{"keep":1}}`},
		{configdocument.JSONC, "// outside\n{\"a\":\"old\",\"outside\":{\"keep\":1},}"},
		{configdocument.JSONLineComments, "// outside\n{\"a\":\"old\",\"outside\":{\"keep\":1},}"},
		{configdocument.TOML, "# outside\na='old'\noutside={keep=1}\n"},
	} {
		t.Run(test.syntax.String(), func(t *testing.T) {
			codec := fixtureCodec(t, test.syntax)
			set := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, test.syntax, `"new"`))
			original := aggregate.ExistingDocument([]byte(test.source))
			selection := selectSets(t, set)
			before := read(t, codec, original, selection)
			plan := plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key(t, "a"): &set})
			rendered, failure := codec.Render(original, plan)
			if failure != nil {
				t.Fatal(failure)
			}
			if actual := read(t, codec, rendered.Document(), selection); !actual.Equal(rendered.Expected()) {
				t.Fatal("candidate evidence does not describe its actual native bytes")
			}
			if !lookup(t, test.syntax, rendered.Document(), key(t, "a")).Equal(fixtureValue(t, test.syntax, `"new"`)) {
				t.Fatal("desired value was not applied")
			}
			assertComments(t, test.syntax, original, rendered.Document())

			parsed := parse(t, test.syntax, rendered.Document())
			addition, err := configdocument.Set(key(t, "outside", "added"), fixtureValue(t, test.syntax, "true"))
			if err != nil {
				t.Fatal(err)
			}
			concurrent, err := configdocument.Edit(t.Context(), parsed, []configdocument.Change{addition})
			if err != nil {
				t.Fatal(err)
			}
			restored, failure := codec.Restore(aggregate.ExistingDocument(concurrent.Bytes()), before)
			if failure != nil {
				t.Fatal(failure)
			}
			if !read(t, codec, restored.Document(), selection).Equal(before) ||
				!lookup(t, test.syntax, restored.Document(), key(t, "outside", "added")).Equal(fixtureValue(t, test.syntax, "true")) {
				t.Fatal("recovery failed to restore selected values and preserve a fresh outside-scope value")
			}
			assertComments(t, test.syntax, original, restored.Document())
			if !original.Equal(aggregate.ExistingDocument([]byte(test.source))) {
				t.Fatal("pure candidate generation mutated its original document")
			}
		})
	}
}

func TestNativeCodecPreservesLiteralKeyIdentityAndObservedObjects(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	paths := []output.KeyPath{key(t, ""), key(t, "a/b"), key(t, "~"), key(t, "."), key(t, "line\n\x00"), key(t, "object")}
	document := aggregate.ExistingDocument([]byte(`{"":0,"a/b":1,"~":2,".":3,"line\n\u0000":4,"object":{"foreign":true}}`))
	sets := make([]aggregate.ContributionSet, 0, len(paths))
	for _, path := range paths {
		sets = append(sets, fixtureSet(t, codec, path, fixtureValue(t, configdocument.JSON, "true")))
	}
	snapshot := read(t, codec, document, selectSets(t, sets...))
	for _, set := range sets {
		state, found := snapshot.State(set.Contract())
		if !found || !state.Present() {
			t.Fatal("literal native property was lost")
		}
		decoded, err := configdocument.DecodeValue(t.Context(), configdocument.JSON, state.CanonicalProjection(), fixtureLimits)
		if err != nil || !decoded.Equal(lookup(t, configdocument.JSON, document, set.Address().KeyPath())) {
			t.Fatal("actual observed value was collapsed to atomic ownership or a legacy path")
		}
	}
	object := fixtureSet(t, codec, key(t, "object"), fixtureValue(t, configdocument.JSON, `{"nested":1}`))
	if err := codec.ValidateContributions(object); err == nil {
		t.Fatal("nonempty desired object bypassed leaf ownership")
	}
}

func TestNativeCodecOccupancyAndObstructions(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	parent := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, configdocument.JSON, `"old"`))
	child := fixtureSet(t, codec, key(t, "a", "b"), fixtureValue(t, configdocument.JSON, "true"))
	for _, test := range []struct {
		document aggregate.Document
		want     aggregate.ContributionOccupancyState
	}{
		{aggregate.AbsentDocument(), aggregate.ContributionAbsent},
		{aggregate.ExistingDocument([]byte(`{"a":"old"}`)), aggregate.ContributionPresent},
		{aggregate.ExistingDocument([]byte(`{"a":"different"}`)), aggregate.ContributionAbsent},
	} {
		state, _ := read(t, codec, test.document, selectSets(t, parent)).State(parent.Contract())
		occupancy, err := codec.ClassifyContributionOccupancy(state, parent)
		if err != nil {
			t.Fatal(err)
		}
		actual, found := occupancy.State(parent.Contributions()[0].SubjectID())
		if !found || actual != test.want {
			t.Fatal("desired contribution occupancy differs from actual evidence")
		}
	}
	document := aggregate.ExistingDocument([]byte(`{"a":"old"}`))
	blocked := read(t, codec, document, selectSets(t, parent, child))
	state, _ := blocked.State(child.Contract())
	ancestor, found := state.BlockedBy()
	if !found || ancestor != parent.Address().KeyPath() || state.Absent() {
		t.Fatal("selected ancestor obstruction became plain absence")
	}
	occupancy, err := codec.ClassifyContributionOccupancy(state, child)
	if err != nil {
		t.Fatal(err)
	}
	if actual, _ := occupancy.State(child.Contributions()[0].SubjectID()); actual != aggregate.ContributionAmbiguous {
		t.Fatal("blocked traversal established physical contribution absence")
	}
	for _, source := range []string{`{"a":"old"}`, `{"a":[]}`, `{"a":null}`, `[]`, `null`, `"root"`} {
		snapshot, failure := codec.Read(aggregate.ExistingDocument([]byte(source)), selectSets(t, child))
		if failure == nil || failure.Reason() != aggregate.CodecFailureSelectedShapeUnsupported || len(snapshot.States()) != 0 {
			t.Fatal("unselected/root obstruction acquired complete native evidence")
		}
	}
}

func fixtureCodec(t testing.TB, syntax configdocument.Syntax) aggregate.Codec {
	t.Helper()
	id, ok := aggregate.HostConfigCodecForSyntax(syntax.String())
	if !ok {
		t.Fatal("fixture syntax has no native codec identity")
	}
	codec, err := nativecodec.New(t.Context(), id, fixtureLimits, fixtureLimits)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func key(t testing.TB, parts ...string) output.KeyPath {
	t.Helper()
	path, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureSet(t testing.TB, codec aggregate.Codec, path output.KeyPath, value configdocument.Value) aggregate.ContributionSet {
	t.Helper()
	root, err := output.Parse("@pi-agent/extensions/example/config.data")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(path)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, root, footprint, codec.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := value.CanonicalContext(t.Context(), fixtureLimits)
	if err != nil {
		t.Fatal(err)
	}
	body, err := aggregate.NewManagedContribution(contract, canonical)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, path)
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func fixtureValue(t testing.TB, syntax configdocument.Syntax, text string) configdocument.Value {
	t.Helper()
	source := `{"value":` + text + `}`
	if syntax == configdocument.TOML {
		source = "value=" + text
	}
	return lookup(t, syntax, aggregate.ExistingDocument([]byte(source)), key(t, "value"))
}

func parse(t testing.TB, syntax configdocument.Syntax, document aggregate.Document) configdocument.Document {
	t.Helper()
	parsed, err := configdocument.Parse(t.Context(), document.Content(), syntax, fixtureLimits)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func lookup(t testing.TB, syntax configdocument.Syntax, document aggregate.Document, path output.KeyPath) configdocument.Value {
	t.Helper()
	value, present, err := parse(t, syntax, document).Lookup(path)
	if err != nil || !present {
		t.Fatalf("fixture lookup failed: present=%v, err=%v", present, err)
	}
	return value
}

func selectSets(t testing.TB, sets ...aggregate.ContributionSet) aggregate.Selection {
	t.Helper()
	contracts := make([]aggregate.ProjectionContract, len(sets))
	for index, set := range sets {
		contracts[index] = set.Contract()
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func read(t testing.TB, codec aggregate.Codec, document aggregate.Document, selection aggregate.Selection) aggregate.Snapshot {
	t.Helper()
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	return snapshot
}

func plan(t testing.TB, before aggregate.Snapshot, desired map[output.KeyPath]*aggregate.ContributionSet) aggregate.Plan {
	t.Helper()
	intents := make([]aggregate.ProjectionIntent, 0, len(before.States()))
	for _, state := range before.States() {
		intent, err := aggregate.NewProjectionIntent(state, desired[state.Contract().Address().KeyPath()])
		if err != nil {
			t.Fatal(err)
		}
		intents = append(intents, intent)
	}
	plan, err := aggregate.NewPlan(before, intents)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func assertComments(t testing.TB, syntax configdocument.Syntax, before, after aggregate.Document) {
	t.Helper()
	left, right := parse(t, syntax, before).Comments(), parse(t, syntax, after).Comments()
	slices.Sort(left)
	slices.Sort(right)
	if !slices.Equal(left, right) {
		t.Fatal("native comments were lost or duplicated")
	}
}

func TestNativeCodecRequiresLiveContextAndIndependentLimits(t *testing.T) {
	codec := fixtureCodec(t, configdocument.JSON)
	set := fixtureSet(t, codec, key(t, "a"), fixtureValue(t, configdocument.JSON, `"large value"`))
	selection := selectSets(t, set)
	source := aggregate.ExistingDocument([]byte(`{"a":"large value"}`))
	for _, limits := range []struct{ document, value configdocument.Limits }{
		{configdocument.Limits{MaximumBytes: 4, MaximumDepth: 4}, fixtureLimits},
		{fixtureLimits, configdocument.Limits{MaximumBytes: 10, MaximumDepth: 4}},
	} {
		bounded, err := nativecodec.New(t.Context(), codec.ContractID(), limits.document, limits.value)
		if err != nil {
			t.Fatal(err)
		}
		actual, failure := bounded.Read(source, selection)
		if failure == nil || failure.Reason() != aggregate.CodecFailureResourceLimitExceeded || len(actual.States()) != 0 {
			t.Fatal("raw/value resource refusal returned partial evidence or lost its class")
		}
		if bounded.MaximumDocumentBytes() != limits.document.MaximumBytes {
			t.Fatal("raw document admission borrowed the canonical-value budget")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	canceled, err := nativecodec.New(ctx, codec.ContractID(), fixtureLimits, fixtureLimits)
	if err != nil {
		t.Fatal(err)
	}
	before := read(t, codec, source, selection)
	p := plan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key(t, "a"): &set})
	cancel()
	if _, failure := canceled.Read(source, selection); failure == nil || failure.Reason() != aggregate.CodecFailureOperationCanceled {
		t.Fatal("read ignored cancellation after codec construction")
	}
	if _, failure := canceled.Render(source, p); failure == nil || failure.Reason() != aggregate.CodecFailureOperationCanceled {
		t.Fatal("render ignored cancellation after codec construction")
	}
	if _, failure := canceled.Restore(source, before); failure == nil || failure.Reason() != aggregate.CodecFailureOperationCanceled {
		t.Fatal("recovery ignored cancellation after codec construction")
	}
	var failure *aggregate.CodecFailure
	if err := canceled.ValidateContributions(set); !errors.As(err, &failure) || failure.Reason() != aggregate.CodecFailureOperationCanceled {
		t.Fatal("contribution admission lost its safe cancellation class")
	}
	if _, err := nativecodec.New(ctx, codec.ContractID(), fixtureLimits, fixtureLimits); !errors.Is(err, context.Canceled) {
		t.Fatal("constructor lost ordinary contextual cancellation")
	}
	if _, err := nativecodec.New(nil, codec.ContractID(), fixtureLimits, fixtureLimits); err == nil {
		t.Fatal("nil operation context was admitted")
	}
	if _, err := nativecodec.New(t.Context(), codec.ContractID(), configdocument.Limits{}, fixtureLimits); err == nil {
		t.Fatal("missing raw document bounds were admitted")
	}
	if _, err := nativecodec.New(t.Context(), codec.ContractID(), fixtureLimits, configdocument.Limits{}); err == nil {
		t.Fatal("missing canonical value bounds were admitted")
	}
	if _, err := nativecodec.New(t.Context(), "unknown", fixtureLimits, fixtureLimits); err == nil {
		t.Fatal("unknown native grammar was admitted")
	}
	malformed, failure := codec.Read(aggregate.ExistingDocument([]byte(`{"a":"SECRET_CANARY",`)), selection)
	if failure == nil || len(malformed.States()) != 0 || strings.Contains(failure.Error(), "SECRET_CANARY") {
		t.Fatal("parser failure leaked host data or returned partial evidence")
	}
}
