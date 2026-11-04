package recovery

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestDocumentRemovalRetainsRelationsAndOnePhysicalIntent(t *testing.T) {
	first := testRemovalIntent(t)
	destination, err := output.Parse("@pi-agent/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRemovalDemand(target.ScopeGlobal, destination, first.States())
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewRemovalDemandSet([]RemovalDemand{first.Demand(), second})
	if err != nil {
		t.Fatal(err)
	}
	removal, err := NewDocumentRemoval(set, first.Namespace())
	if err != nil || !removal.Demands().Equal(set) {
		t.Fatalf("logical coverage changed: %v", err)
	}
	physical, err := removal.PhysicalIntent()
	if err != nil || !physical.Namespace().equal(first.Namespace()) {
		t.Fatalf("physical cleanup namespace changed: %v", err)
	}
	if len(physical.States()) != len(first.States()) || !physical.States()[0].Equal(first.States()[0]) {
		t.Fatal("physical cleanup lost the complete file envelope")
	}
	copy := removal.Demands().Demands()
	copy[0] = RemovalDemand{}
	if !removal.Demands().Equal(set) {
		t.Fatal("returned relation slice mutated the group")
	}
}

func TestDocumentRemovalRejectsInvalidAndDisagreeingEnvelopes(t *testing.T) {
	first := testRemovalIntent(t)
	before, _ := first.States()[0].Before()
	before.PathMode = NewPermissionMode(0o700)
	changed, err := NewBeforeRemovalState(before)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("other.json")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewRemovalDemand(target.ScopeProject, destination, []RemovalState{changed})
	if err != nil {
		t.Fatal(err)
	}
	disagreeing, err := NewRemovalDemandSet([]RemovalDemand{first.Demand(), other})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDocumentRemoval(disagreeing, first.Namespace()); err == nil {
		t.Fatal("one physical removal accepted differing file modes")
	}
	if _, err := NewDocumentRemoval(RemovalDemandSet{}, first.Namespace()); err == nil {
		t.Fatal("empty logical coverage admitted")
	}
	valid, err := NewRemovalDemandSet([]RemovalDemand{first.Demand()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDocumentRemoval(valid, RemovalNamespaceAuthority{}); err == nil {
		t.Fatal("missing namespace admitted")
	}
	directory := testRemovalIntentForKind(t, PathKindDirectory)
	set, err := NewRemovalDemandSet([]RemovalDemand{directory.Demand()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDocumentRemoval(set, directory.Namespace()); err == nil {
		t.Fatal("document group acquired directory-removal authority")
	}
	if _, err := (DocumentRemoval{}).PhysicalIntent(); err == nil {
		t.Fatal("zero removal group acquired a physical intent")
	}
}
