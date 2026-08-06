//go:build darwin || linux

package execute

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/residue"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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

func bindTestFileRemovalIntent(
	t *testing.T,
	authority *mutationAuthority,
	destination mutationDestination,
	content []byte,
) {
	t.Helper()
	parentRoot, err := rootedpath.CaptureRoot(filepath.Dir(destination.hostPath))
	if err != nil {
		t.Fatalf("capture test removal parent: %v", err)
	}
	defer parentRoot.Close()
	parentAuthority, err := parentRoot.Authority()
	if err != nil {
		t.Fatalf("read test removal parent authority: %v", err)
	}
	provenance, err := parentAuthority.Provenance()
	if err != nil {
		t.Fatalf("read test removal parent provenance: %v", err)
	}
	parent, err := recovery.NewManifestRootProvenance(
		provenance.PhysicalRoot(),
		provenance.ObjectFingerprint(),
		provenance.MountFingerprint(),
	)
	if err != nil {
		t.Fatalf("construct test removal parent provenance: %v", err)
	}
	retainedRoot, err := rootedpath.CaptureRoot(filepath.Dir(parent.PhysicalRoot()))
	if err != nil {
		t.Fatalf("capture test retained removal ancestor: %v", err)
	}
	defer retainedRoot.Close()
	retainedAuthority, err := retainedRoot.Authority()
	if err != nil {
		t.Fatalf("read test retained removal ancestor authority: %v", err)
	}
	retainedEvidence, err := retainedAuthority.Provenance()
	if err != nil {
		t.Fatalf("read test retained removal ancestor provenance: %v", err)
	}
	retained, err := recovery.NewManifestRootProvenance(
		retainedEvidence.PhysicalRoot(),
		retainedEvidence.ObjectFingerprint(),
		retainedEvidence.MountFingerprint(),
	)
	if err != nil {
		t.Fatalf("construct test retained removal ancestor provenance: %v", err)
	}
	missingSuffix, err := filepath.Rel(retained.PhysicalRoot(), parent.PhysicalRoot())
	if err != nil {
		t.Fatalf("derive test removal parent suffix: %v", err)
	}
	residue, err := residue.NewLogicalRemovalResidueName(".daem-tombstone-test-file")
	if err != nil {
		t.Fatalf("construct test removal residue: %v", err)
	}
	namespace, err := recovery.NewExistingParentAuthority(parent, retained, filepath.ToSlash(missingSuffix), residue)
	if err != nil {
		t.Fatalf("construct test removal namespace: %v", err)
	}
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      recovery.NewPermissionMode(0o600),
		Kind:          recovery.PathKindFile,
		ContentHash:   string(artifact.HashFileContent(content)),
	})
	if err != nil {
		t.Fatalf("construct test removal state: %v", err)
	}
	demand, err := recovery.NewRemovalDemand(
		target.ScopeProject,
		destination.logical,
		[]recovery.RemovalState{state},
	)
	if err != nil {
		t.Fatalf("construct test removal demand: %v", err)
	}
	intent, err := recovery.NewRemovalIntent(demand, namespace)
	if err != nil {
		t.Fatalf("construct test removal intent: %v", err)
	}
	if authority.removalIntents == nil {
		authority.removalIntents = make(map[removalRelationKey]recovery.RemovalIntent)
	}
	authority.removalIntents[removalRelationKey{scope: target.ScopeProject, destination: destination.logical}] = intent
	authority.removalAuthorityBound = true
}

func bindTestFileRemovalIntentForDestination(
	t *testing.T,
	authority *mutationAuthority,
	destination output.Destination,
	content []byte,
) {
	t.Helper()
	bound, err := authority.resolveBoundDestination(target.ScopeProject, destination)
	if err != nil {
		t.Fatalf("resolve test removal destination: %v", err)
	}
	bindTestFileRemovalIntent(t, authority, bound, content)
}

func TestValidateRemovalNamespaceAcceptsRemovedExistingParentThroughRetainedAncestor(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestFileRemovalIntent(t, authority, destination, []byte("inside"))
	intent, present := authority.removalIntents[removalRelationKey{scope: target.ScopeProject, destination: destination.logical}]
	if !present {
		t.Fatal("test removal intent is missing")
	}
	if err := os.Rename(parent, parent+"-moved"); err != nil {
		t.Fatalf("move removal parent away: %v", err)
	}
	if err := validateRemovalNamespace(context.Background(), destination, intent.Namespace()); err != nil {
		t.Fatalf("validateRemovalNamespace after parent disappearance: %v", err)
	}
}

func TestValidateRemovalNamespaceRejectsReplacementExistingParent(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestFileRemovalIntent(t, authority, destination, []byte("inside"))
	intent, present := authority.removalIntents[removalRelationKey{scope: target.ScopeProject, destination: destination.logical}]
	if !present {
		t.Fatal("test removal intent is missing")
	}
	if err := os.Rename(parent, parent+"-moved"); err != nil {
		t.Fatalf("move removal parent away: %v", err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("replace removal parent: %v", err)
	}
	if err := validateRemovalNamespace(context.Background(), destination, intent.Namespace()); err == nil {
		t.Fatal("validateRemovalNamespace accepted replacement parent")
	}
}
