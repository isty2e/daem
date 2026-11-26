package document

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestRecordedImageIdentityPreservesWholeBaselineChecks(t *testing.T) {
	row := nativeWholeFileJournalBaseline(t, target.TargetPi, target.ScopeProject)
	state, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{row}})
	if err != nil {
		t.Fatal(err)
	}
	index, err := IndexSnapshotsForClaims(t.Context(), state, state, NativeClaims)
	if err != nil {
		t.Fatal(err)
	}
	groups := []StateGroup{{afterAliases: []aggregate.DocumentAddress{stateGroupWholeAlias(t, row)}}}
	mode := recovery.PermissionMode(row.FileMode().Perm())
	identity, err := NewImageIdentity(true, row.ContentHash(), &mode)
	if err != nil {
		t.Fatal(err)
	}
	mode = 0o644
	if err := index.ValidateExpectedBaselines(t.Context(), groups, aggregate.CodecCatalog{}, []ImageIdentity{identity}, RecordedStateMode); err != nil {
		t.Fatalf("recorded baseline borrowed mutable mode input: %v", err)
	}
	wrongDigest, err := NewImageIdentity(true, artifact.HashFileContent([]byte("other")), recovery.NewPermissionMode(row.FileMode()))
	if err != nil {
		t.Fatal(err)
	}
	hostModeImage, err := NewImageIdentity(true, row.ContentHash(), &mode)
	if err != nil {
		t.Fatal(err)
	}
	absent, err := NewImageIdentity(false, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]ImageIdentity{nil, {{}}, {wrongDigest}, {hostModeImage}, {absent}} {
		if err := index.ValidateExpectedBaselines(t.Context(), groups, aggregate.CodecCatalog{}, invalid, RecordedStateMode); err == nil {
			t.Fatal("whole baseline accepted missing, zero, absent, changed-content or changed-mode evidence")
		}
	}
	contents := aggregate.ExistingDocument([]byte("host bytes"))
	groups[0].comparison, err = NewWholeComparison(t.Context(), ComparisonInput{
		Before: contents, Expected: contents, BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := index.ValidateExpectedBaselines(t.Context(), groups, aggregate.CodecCatalog{}, []ImageIdentity{hostModeImage}, PreserveExistingMode); err != nil {
		t.Fatalf("a recorded update must use the original host mode: %v", err)
	}
	if err := index.ValidateExpectedBaselines(t.Context(), groups, aggregate.CodecCatalog{}, []ImageIdentity{identity}, PreserveExistingMode); err == nil {
		t.Fatal("a recorded update silently reset the host mode to the creation mode")
	}

	missing := aggregate.AbsentDocument()
	comparison, err := NewWholeComparison(t.Context(), ComparisonInput{
		Before: missing, Expected: missing, Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	groups[0].comparison = comparison
	if err := index.ValidateExpectedBaselines(t.Context(), groups, aggregate.CodecCatalog{}, []ImageIdentity{absent}, RecordedStateMode); err != nil {
		t.Fatal("an unchanged whole row lost its observed absence", err)
	}
	changed, err := durable.NewManagedPathState(row.Subject(), row.ConsumerTargets(), row.Scope(), row.Destination(),
		artifact.HashFileContent([]byte("another baseline")), row.ContentKind(), row.PermissionPolicy(), row.FileMode())
	if err != nil {
		t.Fatal(err)
	}
	beforeChange, err := state.WithManagedPaths([]durable.ManagedPathState{changed})
	if err != nil {
		t.Fatal(err)
	}
	for _, before := range []durable.Snapshot{durable.EmptySnapshot(), beforeChange} {
		index, err := IndexSnapshotsForClaims(t.Context(), before, state, NativeClaims)
		if err != nil {
			t.Fatal(err)
		}
		if err := index.ValidateExpectedBaselines(t.Context(), groups, aggregate.CodecCatalog{}, []ImageIdentity{absent}, RecordedStateMode); err == nil {
			t.Fatal("absence supplied a new or changed whole baseline")
		}
	}
}

func TestCanonicalRecoveryComparisonCannotReplaceItsValidatedWitnesses(t *testing.T) {
	fixture := documentClaimsForTest(t)
	row := membershipNativeRow(t, fixture.alias, "native-json-v1", "model")
	state := selectionState(t, row)
	input := stateGroupInput(t, fixture, []durable.ManagedAggregateState{row}, `{"model":1}`, `{"model":1}`)
	groups, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, []StateGroupInput{input})
	if err != nil {
		t.Fatal(err)
	}
	original := groups[0]
	canonical, err := original.CanonicalRecovery(t.Context(), input.comparison)
	if err != nil {
		t.Fatal(err)
	}
	aliases := canonical.Aliases()
	aliases[0] = aggregate.DocumentAddress{}
	bindings := canonical.BindingIntents()
	bindings[0] = ownership.DocumentBindingIntent{}
	if original.BindingIntents()[0].Validate() != nil {
		t.Fatal("group exposed its mutable binding slice")
	}
	if original.Aliases()[0] != fixture.alias {
		t.Fatal("group exposed its mutable alias slice")
	}
	changed, err := NewWholeComparison(t.Context(), ComparisonInput{
		Before: aggregate.ExistingDocument([]byte(`{"model":2}`)), Expected: input.comparison.ExpectedDocument(),
		BeforeMode: recovery.NewPermissionMode(0o600), ExpectedMode: recovery.NewPermissionMode(0o600),
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []Comparison{{}, changed} {
		if _, err := original.CanonicalRecovery(t.Context(), invalid); err == nil {
			t.Fatal("different or zero recovery witnesses replaced the validated group")
		}
	}
	if _, err := (StateGroup{}).CanonicalRecovery(t.Context(), input.comparison); err == nil {
		t.Fatal("zero group gained validated recovery evidence")
	}
}
