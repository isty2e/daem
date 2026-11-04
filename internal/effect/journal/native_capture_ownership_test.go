package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeCaptureOwnershipFeedsWholeCaptureFromOneRegistryRead(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, action := range []string{"create", "replace", "record", "remove"} {
			t.Run(string(scope)+"/"+action, func(t *testing.T) {
				fixture := wholeGroupFixture(t, target.TargetPi, scope, action, []byte("before"), []byte("after"))
				input, reader := captureOwnershipFixture(t, fixture)
				initial := reader.value
				prepared, err := input.prepare(t.Context())
				if err != nil || len(prepared) != 1 || reader.loads != 1 {
					t.Fatalf("prepare capture ownership: groups=%d loads=%d error=%v", len(prepared), reader.loads, err)
				}
				if !reader.value.Equal(initial) || len(prepared[0].bindings) != 1 {
					t.Fatal("preparation published registry state or omitted project/global binding work")
				}
				if scope == target.ScopeProject && (prepared[0].claims != nil || len(prepared[0].provisional) != 0) {
					t.Fatal("project-only capture invented exclusive claims")
				}
				fixture.input.nativeDocumentCaptureInput = prepared[0]
				group, comparison, err := prepareWholeDocumentGroup(t.Context(), fixture.namespace, fixture.input)
				if err != nil {
					t.Fatal(err)
				}
				fixture.stageAndReconstruct(t, group, comparison)
				fixture.requireUnchangedHost(t)
			})
		}
	}
}

func TestNativeCaptureOwnershipReshapesCompleteGlobalClaims(t *testing.T) {
	for _, direction := range []string{"whole-to-keys", "keys-to-whole"} {
		t.Run(direction, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "record", []byte("before"), nil)
			input, reader := captureOwnershipFixture(t, fixture)
			alias := fixture.namespace.Addresses()[0]
			first := membershipNativeRow(t, alias, "native-json-v1", "selected")
			unchanged := membershipNativeRow(t, alias, "native-json-v1", "unchanged")
			input.after = selectionState(t, first, unchanged)
			beforeClaims := reader.value.Claims()
			wantAfter := []output.Footprint{first.Contribution().Contract().Address().Footprint(), unchanged.Contribution().Contract().Address().Footprint()}
			if direction == "keys-to-whole" {
				input.before, input.after = input.after, input.before
				beforeClaims = nil
				exact, known := fixture.namespace.observation.Exact()
				if !known {
					t.Fatal("existing whole document has no exact namespace")
				}
				for _, footprint := range wantAfter {
					beforeClaims = append(beforeClaims, documentActiveClaim(t, documentClaimFixture{owner: input.owner, exact: exact}, footprint))
				}
				wantAfter = []output.Footprint{output.WholePathFootprint()}
				var err error
				reader.value, err = ownership.NewRegistry(beforeClaims, reader.value.DocumentBindings()...)
				if err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := input.prepare(t.Context())
			if err != nil || len(prepared) != 1 || prepared[0].claims == nil {
				t.Fatalf("reshape: %v", err)
			}
			transition := prepared[0].claims
			if !slices.EqualFunc(transition.Before(), beforeClaims, ownership.Claim.Equal) || len(transition.After()) != len(wantAfter) {
				t.Fatal("reshape lost the complete before or after fragment")
			}
			for _, claim := range transition.After() {
				if !slices.Contains(wantAfter, claim.Address().Footprint()) {
					t.Fatal("after claims contain an unselected membership extent")
				}
			}
			intent := prepared[0].bindings[0]
			after, known := intent.KnownAfter()
			if !known || !after.Equal(intent.Before()) {
				t.Fatal("whole/keyed mode transition replaced or retired the binding")
			}
		})
	}
}

func TestNativeCaptureOwnershipRetainsStoredProvenanceAndUnchangedSibling(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "record", []byte("before"), nil)
	input, reader := captureOwnershipFixture(t, fixture)
	alias := fixture.namespace.Addresses()[0]
	selected := membershipNativeRow(t, alias, "native-json-v1", "selected")
	unchanged := membershipNativeRow(t, alias, "native-json-v1", "unchanged")
	input.before, input.after = selectionState(t, selected, unchanged), selectionState(t, unchanged)
	stored := reader.value.DocumentBindings()[0]
	var err error
	input.owner, err = stateauthority.New(input.owner.StatefileAuthority(), filepath.Join(t.TempDir(), "diagnostic-alias.toml"))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := input.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	intent := prepared[0].bindings[0]
	after, known := intent.KnownAfter()
	retained, present := after.Get()
	if !known || !present || !retained.Equal(stored) || !intent.Owner().ExactEqual(stored.Owner()) {
		t.Fatal("unchanged sibling lost its exact historical binding/provenance")
	}

	input.after = durable.EmptySnapshot()
	prepared, err = input.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	after, known = prepared[0].bindings[0].KnownAfter()
	if _, present := after.Get(); !known || present {
		t.Fatal("last native member did not retire its binding")
	}
}

func TestNativeCaptureOwnershipRejectsMissingOrStaleBeforeFacts(t *testing.T) {
	for _, fault := range []string{"reader", "read failure", "zero registry", "missing claim", "reserved claim", "foreign claim", "extra owned claim", "missing binding", "stale binding", "new membership with old binding"} {
		t.Run(fault, func(t *testing.T) {
			fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "record", []byte("before"), nil)
			input, reader := captureOwnershipFixture(t, fixture)
			claims, bindings := reader.value.Claims(), reader.value.DocumentBindings()
			var err error
			switch fault {
			case "reader":
				input.registry = nil
			case "read failure":
				reader.err = errors.New("registry read failed")
			case "zero registry":
				reader.value = ownership.Registry{}
			case "missing claim":
				claims = nil
			case "reserved claim":
				claims[0], err = ownership.NewReservedClaim(claims[0].Address(), input.owner, "interrupted")
			case "foreign claim":
				foreign := documentClaimsForTest(t).owner
				claims[0], err = ownership.NewActiveClaim(claims[0].Address(), foreign)
			case "extra owned claim":
				alias := fixture.namespace.Addresses()[0]
				row := membershipNativeRow(t, alias, "native-json-v1", "selected")
				input.before, input.after = selectionState(t, row), selectionState(t, row)
				exact, _ := fixture.namespace.observation.Exact()
				claims = []ownership.Claim{
					documentActiveClaim(t, documentClaimFixture{owner: input.owner, exact: exact}, documentKeyFootprint(t, "selected")),
					documentActiveClaim(t, documentClaimFixture{owner: input.owner, exact: exact}, documentKeyFootprint(t, "not-in-snapshot")),
				}
			case "missing binding":
				bindings = nil
			case "stale binding":
				bindings[0], err = ownership.NewDocumentBinding(input.owner, bindings[0].Destination(), pathtest.Exact(filepath.Join(t.TempDir(), "other.json")))
			case "new membership with old binding":
				input.before = durable.EmptySnapshot()
				claims = nil
			}
			if err != nil {
				t.Fatal(err)
			}
			if fault != "zero registry" {
				reader.value, err = ownership.NewRegistry(claims, bindings...)
				if err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := input.prepare(t.Context())
			if err == nil || prepared != nil {
				t.Fatalf("invalid before facts produced usable captures: %v", err)
			}
			if reader.err != nil && !errors.Is(err, reader.err) {
				t.Fatalf("registry failure was hidden: %v", err)
			}
			fixture.requireUnchangedHost(t)
		})
	}
}

func TestNativeCaptureOwnershipProvisionalAcquiresNeverMintExactClaims(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "create", nil, []byte("after"))
	pending, provisional := fixture.namespace.observation.Provisional()
	if !provisional {
		t.Skip("filesystem supplied exact authority for the absent destination")
	}
	input, reader := captureOwnershipFixture(t, fixture)
	prepared, err := input.prepare(t.Context())
	if err != nil || len(prepared) != 1 || prepared[0].claims != nil || len(prepared[0].provisional) != 1 {
		t.Fatalf("provisional capture: %v", err)
	}
	if !prepared[0].provisional[0].intent.Path().Equal(pending) || len(reader.value.Claims()) != 0 {
		t.Fatal("provisional capture minted exact ownership")
	}
	if _, known := prepared[0].bindings[0].KnownAfter(); known {
		t.Fatal("provisional binding became exact without promotion")
	}
	input.before = input.after
	if values, err := input.prepare(t.Context()); err == nil || values != nil {
		t.Fatal("provisional acquisition reused before membership")
	}
	fixture.requireUnchangedHost(t)
}

func TestNativeCaptureOwnershipDiscardsEarlierGroupsOnFailureAndCancellation(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeProject, "record", []byte("before"), nil)
	input, reader := captureOwnershipFixture(t, fixture)
	other := wholeGroupFixture(t, target.TargetCodex, target.ScopeProject, "record", []byte("other"), nil)
	input.namespaces = append(input.namespaces, other.namespace)
	var err error
	input.before, err = input.before.WithManagedPaths(append(input.before.ManagedPaths(), other.envelope.before.ManagedPaths()...))
	if err != nil {
		t.Fatal(err)
	}
	input.after = input.before
	if values, err := input.prepare(t.Context()); err == nil || values != nil || reader.loads != 1 {
		t.Fatalf("later missing binding leaked an earlier group: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	reader.afterLoad = cancel
	if values, err := input.prepare(ctx); !errors.Is(err, context.Canceled) || values != nil {
		t.Fatalf("canceled read leaked a capture: %v", err)
	}
	fixture.requireUnchangedHost(t)
	other.requireUnchangedHost(t)
}

func captureOwnershipFixture(t *testing.T, fixture wholeGroupTestFixture) (nativeCaptureOwnershipInput, *nativeCaptureRegistryReader) {
	t.Helper()
	var claims []ownership.Claim
	if fixture.input.claims != nil {
		claims = fixture.input.claims.Before()
	}
	var bindings []ownership.DocumentBinding
	for _, intent := range fixture.input.bindings {
		if binding, present := intent.Before().Get(); present {
			bindings = append(bindings, binding)
		}
	}
	current, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	reader := &nativeCaptureRegistryReader{value: current}
	return nativeCaptureOwnershipInput{
		owner: fixture.input.owner, operationID: fixture.input.operationID,
		before: fixture.envelope.before, after: fixture.envelope.after,
		namespaces: []NativeDocumentNamespace{fixture.namespace}, registry: reader,
		limits: fixture.input.limits, budget: fixture.input.budget,
	}, reader
}

type nativeCaptureRegistryReader struct {
	ownershipmutation.RegistryReader
	value     ownership.Registry
	err       error
	loads     int
	afterLoad func()
}

func (reader *nativeCaptureRegistryReader) Load(context.Context) (ownership.Registry, error) {
	reader.loads++
	if reader.afterLoad != nil {
		reader.afterLoad()
	}
	return reader.value, reader.err
}

func (reader *nativeCaptureRegistryReader) LoadForClaimRemovals(ctx context.Context, expected []ownership.Claim, maximumPhysicalDepth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	if len(expected) != 0 || maximumPhysicalDepth != physicalwork.MaximumPhysicalPathDepth || budget == nil {
		return ownership.Registry{}, fmt.Errorf("capture read must use its bounded strict selection")
	}
	return reader.Load(ctx)
}

func TestNativeCaptureOwnershipFeedsSelectedCapture(t *testing.T) {
	fixture := nativeJournalFixture(t)
	selected := fixture.after.ManagedAggregates()[0]
	original := aggregate.ExistingDocument([]byte(`{"model":true,"foreign":99}`))
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	capture := selectedCaptureFixture(t, fixture, original, edits, nil, NativeDocumentRoots{})
	var claims []ownership.Claim
	if capture.input.claims != nil {
		claims = capture.input.claims.Before()
	}
	var bindings []ownership.DocumentBinding
	for _, intent := range capture.input.bindings {
		if binding, present := intent.Before().Get(); present {
			bindings = append(bindings, binding)
		}
	}
	current, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	reader := &nativeCaptureRegistryReader{value: current}
	input := nativeCaptureOwnershipInput{
		owner: capture.input.owner, operationID: capture.input.operationID,
		before: fixture.before, after: fixture.after, registry: reader,
		namespaces: []NativeDocumentNamespace{capture.namespace}, limits: capture.input.limits, budget: capture.input.budget,
	}
	prepared, err := input.prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	capture.input.nativeDocumentCaptureInput = prepared[0]
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	capture.requireCompleteStage(t, group, comparison)
}
