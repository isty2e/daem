package pipackage_test

import (
	"bytes"
	"path/filepath"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestOrderSelectionComparisonSeparatesIntentFromObservedContent(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	input, _ := loadedOrderFixture(t, target.ScopeProject, filepath.Join(root, ".pi"), root, home,
		[]orderSpec{{id: "a", source: "npm:a"}, {id: "b", source: "npm:b"}},
		aggregate.ExistingDocument([]byte(`{"packages":["npm:b","npm:a"],"theme":"day"}`)))
	before, err := observepipackage.ReconstructOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Document = aggregate.ExistingDocument([]byte(`{"packages":["npm:a","npm:foreign","npm:b"],"theme":"night"}`))
	fresh, err := observepipackage.ReconstructOrder(input)
	if err != nil || !before.SameSelection(fresh) {
		t.Fatal("actual post-command membership or candidate changed locked order intent", err)
	}
	if before.SameSelection(observepipackage.OrderObservation{}) {
		t.Fatal("an invalid observation became order intent")
	}
	input.SettingsContext, err = extensiontopology.NewLocalSourceContext(input.SettingsContext.BaseRoot(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherContext, err := observepipackage.ReconstructOrder(input)
	if err != nil || before.SameSelection(otherContext) {
		t.Fatal("changed settings interpretation became the same order intent", err)
	}
	otherInput, _ := loadedOrderFixture(t, target.ScopeProject, filepath.Join(root, ".pi"), root, home,
		[]orderSpec{{id: "b", source: "npm:b"}, {id: "a", source: "npm:a"}}, input.Document)
	otherOrder, err := observepipackage.ReconstructOrder(otherInput)
	if err != nil || before.SameSelection(otherOrder) {
		t.Fatal("changed locked precedence became the same order intent", err)
	}
}

func TestOrderCaptureAndProjectionPreserveActualInputAndExplicitContexts(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	before := aggregate.ExistingDocument([]byte(`{"packages":["npm:b","npm:foreign", "npm:a"],"theme":"day"}`))
	loaded, live := loadedOrderFixture(t, target.ScopeProject, filepath.Join(root, ".pi"), root, home,
		[]orderSpec{{id: "a", source: "npm:a"}, {id: "b", source: "npm:b"}}, before)
	observation, err := observepipackage.ReconstructOrder(loaded)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := observation.CaptureInput()
	if err != nil || !captured.Document.Equal(before) {
		t.Fatalf("capture did not retain the actual original: %v", err)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	replayed, err := observepipackage.ReconstructOrder(captured)
	if err != nil {
		t.Fatal(err)
	}
	candidate, _ := observation.Candidate()
	replayedBytes, _ := replayed.Candidate()
	if !bytes.Equal(candidate, replayedBytes) {
		t.Fatal("capture changed the actual selected order")
	}
	current := aggregate.ExistingDocument(bytes.Replace(before.Content(), []byte(`"day"`), []byte(`"night"`), 1))
	projected, err := observation.Project(current)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(projected.Content(), []byte(`"theme":"night"`)) {
		t.Fatal("Pi projection overwrote an independent native edit")
	}
	if err := observation.VerifyPackageContent(projected.Content(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := observation.Project(aggregate.ExistingDocument(bytes.Replace(before.Content(), []byte("npm:foreign"), []byte("npm:third"), 1))); err == nil {
		t.Fatal("Pi projection accepted a third package baseline")
	}
	captured.Relations[0] = observepipackage.ScopedRelation{}
	if _, err := observation.CaptureInput(); err != nil {
		t.Fatal("returned relation-slice mutation changed the observation")
	}
	writeSettingsBytes(t, observation.SettingsPath(), before.Content())
	ambient, err := observepipackage.ReadOrder(live)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ambient.CaptureInput(); err == nil {
		t.Fatal("ambient-only observation became replay evidence")
	}
}

func TestOrderCaptureRetainsAndComparesTheSuppliedProjectionContract(t *testing.T) {
	root := t.TempDir()
	input, _ := loadedOrderFixture(t, target.ScopeProject, filepath.Join(root, ".pi"), root, root,
		[]orderSpec{{id: "a", source: "npm:a"}, {id: "b", source: "npm:b"}},
		aggregate.ExistingDocument([]byte(`{"packages":["npm:b","npm:a"]}`)))
	original, err := observepipackage.ReconstructOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	projection := input.Capability.NativeSequences()[0]
	alternate, err := profile.NewNativeSequenceProjection(projection.SequenceID(), projection.Destination(), configuration.JSONC, projection.ArrayPath())
	if err != nil {
		t.Fatal(err)
	}
	input.Capability, err = profile.NewExtensionOrderCapability(profile.ExtensionOrderCapabilityInput{
		Carrier: input.Capability.Carrier(), Scope: input.Scope, ClassID: input.Capability.ClassID(),
		MemberIdentityContract: input.Capability.MemberIdentityContract(), SequenceMembership: input.Capability.SequenceMembership(),
		NativeSequences: []profile.NativeSequenceProjection{alternate}, RuntimeMeaning: input.Capability.RuntimeMeaning(),
	})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := observepipackage.ReconstructOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := changed.CaptureInput()
	if err != nil || !captured.Capability.Equal(input.Capability) {
		t.Fatal("capture replaced the supplied projection with the current catalog", err)
	}
	if original.SameSelection(changed) || changed.SameSelection(original) {
		t.Fatal("different grammar contracts became the same replayable intent")
	}
}

func TestOrderProjectionDoesNotCreateMissingSettingsOrPackageArrays(t *testing.T) {
	for _, original := range []aggregate.Document{
		aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{}`)),
		aggregate.ExistingDocument([]byte(`{"packages":[]}`)),
	} {
		root := t.TempDir()
		input, _ := loadedOrderFixture(t, target.ScopeProject, filepath.Join(root, ".pi"), root, t.TempDir(),
			[]orderSpec{{id: "a", source: "npm:a"}}, original)
		observation, err := observepipackage.ReconstructOrder(input)
		if err != nil {
			t.Fatal(err)
		}
		projected, err := observation.Project(original)
		if err != nil || !projected.Equal(original) || observation.Changed() {
			t.Fatalf("no-op order changed document or array presence: %v", err)
		}
	}
}
