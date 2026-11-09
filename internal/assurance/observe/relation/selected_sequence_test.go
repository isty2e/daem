package relation_test

import (
	"slices"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestSelectedSequenceCompactsMembershipBeforeOrderButRetainsOriginalCAS(t *testing.T) {
	capability := selectedCapability(t, target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin)
	alpha := mustObservedOrderMember(t, "alpha", "alpha")
	beta := mustObservedOrderMember(t, "beta", "beta")
	retired := mustObservedOrderMember(t, "retired", "retired")
	foreign, err := observerelation.NewObservedRelationRow(mustObservedLoadIdentity(t, "foreign"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []observerelation.ObservedRelationRow{mustCorrelatedObservedRow(t, beta), foreign, mustCorrelatedObservedRow(t, retired), mustCorrelatedObservedRow(t, alpha)}
	original := selectedOriginal(t, capability, capability.NativeSequences()[0].SequenceID(), rows)
	constraint := selectedConstraint(t, capability, alpha, beta)
	removed := []int{2}
	selected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, removed)
	if err != nil {
		t.Fatal(err)
	}
	if !selected.Original().SameOrdering(original) || selected.Original().Revision() != original.Revision() || selected.Original().Authority() != original.Authority() {
		t.Fatal("surviving planning rows replaced the original disk observation")
	}
	if selected.Projection().Destination().String() != ".opencode/opencode.json" || selected.Projection().ArrayPath().String() != "/plugin" || selected.MemberIdentityContract() != capability.MemberIdentityContract() {
		t.Fatal("selected facts lost their explicit native projection or identity contract")
	}
	if !slices.Equal(selected.SurvivorOrder(), []int{2, 1, 0}) || !slices.Equal(selected.SurvivingRows(), []observerelation.ObservedRelationRow{rows[0], rows[1], rows[3]}) ||
		!slices.Equal(selected.ExpectedRows(), []observerelation.ObservedRelationRow{rows[3], rows[1], rows[0]}) || len(selected.PrecedenceChanges()) != 2 || !selected.Changed() {
		t.Fatal("compaction or fixed/foreign-slot preparation changed the selected meaning")
	}
	if rejected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, nil); err == nil || rejected.Projection().Validate() == nil {
		t.Fatal("a correlated retired row outside the desired constraint was silently dropped")
	}
	removed[0] = 0
	selected.RemovedSlots()[0], selected.SurvivorOrder()[0] = 0, 0
	selected.SurvivingRows()[0], selected.ExpectedRows()[0] = observerelation.ObservedRelationRow{}, observerelation.ObservedRelationRow{}
	selected.PrecedenceChanges()[0] = observerelation.PrecedenceChange{}
	if !slices.Equal(selected.RemovedSlots(), []int{2}) || !slices.Equal(selected.SurvivorOrder(), []int{2, 1, 0}) || selected.ExpectedRows()[0] != rows[3] || selected.PrecedenceChanges()[0].ManagedSubject().IsZero() {
		t.Fatal("caller mutation changed immutable selected-sequence facts")
	}
}

func TestSelectedSequenceDoesNotConfuseClassCompletionWithPhysicalEvidence(t *testing.T) {
	alpha := mustObservedOrderMember(t, "alpha", "alpha")
	beta := mustObservedOrderMember(t, "beta", "beta")
	for _, test := range []struct {
		target  target.Target
		carrier desiredextension.Carrier
	}{
		{target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin},
		{target.TargetPi, desiredextension.CarrierPiPackage},
	} {
		t.Run(string(test.target), func(t *testing.T) {
			capability := selectedCapability(t, test.target, test.carrier)
			constraint := selectedConstraint(t, capability, alpha, beta)
			for _, rows := range [][]observerelation.ObservedRelationRow{nil, {mustCorrelatedObservedRow(t, alpha)}} {
				original := selectedOriginal(t, capability, capability.NativeSequences()[0].SequenceID(), rows)
				selected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, nil)
				if err != nil || selected.Changed() || !slices.Equal(selected.ExpectedRows(), rows) || selected.Original().Revision() != original.Revision() {
					t.Fatal("a physical subset imposed or invented class-wide membership", err)
				}
			}
		})
	}
	capability := selectedCapability(t, target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin)
	constraint := selectedConstraint(t, capability, alpha, beta)
	projections := capability.NativeSequences()
	server := selectedOriginal(t, capability, projections[0].SequenceID(), []observerelation.ObservedRelationRow{mustCorrelatedObservedRow(t, alpha)})
	tui := selectedOriginal(t, capability, projections[2].SequenceID(), []observerelation.ObservedRelationRow{mustCorrelatedObservedRow(t, beta)})
	left, err := observerelation.NewSelectedSequenceEdit(capability, server, constraint, nil)
	if err != nil {
		t.Fatal(err)
	}
	right, err := observerelation.NewSelectedSequenceEdit(capability, tui, constraint, nil)
	if err != nil || left.Original().ClassID() != right.Original().ClassID() || left.Projection().Destination() == right.Projection().Destination() {
		t.Fatal("independent documents of one class lost their native selections", err)
	}
}

func TestSelectedSequenceRejectsMismatchedClassAndMalformedOriginalSlots(t *testing.T) {
	capability := selectedCapability(t, target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin)
	alpha := mustObservedOrderMember(t, "alpha", "alpha")
	beta := mustObservedOrderMember(t, "beta", "beta")
	constraint := selectedConstraint(t, capability, alpha, beta)
	original := selectedOriginal(t, capability, capability.NativeSequences()[0].SequenceID(), []observerelation.ObservedRelationRow{mustCorrelatedObservedRow(t, beta), mustCorrelatedObservedRow(t, alpha)})
	for _, removed := range [][]int{{-1}, {2}, {0, 0}, {1, 0}} {
		if rejected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, removed); err == nil || rejected.Projection().Validate() == nil {
			t.Fatal("invalid original slots returned selected edit facts", removed, err)
		}
	}
	other := selectedCapability(t, target.TargetPi, desiredextension.CarrierPiPackage)
	if rejected, err := observerelation.NewSelectedSequenceEdit(other, original, constraint, nil); err == nil || rejected.Projection().Validate() == nil {
		t.Fatal("another profile class adopted the observed sequence")
	}
	unknown, err := hostrelation.NewPhysicalSequenceID("opencode:project:unknown.plugins")
	if err != nil {
		t.Fatal(err)
	}
	unselected := selectedOriginal(t, capability, unknown, original.OrderedRows())
	if rejected, err := observerelation.NewSelectedSequenceEdit(capability, unselected, constraint, nil); err == nil || rejected.Projection().Validate() == nil {
		t.Fatal("unknown physical sequence inferred a native projection")
	}
	retired := selectedOriginal(t, capability, original.SequenceID(), []observerelation.ObservedRelationRow{
		mustCorrelatedObservedRow(t, mustObservedOrderMember(t, "retired-a", "retired-a")),
		mustCorrelatedObservedRow(t, mustObservedOrderMember(t, "retired-b", "retired-b")),
	})
	all, err := observerelation.NewSelectedSequenceEdit(capability, retired, constraint, []int{0, 1})
	if err != nil || !all.Changed() || len(all.SurvivorOrder()) != 0 || len(all.ExpectedRows()) != 0 || all.Original().Revision() != retired.Revision() {
		t.Fatal("removal-only preparation reset CAS or invented an added row", err)
	}
}

func TestSelectedSequenceRemovalAdmitsOnlyRetiredCorrelatedSubjects(t *testing.T) {
	capability := selectedCapability(t, target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin)
	alpha := mustObservedOrderMember(t, "alpha", "alpha")
	beta := mustObservedOrderMember(t, "beta", "beta")
	retired := mustObservedOrderMember(t, "retired", "retired")
	foreign, err := observerelation.NewObservedRelationRow(mustObservedLoadIdentity(t, "foreign"))
	if err != nil {
		t.Fatal(err)
	}
	constraint := selectedConstraint(t, capability, alpha, beta)
	current := []observerelation.ObservedRelationRow{mustCorrelatedObservedRow(t, beta), foreign, mustCorrelatedObservedRow(t, alpha)}
	mixed := []observerelation.ObservedRelationRow{current[0], foreign, mustCorrelatedObservedRow(t, retired), current[2]}
	for _, test := range []struct {
		name    string
		rows    []observerelation.ObservedRelationRow
		removed []int
	}{
		{"foreign", current, []int{1}},
		{"desired", current, []int{0}},
		{"mixed foreign", mixed, []int{1, 2}},
		{"mixed desired", mixed, []int{2, 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := selectedOriginal(t, capability, capability.NativeSequences()[0].SequenceID(), test.rows)
			if selected, err := observerelation.NewSelectedSequenceEdit(capability, original, constraint, test.removed); err == nil || selected.Projection().Validate() == nil {
				t.Fatal("foreign or still-desired rows escaped the retirement boundary", test.removed, err)
			}
		})
	}
}

func selectedCapability(t *testing.T, selected target.Target, carrier desiredextension.Carrier) profile.ExtensionOrderCapability {
	t.Helper()
	capability, present := hostsurfacecatalog.Product().ExtensionOrderCapability(selected, target.ScopeProject, carrier)
	if !present {
		t.Fatal("fixture order capability is not admitted")
	}
	return capability
}

func selectedConstraint(t *testing.T, capability profile.ExtensionOrderCapability, members ...hostrelation.RelationOrderMember) hostrelation.RelationOrderConstraint {
	t.Helper()
	constraint, err := hostrelation.NewRelationOrderConstraint(capability.ClassID(), capability.MemberIdentityContract(), capability.RuntimeMeaning(), members)
	if err != nil {
		t.Fatal(err)
	}
	return constraint
}

func selectedOriginal(t *testing.T, capability profile.ExtensionOrderCapability, sequence hostrelation.PhysicalSequenceID, rows []observerelation.ObservedRelationRow) observerelation.ObservedRelationSequence {
	t.Helper()
	original, err := observerelation.NewObservedRelationSequence(capability.ClassID(), sequence, "chosen-array", "original-disk-CAS", rows)
	if err != nil {
		t.Fatal(err)
	}
	return original
}
