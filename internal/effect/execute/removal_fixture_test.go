package execute

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func testManagedPathRemovalDemandSet(
	t *testing.T,
	before *durable.ManagedPathState,
	beforeMode os.FileMode,
	expected *durable.ManagedPathState,
	expectedMode os.FileMode,
) recovery.RemovalDemandSet {
	t.Helper()
	if before == nil && expected == nil {
		return recovery.RemovalDemandSet{}
	}
	var scope target.Scope
	var destination output.Destination
	if before != nil {
		scope = before.Scope()
		destination = before.Destination()
	} else {
		scope = expected.Scope()
		destination = expected.Destination()
	}
	states := make([]recovery.RemovalState, 0, 2)
	if before != nil {
		state, err := testManagedPathRemovalState(*before, beforeMode, true)
		if err != nil {
			t.Fatalf("construct before removal state: %v", err)
		}
		states = append(states, state)
	}
	if expected != nil {
		state, err := testManagedPathRemovalState(*expected, expectedMode, false)
		if err != nil {
			t.Fatalf("construct expected removal state: %v", err)
		}
		states = append(states, state)
	}
	demand, err := recovery.NewRemovalDemand(scope, destination, states)
	if err != nil {
		t.Fatalf("construct removal demand: %v", err)
	}
	set, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatalf("construct removal demand set: %v", err)
	}
	return set
}

func emptyRemovalDemandsForTest() recovery.RemovalDemandSet {
	return recovery.RemovalDemandSet{}
}

func testManagedPathRemovalState(
	state durable.ManagedPathState,
	mode os.FileMode,
	before bool,
) (recovery.RemovalState, error) {
	kind := recovery.PathKindFile
	pathMode := recovery.NewPermissionMode(mode)
	if state.ContentKind() == realization.PathProjectionDirectory {
		kind = recovery.PathKindDirectory
		pathMode = nil
	}
	if state.ContentHash() == "" {
		return recovery.RemovalState{}, fmt.Errorf("managed path removal state requires content hash")
	}
	if before {
		return recovery.NewBeforeRemovalState(recovery.BeforePathState{
			Existed:     true,
			Kind:        kind,
			ContentHash: string(state.ContentHash()),
			PathMode:    pathMode,
		})
	}
	return recovery.NewExpectedRemovalState(recovery.ExpectedPathState{
		Existed:     true,
		Kind:        kind,
		ContentHash: string(state.ContentHash()),
		PathMode:    pathMode,
	})
}

func bindTestFileRemovalIntent(t *testing.T, authority *mutationAuthority, destination mutationnamespace.Destination, content []byte) {
	t.Helper()
	installRemovalIntentsForTest(t, authority, []recovery.RemovalIntent{testFileRemovalIntent(t, destination, content)})
}

func testFileRemovalIntent(t *testing.T, destination mutationnamespace.Destination, content []byte) recovery.RemovalIntent {
	t.Helper()
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed: true, PathExisted: true, ParentExisted: true, PathMode: recovery.NewPermissionMode(0o600),
		Kind: recovery.PathKindFile, ContentHash: string(artifact.HashFileContent(content)),
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(filepath.Dir(destination.HostPath()))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	selected, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := selected.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	parent, err := recovery.NewRootProvenance(provenance.PhysicalRoot(), provenance.ObjectFingerprint(), provenance.MountFingerprint())
	if err != nil {
		t.Fatal(err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(".daem-tombstone-0123456789abcdef0123456789abcdef", ".daem-cleanup-0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := recovery.NewExistingParentAuthority(parent, names)
	if err != nil {
		t.Fatal(err)
	}
	demand, err := recovery.NewRemovalDemand(destination.Scope(), destination.Logical(), []recovery.RemovalState{state})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := recovery.NewRemovalIntent(demand, namespace)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func installRemovalIntentsForTest(t *testing.T, authority *mutationAuthority, intents []recovery.RemovalIntent) {
	t.Helper()
	demands, err := removaloperation.DemandsForIntents(intents)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(demands.Len())
	if err != nil {
		t.Fatal(err)
	}
	operation := removaloperation.New(authority.destinations, authority.filesystem, authority.control, budget, authority.generalTraversalPhase)
	if err := operation.PrepareDemands(demands, budget); err != nil {
		t.Fatal(err)
	}
	if err := operation.BindIntents(intents); err != nil {
		t.Fatal(err)
	}
	authority.removals, authority.physicalWorkBudget = operation, budget
}
