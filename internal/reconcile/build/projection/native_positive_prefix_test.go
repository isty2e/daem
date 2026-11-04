package projection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
	"github.com/isty2e/daem/test/outputtest"
)

func TestPositiveNativePrefixLeavesRetirementForTheSuffix(t *testing.T) {
	for _, test := range []struct {
		name     string
		before   string
		previous []nativePrefixKey
		desired  []nativePrefixKey
		adopt    bool
		kind     reconcile.AggregateDecisionKind
		prefix   string
		complete string
	}{
		{
			name:     "create and replace before unrelated retirement",
			before:   `{"a":1,"z":1,"foreign":true}`,
			previous: []nativePrefixKey{{"settings", "a", "1"}, {"settings", "z", "1"}},
			desired:  []nativePrefixKey{{"settings", "a", "2"}, {"settings", "b", "3"}},
			kind:     reconcile.AggregateReplace,
			prefix:   `{"a":2,"b":3,"z":1,"foreign":true}`,
			complete: `{"a":2,"b":3,"foreign":true}`,
		},
		{
			name:     "matching adoption does not rewrite",
			before:   "{\n  \"a\": 2, \"z\": 1, \"foreign\": true\n}\n",
			previous: []nativePrefixKey{{"settings", "z", "1"}},
			desired:  []nativePrefixKey{{"settings", "a", "2"}}, adopt: true,
			kind:     reconcile.AggregateRecord,
			prefix:   `{"a":2,"z":1,"foreign":true}`,
			complete: `{"a":2,"foreign":true}`,
		},
		{
			name:     "current selected key is not a fake write",
			before:   "{ \"a\": 2, \"z\": 1, \"foreign\": true }\n",
			previous: []nativePrefixKey{{"settings", "a", "2"}, {"settings", "z", "1"}},
			desired:  []nativePrefixKey{{"settings", "a", "2"}},
			kind:     reconcile.AggregateNoOp,
			prefix:   `{"a":2,"z":1,"foreign":true}`,
			complete: `{"a":2,"foreign":true}`,
		},
		{
			name:     "subject replacement within a selected footprint",
			before:   `{"a":1,"z":1,"foreign":true}`,
			previous: []nativePrefixKey{{"old", "a", "1"}, {"old", "z", "1"}},
			desired:  []nativePrefixKey{{"new", "a", "2"}},
			kind:     reconcile.AggregateReplace,
			prefix:   `{"a":2,"z":1,"foreign":true}`,
			complete: `{"a":2,"foreign":true}`,
		},
		{
			name:    "missing document",
			desired: []nativePrefixKey{{"settings", "a", "2"}},
			kind:    reconcile.AggregateCreate, prefix: `{"a":2}`, complete: `{"a":2}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := nativePrefixInput(t, test.before, test.previous, test.desired)
			input.ManageUnmanagedMatches = test.adopt
			decisions, err := BuildAggregateDecisions(input)
			if err != nil {
				t.Fatal(err)
			}
			source := onlyAggregateDecision(t, decisions)
			if source.IsBlocked() {
				t.Fatalf("fixture blocked: %s: %s", source.Reason(), source.Detail())
			}
			assertNativePrefixJSON(t, source.Rendered().Document(), test.complete)

			prefixes, err := BuildPositiveNativePrefix(t.Context(), decisions)
			if err != nil {
				t.Fatal(err)
			}
			prefix := onlyAggregateDecision(t, prefixes)
			if prefix.Kind() != test.kind {
				t.Fatalf("prefix kind = %s, want %s", prefix.Kind(), test.kind)
			}
			assertNativePrefixJSON(t, prefix.Rendered().Document(), test.prefix)
			if (test.kind == reconcile.AggregateNoOp || test.kind == reconcile.AggregateRecord) &&
				(!prefix.Rendered().Document().Equal(source.BeforeDocument()) || prefix.MutatesHost()) {
				t.Fatal("state-only prefix changed bytes or requested a host write")
			}
			if prefix.MutatesState() != (test.kind != reconcile.AggregateNoOp) {
				t.Fatal("prefix lost its classified state transition")
			}
			for _, projection := range prefix.Projections() {
				if projection.Contract().Address().KeyPath().String() == "/z" {
					t.Fatal("removal-only key escaped into prefix")
				}
				for _, original := range source.Projections() {
					if original.Contract().Equal(projection.Contract()) &&
						!reflect.DeepEqual(original.SubjectDecisions(), projection.SubjectDecisions()) {
						t.Fatal("prefix reclassified a subject or lost prior ownership")
					}
				}
			}
			operation, ok := prefix.NativeOperationContract()
			selection, err := prefix.CodecPlan().Before().Selection()
			if err != nil || !ok || operation.ValidateSelection(selection) != nil {
				t.Fatal("prefix operation does not cover its own selection")
			}
			assertNativePrefixJSON(t, source.Rendered().Document(), test.complete)
		})
	}
}

func TestPositiveNativePrefixRetainsExplicitReplacementAdmission(t *testing.T) {
	input := nativePrefixInput(t, `{"foreign":true}`, []nativePrefixKey{{"settings", "a", "1"}}, []nativePrefixKey{{"settings", "a", "2"}})
	id, _ := entity.New(entity.KindHostConfig, "settings")
	key, _ := output.NewKeyPath("a")
	selection, err := topologyprojection.NewHostConfigKeySelection(id, []output.KeyPath{key})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := BuildNativeReplacementDecisions(input, selection)
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := BuildPositiveNativePrefix(t.Context(), decisions)
	if err != nil {
		t.Fatal(err)
	}
	assertNativePrefixJSON(t, onlyAggregateDecision(t, prefix).Rendered().Document(), `{"a":2,"foreign":true}`)
}

func TestPositiveNativePrefixDoesNotTurnBlockedOrUnselectedWorkIntoEffects(t *testing.T) {
	input := nativePrefixInput(t, `{"a":9}`, []nativePrefixKey{{"settings", "a", "1"}}, []nativePrefixKey{{"settings", "a", "2"}})
	blocked, err := BuildAggregateDecisions(input)
	if err != nil || !onlyAggregateDecision(t, blocked).IsBlocked() {
		t.Fatal("fixture did not establish drift", err)
	}
	if prefix, err := BuildPositiveNativePrefix(t.Context(), blocked); err == nil || len(prefix) != 0 {
		t.Fatal("blocked reconciliation yielded a usable prefix")
	}

	input = nativePrefixInput(t, `{"z":1}`, []nativePrefixKey{{"settings", "z", "1"}}, nil)
	retirement, err := BuildAggregateDecisions(input)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryInput := mixedDocumentCodecsInput(t)
	ordinary, err := BuildAggregateDecisions(ordinaryInput)
	if err != nil {
		t.Fatal(err)
	}
	hook := aggregateHookContract(t, "guard", "echo guard")
	items := aggregateItems(t, hook)
	createdHook, err := buildAggregateDecisionsForTest(AggregateInput{
		Locked: aggregateLockedSection(t, hook), Expected: []lock.LockedSubjectContract{hook}, Desired: items,
		Evidence:        []observe.AggregateEvidence{aggregateEvidence(t, items[0].Contribution().Contract(), aggregate.AbsentDocument())},
		SelectedTargets: planSelectedTargets(t, target.TargetCodex),
	})
	if err != nil || !onlyAggregateDecision(t, createdHook).MutatesHost() {
		t.Fatal("fixture did not create an ordinary aggregate", err)
	}
	for _, decisions := range [][]reconcile.AggregateDecision{nil, retirement, ordinary, createdHook} {
		prefix, err := BuildPositiveNativePrefix(t.Context(), decisions)
		if err != nil || len(prefix) != 0 {
			t.Fatal("empty selection or ordinary work produced prefix effects", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if prefix, err := BuildPositiveNativePrefix(ctx, retirement); !errors.Is(err, context.Canceled) || len(prefix) != 0 {
		t.Fatal("cancelled projection returned usable work", err)
	}
	if prefix, err := BuildPositiveNativePrefix(nil, nil); err == nil || len(prefix) != 0 {
		t.Fatal("missing context returned usable work")
	}
}

type nativePrefixKey struct{ owner, name, value string }

func nativePrefixInput(t *testing.T, current string, previous, desired []nativePrefixKey) AggregateInput {
	t.Helper()
	codecID, _ := aggregate.HostConfigCodecForSyntax("json")
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{codecID}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := catalog.Lookup(codecID)
	input := AggregateInput{Codecs: catalog, SelectedTargets: planSelectedTargets(t, target.TargetPi)}
	var locked []lock.LockedSubjectContract
	contracts := make(map[string]aggregate.ProjectionContract)
	for group, keys := range [][]nativePrefixKey{previous, desired} {
		for _, key := range keys {
			path := nativePrefixKeyPath(t, key.name)
			footprint, _ := output.NewKeyFootprint(path)
			contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, outputtest.Parse(t, ".pi/settings.json"), footprint, codecID)
			if err != nil {
				t.Fatal(err)
			}
			contracts[key.name] = contract
			selection, _ := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
			snapshot, failure := codec.Read(aggregate.ExistingDocument(nativePrefixSource(t, limits, []nativePrefixKey{key})), selection)
			if failure != nil {
				t.Fatal(failure)
			}
			body, err := aggregate.NewManagedContribution(contract, snapshot.States()[0].CanonicalProjection())
			if err != nil {
				t.Fatal(err)
			}
			id, _ := entity.New(entity.KindHostConfig, key.owner)
			subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeProject, path)
			if err != nil {
				t.Fatal(err)
			}
			item, err := aggregate.NewSubjectContribution(subject, body)
			if err != nil {
				t.Fatal(err)
			}
			if group == 0 {
				input.States = append(input.States, aggregateStates(t, item)...)
				continue
			}
			placement, err := profile.HostConfigPlacementForContract(contract)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := lock.NewHostConfigKeySubjectContract(id, subject, body, placement)
			if err != nil {
				t.Fatal(err)
			}
			locked = append(locked, entry)
			input.Desired = append(input.Desired, item)
		}
	}
	input.Expected = append([]lock.LockedSubjectContract(nil), locked...)
	sources := make(map[string][]nativePrefixKey)
	for _, key := range desired {
		sources[key.owner] = append(sources[key.owner], key)
	}
	for owner, values := range sources {
		content := nativePrefixSource(t, limits, values)
		placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
			Target: target.TargetPi, Scope: target.ScopeProject, Path: "settings.json",
			Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxJSON,
		})
		if err != nil {
			t.Fatal(err)
		}
		id, _ := entity.New(entity.KindHostConfig, owner)
		subject, _ := resourcetopology.Subject(id)
		identity, err := artifact.NewExactIdentity(artifact.SourceID("local:"+owner+".json"), "", artifact.ArtifactKindFile, artifact.HashFileContent(content))
		if err != nil {
			t.Fatal(err)
		}
		use, _ := lock.NewExactFileUse(target.ScopeProject, false)
		derivation, err := lock.NewDirectResolutionDerivation(identity)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
			EntityID: id, SubjectID: subject, ExactSupply: identity, ExactFileUse: &use,
			HostConfigPlacement: &placement, Derivation: derivation,
		})
		if err != nil {
			t.Fatal(err)
		}
		locked = append(locked, entry)
	}
	input.Locked = aggregateLockedSection(t, locked...)
	var selected []aggregate.ProjectionContract
	for _, contract := range contracts {
		selected = append(selected, contract)
	}
	selection, err := aggregate.NewSelection(selected)
	if err != nil {
		t.Fatal(err)
	}
	document := aggregate.AbsentDocument()
	if current != "" {
		document = aggregate.ExistingDocument([]byte(current))
	}
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(document, snapshot, fileModeForAggregateDocument(document))
	if err != nil {
		t.Fatal(err)
	}
	input.Evidence = []observe.AggregateEvidence{evidence}
	return input
}

func nativePrefixKeyPath(t *testing.T, name string) output.KeyPath {
	t.Helper()
	var key output.KeyPath
	var err error
	if strings.HasPrefix(name, "/") {
		key, err = output.ParseKeyPath(name)
	} else {
		key, err = output.NewKeyPath(name)
	}
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func nativePrefixSource(t *testing.T, limits configdocument.Limits, keys []nativePrefixKey) []byte {
	t.Helper()
	document, err := configdocument.Absent(configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	var changes []configdocument.Change
	for _, key := range keys {
		valueDocument, err := configdocument.Parse(t.Context(), []byte(key.value), configdocument.JSON, limits)
		if err != nil {
			t.Fatal(err)
		}
		value, _, err := valueDocument.Lookup(output.KeyPath{})
		if err != nil {
			t.Fatal(err)
		}
		change, err := configdocument.Set(nativePrefixKeyPath(t, key.name), value)
		if err != nil {
			t.Fatal(err)
		}
		changes = append(changes, change)
	}
	document, err = configdocument.Edit(t.Context(), document, changes)
	if err != nil {
		t.Fatal(err)
	}
	return document.Bytes()
}

func assertNativePrefixJSON(t *testing.T, document aggregate.Document, expected string) {
	t.Helper()
	var got, want map[string]any
	gotDecoder := json.NewDecoder(bytes.NewReader(document.Content()))
	gotDecoder.UseNumber()
	if err := gotDecoder.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if err := gotDecoder.Decode(new(json.RawMessage)); err != io.EOF {
		t.Fatal("document has trailing JSON", err)
	}
	wantDecoder := json.NewDecoder(strings.NewReader(expected))
	wantDecoder.UseNumber()
	if err := wantDecoder.Decode(&want); err != nil {
		t.Fatal(err)
	}
	if err := wantDecoder.Decode(new(json.RawMessage)); err != io.EOF {
		t.Fatal("expected document has trailing JSON", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("document = %s, want %s", document.Content(), expected)
	}
}
