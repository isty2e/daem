//go:build darwin || linux

package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestObserveRemovalSlotPreservesCancellation(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	parent := filepath.Dir(destination.hostPath)
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	selected := filepath.Join(parent, ".daem-cleanup-0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(selected, nil, 0o600); err != nil {
		t.Fatalf("create removal slot: %v", err)
	}
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal budget: %v", err)
	}
	capability, err := authority.acquireRemovalSlot(destination, selected, budget)
	if err != nil {
		t.Fatalf("bind removal slot: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close removal slot probe: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = authority.observeRemovalSlot(
		ctx,
		destination,
		selected,
		"cleanup stage",
		budget,
		budget.RemainingTreeWork(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cleanup observation error = %v, want context.Canceled", err)
	}
}

func TestAcquireRemovalSlotRejectsExhaustedPathBudget(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	parent := filepath.Dir(destination.hostPath)
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	selected := filepath.Join(parent, ".daem-cleanup-0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(selected, nil, 0o600); err != nil {
		t.Fatalf("create removal slot: %v", err)
	}
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal budget: %v", err)
	}
	for budget.AdmitPathComponents(recovery.MaximumPhysicalPathDepth) == nil {
	}

	_, err = authority.acquireRemovalSlot(destination, selected, budget)
	if err == nil || !strings.Contains(err.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("acquire removal slot error = %v, want pre-I/O budget rejection", err)
	}
	if _, err := os.Stat(selected); err != nil {
		t.Fatalf("slot changed after budget rejection: %v", err)
	}
}

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

func bindTestFileRemovalIntent(
	t *testing.T,
	authority *mutationAuthority,
	destination mutationDestination,
	content []byte,
) {
	t.Helper()
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      recovery.NewPermissionMode(0o600),
		Kind:          recovery.PathKindFile,
		ContentHash:   string(artifact.HashFileContent(content)),
	})
	if err != nil {
		t.Fatalf("construct test file removal state: %v", err)
	}
	bindTestRemovalIntent(t, authority, destination, state)
}

func bindTestDirectoryRemovalIntent(
	t *testing.T,
	authority *mutationAuthority,
	destination mutationDestination,
) {
	t.Helper()
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		Kind:          recovery.PathKindDirectory,
		ContentHash:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("construct test directory removal state: %v", err)
	}
	bindTestRemovalIntent(t, authority, destination, state)
}

func bindTestRemovalIntent(
	t *testing.T,
	authority *mutationAuthority,
	destination mutationDestination,
	state recovery.RemovalState,
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
	parent, err := recovery.NewRootProvenance(
		provenance.PhysicalRoot(),
		provenance.ObjectFingerprint(),
		provenance.MountFingerprint(),
	)
	if err != nil {
		t.Fatalf("construct test removal parent provenance: %v", err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(
		".daem-tombstone-0123456789abcdef0123456789abcdef",
		".daem-cleanup-0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("construct test removal residue: %v", err)
	}
	namespace, err := recovery.NewExistingParentAuthority(parent, names)
	if err != nil {
		t.Fatalf("construct test removal namespace: %v", err)
	}
	demand, err := recovery.NewRemovalDemand(
		destination.scope,
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
	allDemands := authority.removalDemands.Demands()
	replaced := false
	for index, existing := range allDemands {
		if existing.Scope() == demand.Scope() && existing.Destination() == demand.Destination() {
			allDemands[index] = demand
			replaced = true
			break
		}
	}
	if !replaced {
		allDemands = append(allDemands, demand)
	}
	demands, err := recovery.NewRemovalDemandSet(allDemands)
	if err != nil {
		t.Fatalf("construct test removal demand set: %v", err)
	}
	budget, err := recovery.NewPhysicalWorkBudget(demands.Len())
	if err != nil {
		t.Fatalf("construct test removal budget: %v", err)
	}
	if authority.removalBindingsPrepared {
		authority.removalBindingsPrepared = false
		authority.removalDestinations = nil
		authority.physicalWorkBudget = nil
	}
	if err := authority.prepareRemovalDemands(demands, budget); err != nil {
		t.Fatalf("prepare test removal demands: %v", err)
	}
	key := removalRelationKey{scope: destination.scope, destination: destination.logical}
	if authority.removalIntents == nil {
		authority.removalIntents = make(map[removalRelationKey]recovery.RemovalIntent)
	}
	authority.removalIntents[key] = intent
	authority.removalAuthorityBound = true
}

func TestForwardPathCapacityIsPreparedBeforeJournalIntentBinding(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	content := []byte("inside")
	if err := os.WriteFile(filepath.Join(parent, "config"), content, 0o600); err != nil {
		t.Fatalf("write removal candidate: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestFileRemovalIntent(t, authority, destination, content)

	// Resource capacity belongs to the transition demand and physical binding.
	// Durable journal intent authority is established later and remains required
	// by removeJournaledRootedEntry itself.
	authority.removalIntents = nil
	authority.removalAuthorityBound = false
	if err := authority.prepareForwardRemovalReservations(t.Context(), nil); err != nil {
		t.Fatalf("prepare forward removal before journal intent binding: %v", err)
	}
	if authority.forwardRemovalExecution == nil {
		t.Fatal("forward removal path capacity was not transferred before journal publication")
	}
	executionBudget := authority.forwardRemovalExecution
	if err := authority.prepareForwardRemovalReservations(t.Context(), nil); err != nil {
		t.Fatalf("repeat forward removal preparation: %v", err)
	}
	if authority.forwardRemovalExecution != executionBudget {
		t.Fatal("repeat forward preparation replaced or duplicated execution capacity")
	}
	if authority.removalAuthorityBound {
		t.Fatal("resource reservation fabricated durable journal intent authority")
	}
}

func TestForwardPathCapacityFailureDoesNotPublishPartialPreparation(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	content := []byte("inside")
	path := filepath.Join(parent, "config")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write removal candidate: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestFileRemovalIntent(t, authority, destination, content)
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove current candidate before certified forward state: %v", err)
	}

	demand := authority.removalDemands.Demands()[0]
	relation := removalRelationKey{scope: demand.Scope(), destination: demand.Destination()}
	state := demand.States()[0]
	certificate := forwardRemovalCertificate{
		relation: relation,
		state:    state,
		measure: func(context.Context, recovery.ArtifactWork) (recovery.ArtifactWork, error) {
			for authority.physicalWorkBudget.AdmitPathComponents(recovery.MaximumPhysicalPathDepth) == nil {
			}
			for authority.physicalWorkBudget.AdmitPathComponents(1) == nil {
			}
			return recovery.NewArtifactWork(0, int64(len(content)))
		},
	}

	err := authority.prepareForwardRemovalReservations(t.Context(), []forwardRemovalCertificate{certificate})
	if err == nil || !strings.Contains(err.Error(), "reserve forward removal path work") {
		t.Fatalf("forward preparation error = %v, want effect-path reservation failure", err)
	}
	if authority.forwardRemovalPrepared {
		t.Fatal("failed forward path reservation published prepared state")
	}
	if authority.forwardRemovalReservations != nil {
		t.Fatal("failed forward path reservation published candidate reservations")
	}
	if authority.forwardRemovalExecution != nil {
		t.Fatal("failed forward path reservation published an execution budget")
	}
}

func TestForwardRemovalReacquiresCapabilityAgainstExecutionBudget(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	content := []byte("inside")
	path := filepath.Join(parent, "config")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write removal candidate: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestFileRemovalIntent(t, authority, destination, content)
	if err := authority.prepareForwardRemovalReservations(t.Context(), nil); err != nil {
		t.Fatalf("prepare forward removal: %v", err)
	}
	for authority.forwardRemovalExecution.AdmitPathComponents(1) == nil {
	}

	capability, err := authority.acquire(destination)
	if err != nil {
		t.Fatalf("acquire pre-removal capability: %v", err)
	}
	expected, err := authority.filesystem.CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		_ = capability.Close()
		t.Fatalf("capture removal identity: %v", err)
	}
	_, err = authority.removeJournaledRootedEntry(
		t.Context(),
		destination,
		capability,
		expected,
	)
	cause := errors.Unwrap(err)
	if err == nil || cause == nil || !strings.Contains(cause.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("forward removal error = %v, want bounded capability rejection", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("candidate changed after pre-effect budget rejection: %v", err)
	}
}

func TestRemovalBindingReusesCanonicalGlobalDestination(t *testing.T) {
	firstParent := filepath.Join(t.TempDir(), "first")
	secondParent := filepath.Join(t.TempDir(), "second")
	for _, parent := range []string{firstParent, secondParent} {
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatalf("create global parent %q: %v", parent, err)
		}
	}
	selectedPath := filepath.Join(firstParent, "config")
	destination := outputtest.Parse(t, "~/config")
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      recovery.NewPermissionMode(0o600),
		Kind:          recovery.PathKindFile,
		ContentHash:   string(artifact.HashFileContent([]byte("inside"))),
	})
	if err != nil {
		t.Fatalf("construct removal state: %v", err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeGlobal, destination, []recovery.RemovalState{state})
	if err != nil {
		t.Fatalf("construct removal demand: %v", err)
	}
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatalf("construct removal demand set: %v", err)
	}
	resolverCalls := 0
	authority, err := newMutationAuthorityWithProjectionEffects(
		Paths{},
		[]ManagedPathEffect{{remove: &managedPathRemoveEffect{facts: managedPathEffectFacts{
			scope: target.ScopeGlobal, destination: destination,
		}}}},
		nil,
		demands,
		nil,
		func(output.Destination) (string, error) {
			resolverCalls++
			if resolverCalls == 1 {
				return selectedPath, nil
			}
			return filepath.Join(secondParent, "config"), nil
		},
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("construct mutation authority: %v", err)
	}
	defer authority.close()
	if resolverCalls != 1 {
		t.Fatalf("global resolver calls = %d, want one bounded selection", resolverCalls)
	}
	canonical, err := authority.resolveBoundDestination(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("resolve canonical global destination: %v", err)
	}
	removal, present := authority.removalDestinations[removalRelationKey{
		scope: target.ScopeGlobal, destination: destination,
	}]
	if !present {
		t.Fatal("prepared removal destination is missing")
	}
	if removal.hostPath != canonical.hostPath {
		t.Fatalf(
			"removal destination = %q, want canonical mutation destination %q",
			removal.hostPath,
			canonical.hostPath,
		)
	}
	if removal.root != canonical.root || !removal.destination.Equal(canonical.destination) {
		t.Fatal("general mutation and removal did not retain one physical binding")
	}
}

func TestRemovalDemandBoundsProjectRootBeforeMutationBinding(t *testing.T) {
	physicalRoot := t.TempDir()
	components := make([]string, recovery.MaximumPhysicalPathDepth+1)
	for index := range components {
		components[index] = "d"
	}
	deepRoot := filepath.Join(append([]string{physicalRoot}, components...)...)
	if err := os.MkdirAll(deepRoot, 0o700); err != nil {
		t.Fatalf("create deep project root: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(deepRoot, alias); err != nil {
		t.Fatalf("create project alias: %v", err)
	}
	destination := outputtest.Parse(t, ".agent/config")
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      recovery.NewPermissionMode(0o600),
		Kind:          recovery.PathKindFile,
		ContentHash:   string(artifact.HashFileContent([]byte("inside"))),
	})
	if err != nil {
		t.Fatalf("construct removal state: %v", err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, []recovery.RemovalState{state})
	if err != nil {
		t.Fatalf("construct removal demand: %v", err)
	}
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatalf("construct removal demand set: %v", err)
	}
	authority, err := newMutationAuthorityWithProjectionEffects(
		Paths{ManifestRoot: alias},
		[]ManagedPathEffect{{remove: &managedPathRemoveEffect{facts: managedPathEffectFacts{
			scope: target.ScopeProject, destination: destination,
		}}}},
		nil,
		demands,
		nil,
		destinationResolver(Paths{ManifestRoot: alias}),
		testFilesystem(),
		nil,
	)
	if authority != nil {
		_ = authority.close()
	}
	if err == nil || !strings.Contains(err.Error(), "physical path depth") {
		t.Fatalf("construct mutation authority error = %v, want bounded project-root rejection", err)
	}
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

func boundRemovalDestinationForTest(
	t *testing.T,
	authority *mutationAuthority,
	destination mutationDestination,
) mutationDestination {
	t.Helper()
	bound, err := authority.removalDestinationFor(destination.scope, destination.logical)
	if err != nil {
		t.Fatalf("resolve test removal destination: %v", err)
	}
	return bound
}

func TestValidateRemovalNamespaceRejectsRelocatedExistingParent(t *testing.T) {
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
	removalDestination, err := authority.removalDestinationFor(destination.scope, destination.logical)
	if err != nil {
		t.Fatalf("resolve removal namespace destination: %v", err)
	}
	observation, err := journal.ObserveRemovalNamespace(
		context.Background(),
		removalDestination.root,
		removalDestination.destination,
		intent.Namespace(),
		authority.physicalWorkBudget,
	)
	if err != nil {
		t.Fatalf("observeRemovalNamespace after parent relocation: %v", err)
	}
	if observation.Status() != recovery.RemovalNamespaceChanged ||
		!strings.Contains(observation.Detail(), "no longer matches") {
		t.Fatalf("namespace observation = %#v, want relocation blocker", observation)
	}
	if err := os.Rename(parent+"-moved", parent); err != nil {
		t.Fatalf("restore relocated removal parent: %v", err)
	}
	observation, err = journal.ObserveRemovalNamespace(
		context.Background(),
		removalDestination.root,
		removalDestination.destination,
		intent.Namespace(),
		authority.physicalWorkBudget,
	)
	if err != nil {
		t.Fatalf("observe restored removal parent: %v", err)
	}
	if observation.Status() != recovery.RemovalNamespaceMatched {
		t.Fatalf("restored namespace status = %q, want matched", observation.Status())
	}
}

func TestObserveRemovalNamespaceAdmitsRootValidationBeforeFilesystemWork(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestFileRemovalIntent(t, authority, destination, []byte("inside"))
	intent, present := authority.removalIntents[removalRelationKey{
		scope: target.ScopeProject, destination: destination.logical,
	}]
	if !present {
		t.Fatal("test removal intent is missing")
	}
	removalDestination, err := authority.removalDestinationFor(
		destination.scope,
		destination.logical,
	)
	if err != nil {
		t.Fatalf("resolve removal namespace destination: %v", err)
	}
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal budget: %v", err)
	}
	for budget.AdmitPathComponents(recovery.MaximumPhysicalPathDepth) == nil {
	}
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatalf("replace selected root path: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement selected root: %v", err)
	}

	_, err = journal.ObserveRemovalNamespace(
		t.Context(),
		removalDestination.root,
		removalDestination.destination,
		intent.Namespace(),
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("namespace observation error = %v, want pre-I/O budget rejection", err)
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
	removalDestination, err := authority.removalDestinationFor(destination.scope, destination.logical)
	if err != nil {
		t.Fatalf("resolve removal namespace destination: %v", err)
	}
	if err := validateRemovalNamespace(
		context.Background(),
		removalDestination,
		intent.Namespace(),
		authority.physicalWorkBudget,
	); err == nil {
		t.Fatal("validateRemovalNamespace accepted replacement parent")
	}
}

func TestValidateRemovalNamespaceHandlesInitiallyAbsentParentVariants(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, string)
		want    recovery.RemovalNamespaceObservationStatus
	}{
		{
			name: "remains absent",
			want: recovery.RemovalNamespaceMatched,
		},
		{
			name: "created on retained mount",
			prepare: func(t *testing.T, parent string) {
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatalf("create parent: %v", err)
				}
			},
			want: recovery.RemovalNamespaceMatched,
		},
		{
			name: "created as file",
			prepare: func(t *testing.T, parent string) {
				if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("create parent file: %v", err)
				}
			},
			want: recovery.RemovalNamespaceChanged,
		},
		{
			name: "created as symlink",
			prepare: func(t *testing.T, parent string) {
				if err := os.Symlink(t.TempDir(), parent); err != nil {
					t.Fatalf("create parent symlink: %v", err)
				}
			},
			want: recovery.RemovalNamespaceChanged,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := newProjectRootForMutationTest(t)
			_, destination := projectMutationDestinationForTest(t, root, ".agent/config")
			namespace := testInitiallyAbsentRemovalNamespace(t, destination, ".agent")
			budget, err := recovery.NewPhysicalWorkBudget(1)
			if err != nil {
				t.Fatalf("construct removal budget: %v", err)
			}
			captured, bound, err := rootedpath.CaptureDestinationBounded(
				destination.hostPath,
				recovery.MaximumPhysicalPathDepth,
				budget,
			)
			if err != nil {
				t.Fatalf("capture current removal destination: %v", err)
			}
			defer captured.Close()
			if test.prepare != nil {
				test.prepare(t, filepath.Join(root, ".agent"))
			}

			observation, err := journal.ObserveRemovalNamespace(
				t.Context(),
				captured,
				bound,
				namespace,
				budget,
			)
			if err != nil {
				t.Fatalf("observe initially absent removal namespace: %v", err)
			}
			if observation.Status() != test.want {
				t.Fatalf("namespace status = %q, want %q; detail=%q", observation.Status(), test.want, observation.Detail())
			}
		})
	}
}

func testInitiallyAbsentRemovalNamespace(
	t *testing.T,
	destination mutationDestination,
	missingSuffix string,
) recovery.RemovalNamespaceAuthority {
	t.Helper()
	authority, err := destination.root.Authority()
	if err != nil {
		t.Fatalf("read retained root authority: %v", err)
	}
	evidence, err := authority.Provenance()
	if err != nil {
		t.Fatalf("read retained root provenance: %v", err)
	}
	provenance, err := recovery.NewRootProvenance(
		evidence.PhysicalRoot(),
		evidence.ObjectFingerprint(),
		evidence.MountFingerprint(),
	)
	if err != nil {
		t.Fatalf("construct retained root provenance: %v", err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(
		".daem-tombstone-fedcba9876543210fedcba9876543210",
		".daem-cleanup-fedcba9876543210fedcba9876543210",
	)
	if err != nil {
		t.Fatalf("construct logical removal names: %v", err)
	}
	namespace, err := recovery.NewInitiallyAbsentParentAuthority(provenance, missingSuffix, names)
	if err != nil {
		t.Fatalf("construct initially absent removal namespace: %v", err)
	}
	return namespace
}

func TestExecuteRemovalCleanupCandidateResumesPartialCleanupStage(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestDirectoryRemovalIntent(t, authority, destination)
	intent := authority.removalIntents[removalRelationKey{
		scope: target.ScopeProject, destination: destination.logical,
	}]
	residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
	if err != nil {
		t.Fatalf("derive removal namespace paths: %v", err)
	}
	if err := os.Mkdir(cleanupPath, 0o700); err != nil {
		t.Fatalf("create partial cleanup stage: %v", err)
	}
	writeMutationTestFile(t, filepath.Join(cleanupPath, "remaining"), "partial")

	candidate := preflightRemovalCleanupCandidateForTest(t, removalCleanupCandidate{
		intent: intent, destination: boundRemovalDestinationForTest(t, authority, destination),
		residuePath: residuePath, cleanupPath: cleanupPath,
	}, true)
	obligation, err := authority.executeRemovalCleanupCandidate(t.Context(), candidate)
	if err != nil {
		t.Fatalf(
			"executeRemovalCleanupCandidate: %v; cause=%v; root-cause=%v",
			err,
			errors.Unwrap(err),
			errors.Unwrap(errors.Unwrap(err)),
		)
	}
	if obligation.Readiness() != recovery.RemovalCleanupDischarged ||
		obligation.Action() != recovery.RemovalCleanupActionCleanupProgress {
		t.Fatalf("cleanup obligation = %#v, want discharged cleanup progress", obligation)
	}
	for _, path := range []string{residuePath, cleanupPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("removal slot %q after cleanup = %v, want absent", filepath.Base(path), err)
		}
	}
}

func TestExecuteRemovalCleanupCandidatePromotesValidatedResidue(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	content := []byte("original")
	bindTestFileRemovalIntent(t, authority, destination, content)
	intent := authority.removalIntents[removalRelationKey{
		scope: target.ScopeProject, destination: destination.logical,
	}]
	residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
	if err != nil {
		t.Fatalf("derive removal namespace paths: %v", err)
	}
	if err := os.WriteFile(residuePath, content, 0o600); err != nil {
		t.Fatalf("write validated residue: %v", err)
	}

	candidate := preflightRemovalCleanupCandidateForTest(t, removalCleanupCandidate{
		intent: intent, destination: boundRemovalDestinationForTest(t, authority, destination),
		residuePath: residuePath, cleanupPath: cleanupPath,
	}, false)
	obligation, err := authority.executeRemovalCleanupCandidate(t.Context(), candidate)
	if err != nil {
		t.Fatalf(
			"executeRemovalCleanupCandidate: %v; cause=%v; root-cause=%v",
			err,
			errors.Unwrap(err),
			errors.Unwrap(errors.Unwrap(err)),
		)
	}
	if obligation.Readiness() != recovery.RemovalCleanupDischarged ||
		obligation.Action() != recovery.RemovalCleanupActionPromoteResidue {
		t.Fatalf("cleanup obligation = %#v, want discharged promotion", obligation)
	}
	for _, path := range []string{residuePath, cleanupPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("removal slot %q after cleanup = %v, want absent", filepath.Base(path), err)
		}
	}
}

func TestExecuteRemovalCleanupCandidatePreservesMismatchedResidue(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestFileRemovalIntent(t, authority, destination, []byte("authorized"))
	intent := authority.removalIntents[removalRelationKey{
		scope: target.ScopeProject, destination: destination.logical,
	}]
	residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
	if err != nil {
		t.Fatalf("derive removal namespace paths: %v", err)
	}
	if err := os.WriteFile(residuePath, []byte("foreign"), 0o600); err != nil {
		t.Fatalf("write mismatched residue: %v", err)
	}

	candidate := preflightRemovalCleanupCandidateForTest(t, removalCleanupCandidate{
		intent: intent, destination: boundRemovalDestinationForTest(t, authority, destination),
		residuePath: residuePath, cleanupPath: cleanupPath,
	}, false)
	_, err = authority.executeRemovalCleanupCandidate(t.Context(), candidate)
	var cleanupErr *removalCleanupError
	if !errors.As(err, &cleanupErr) ||
		cleanupErr.readiness != recovery.RemovalCleanupBlocked ||
		cleanupErr.reason != recovery.RemovalCleanupReasonResidueMismatch {
		t.Fatalf("cleanup error = %#v, want blocked residue mismatch", err)
	}
	assertMutationTestFile(t, residuePath, "foreign")
	if _, err := os.Lstat(cleanupPath); !os.IsNotExist(err) {
		t.Fatalf("cleanup stage after blocked mismatch = %v, want absent", err)
	}
}

func TestExecuteRemovalCleanupCandidateDurablyConfirmsBothSlotsAbsent(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	bindTestFileRemovalIntent(t, authority, destination, []byte("authorized"))
	intent := authority.removalIntents[removalRelationKey{
		scope: target.ScopeProject, destination: destination.logical,
	}]
	residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
	if err != nil {
		t.Fatalf("derive removal namespace paths: %v", err)
	}

	candidate := preflightRemovalCleanupCandidateForTest(t, removalCleanupCandidate{
		intent: intent, destination: boundRemovalDestinationForTest(t, authority, destination),
		residuePath: residuePath, cleanupPath: cleanupPath,
	}, false)
	obligation, err := authority.executeRemovalCleanupCandidate(t.Context(), candidate)
	if err != nil {
		t.Fatalf("executeRemovalCleanupCandidate: %v", err)
	}
	if obligation.Readiness() != recovery.RemovalCleanupDischarged ||
		obligation.Action() != recovery.RemovalCleanupActionConfirmAbsence {
		t.Fatalf("cleanup obligation = %#v, want discharged absence confirmation", obligation)
	}
}

func TestExecuteRemovalCleanupCandidateHandlesZeroWorkEntries(t *testing.T) {
	for _, test := range []struct {
		name      string
		prepare   func(*testing.T, *mutationAuthority, mutationDestination) (recovery.RemovalIntent, string, string)
		recursive bool
		want      recovery.RemovalCleanupActionKind
	}{
		{
			name: "empty file residue",
			prepare: func(t *testing.T, authority *mutationAuthority, destination mutationDestination) (
				recovery.RemovalIntent,
				string,
				string,
			) {
				bindTestFileRemovalIntent(t, authority, destination, nil)
				intent := authority.removalIntents[removalRelationKey{
					scope: target.ScopeProject, destination: destination.logical,
				}]
				residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(residuePath, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return intent, residuePath, cleanupPath
			},
			want: recovery.RemovalCleanupActionPromoteResidue,
		},
		{
			name: "empty directory cleanup stage",
			prepare: func(t *testing.T, authority *mutationAuthority, destination mutationDestination) (
				recovery.RemovalIntent,
				string,
				string,
			) {
				bindTestDirectoryRemovalIntent(t, authority, destination)
				intent := authority.removalIntents[removalRelationKey{
					scope: target.ScopeProject, destination: destination.logical,
				}]
				residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(cleanupPath, 0o700); err != nil {
					t.Fatal(err)
				}
				return intent, residuePath, cleanupPath
			},
			recursive: true,
			want:      recovery.RemovalCleanupActionCleanupProgress,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := newProjectRootForMutationTest(t)
			parent := filepath.Join(root, ".agent")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
			intent, residuePath, cleanupPath := test.prepare(t, authority, destination)
			zero, err := recovery.NewArtifactWork(0, 0)
			if err != nil {
				t.Fatal(err)
			}
			candidate := preflightRemovalCleanupCandidateWithWorkForTest(
				t,
				removalCleanupCandidate{
					intent: intent, destination: boundRemovalDestinationForTest(t, authority, destination),
					residuePath: residuePath, cleanupPath: cleanupPath,
				},
				test.recursive,
				zero,
			)

			obligation, err := authority.executeRemovalCleanupCandidate(t.Context(), candidate)
			if err != nil {
				t.Fatalf("execute zero-work cleanup candidate: %v", err)
			}
			if obligation.Readiness() != recovery.RemovalCleanupDischarged ||
				obligation.Action() != test.want {
				t.Fatalf("zero-work cleanup obligation = %#v, want discharged %q", obligation, test.want)
			}
			for _, path := range []string{residuePath, cleanupPath} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("zero-work removal slot %q after cleanup = %v, want absent", path, err)
				}
			}
		})
	}
}

type growRemovalStageBeforeCleanupStore struct {
	mutationfs.Store
	grow  func(string)
	calls int
}

func (filesystem *growRemovalStageBeforeCleanupStore) CleanupRootedRemovalStage(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
	names mutationfs.LogicalRemovalNames,
	limits mutationfs.TreeTraversalLimits,
) (mutationfs.CommitOutcome, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	filesystem.calls++
	filesystem.grow(path)
	return filesystem.Store.CleanupRootedRemovalStage(
		ctx,
		capability,
		expected,
		names,
		limits,
	)
}

func TestExecuteRemovalCleanupRejectsGrowthBeyondZeroSemanticWork(t *testing.T) {
	for _, test := range []struct {
		name      string
		directory bool
		prepare   func(*testing.T, string)
		grow      func(*testing.T, string)
	}{
		{
			name: "file gains one byte",
			prepare: func(t *testing.T, path string) {
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			grow: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:      "directory gains one child",
			directory: true,
			prepare: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
			grow: func(t *testing.T, path string) {
				if err := os.WriteFile(filepath.Join(path, "new"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := newProjectRootForMutationTest(t)
			parent := filepath.Join(root, ".agent")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
			if test.directory {
				bindTestDirectoryRemovalIntent(t, authority, destination)
			} else {
				bindTestFileRemovalIntent(t, authority, destination, nil)
			}
			intent := authority.removalIntents[removalRelationKey{
				scope: target.ScopeProject, destination: destination.logical,
			}]
			residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
			if err != nil {
				t.Fatal(err)
			}
			test.prepare(t, cleanupPath)
			base := authority.filesystem
			filesystem := &growRemovalStageBeforeCleanupStore{
				Store: base,
				grow:  func(path string) { test.grow(t, path) },
			}
			authority.filesystem = filesystem
			zero, err := recovery.NewArtifactWork(0, 0)
			if err != nil {
				t.Fatal(err)
			}
			candidate := preflightRemovalCleanupCandidateWithWorkForTest(
				t,
				removalCleanupCandidate{
					intent: intent, destination: boundRemovalDestinationForTest(t, authority, destination),
					residuePath: residuePath, cleanupPath: cleanupPath,
				},
				test.directory,
				zero,
			)

			if _, err := authority.executeRemovalCleanupCandidate(t.Context(), candidate); err == nil {
				t.Fatal("cleanup accepted growth beyond zero semantic work")
			}
			if filesystem.calls != 1 {
				t.Fatalf("cleanup calls = %d, want 1", filesystem.calls)
			}
			if _, err := os.Lstat(cleanupPath); err != nil {
				t.Fatalf("grown cleanup stage was removed: %v", err)
			}
		})
	}
}

func preflightRemovalCleanupCandidateForTest(
	t *testing.T,
	candidate removalCleanupCandidate,
	recursive bool,
) removalCleanupCandidate {
	t.Helper()
	work, err := recovery.NewArtifactWork(128, 1<<20)
	if err != nil {
		t.Fatalf("construct removal tree work: %v", err)
	}
	return preflightRemovalCleanupCandidateWithWorkForTest(t, candidate, recursive, work)
}

func preflightRemovalCleanupCandidateWithWorkForTest(
	t *testing.T,
	candidate removalCleanupCandidate,
	recursive bool,
	work recovery.ArtifactWork,
) removalCleanupCandidate {
	t.Helper()
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := journal.ReserveRemovalExecutionObservationWork(
		budget,
		candidate.destination.hostPath,
		candidate.residuePath,
		candidate.cleanupPath,
	); err != nil {
		t.Fatalf("reserve removal observation work: %v", err)
	}
	var reserveErr error
	if recursive {
		reserveErr = budget.ReserveDirectoryReobservation(work)
	} else {
		reserveErr = budget.ReserveReobservation(work)
	}
	if reserveErr != nil {
		t.Fatalf("reserve removal reobservation: %v", reserveErr)
	}
	if recursive {
		if err := budget.ReserveDirectoryCleanup(work); err != nil {
			t.Fatalf("reserve removal directory cleanup: %v", err)
		}
	}
	executionBudget, err := budget.BeginReservedExecution()
	if err != nil {
		t.Fatalf("begin reserved removal execution: %v", err)
	}
	candidate.budget = executionBudget
	candidate.residueWork = work
	candidate.cleanupSlotWork = work
	candidate.cleanupWork = work
	candidate.executionPreflighted = true
	return candidate
}

func TestExecuteRemovalCleanupCandidateRequiresCompleteOperationPreflight(t *testing.T) {
	authority := mutationAuthority{}
	_, err := authority.executeRemovalCleanupCandidate(
		t.Context(),
		removalCleanupCandidate{},
	)
	if err == nil || !strings.Contains(err.Error(), "lacks complete operation preflight") {
		t.Fatalf("unpreflighted cleanup error = %v", err)
	}
}

func TestRemovalCleanupActionFailureHasStablePublicSafeReason(t *testing.T) {
	root := newProjectRootForMutationTest(t)
	parent := filepath.Join(root, ".agent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("create removal parent: %v", err)
	}
	authority, destination := projectMutationDestinationForTest(t, root, ".agent/config")
	content := []byte("authorized")
	bindTestFileRemovalIntent(t, authority, destination, content)
	intent := authority.removalIntents[removalRelationKey{
		scope: target.ScopeProject, destination: destination.logical,
	}]
	matched, err := recovery.NewRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
	if err != nil {
		t.Fatalf("construct matched namespace: %v", err)
	}
	residue, err := recovery.NewRemovalResidueEntryObservation(
		recovery.RemovalResidueEntryPresent,
		recovery.PathKindFile,
		string(artifact.HashFileContent(content)),
		recovery.NewPermissionMode(0o600),
		"",
		"",
	)
	if err != nil {
		t.Fatalf("construct residue observation: %v", err)
	}
	absent, err := recovery.NewRemovalResidueEntryObservation(
		recovery.RemovalResidueEntryAbsent,
		"",
		"",
		nil,
		"",
		"",
	)
	if err != nil {
		t.Fatalf("construct absent observation: %v", err)
	}
	obligation, err := intent.AssessCleanup(
		matched,
		recovery.NewRemovalResidueObservation(residue, absent),
	)
	if err != nil {
		t.Fatalf("assess cleanup: %v", err)
	}

	publicErr := newRemovalCleanupError(obligation, errors.New("private path /Users/example/secret"))
	var cleanupErr *removalCleanupError
	if !errors.As(publicErr, &cleanupErr) {
		t.Fatalf("cleanup error type = %T, want *removalCleanupError", publicErr)
	}
	if cleanupErr.reason != recovery.RemovalCleanupReasonActionFailed ||
		!strings.Contains(publicErr.Error(), `cleanup action "promote_residue" did not complete`) {
		t.Fatalf("cleanup error = %v, want stable action failure", publicErr)
	}
	if strings.Contains(publicErr.Error(), "/Users/example/secret") {
		t.Fatalf("cleanup error exposed private cause: %v", publicErr)
	}
}
