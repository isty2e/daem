package execute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestMutationAuthoritySeparatesProjectAndGlobalDestinations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	dataDir := t.TempDir()
	allowedGlobalPath := filepath.Join(dataDir, "managed")
	t.Setenv("HOME", dataDir)
	t.Setenv("USERPROFILE", dataDir)
	selected := []scopedDestinationForTest{
		{scope: target.ScopeProject, destination: outputtest.Parse(t, ".agents/skill")},
		{scope: target.ScopeGlobal, destination: outputtest.Parse(t, "~/managed")},
	}
	paths := Paths{ManifestRoot: root, DataDir: dataDir}
	authority, err := captureScopedDestinationsForTest(
		paths,
		selected,
		emptyRemovalDemandsForTest(),
		nil,
		destinationResolver(paths),
		testFilesystem(),
	)
	if err != nil {
		t.Fatalf("newMutationAuthorityWithProjectionEffects returned error: %v", err)
	}
	defer authority.close()

	projectDestination, err := authority.destinations.Resolve(
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skill"),
	)
	if err != nil {
		t.Fatalf("resolve project destination: %v", err)
	}
	projectProvenance, err := authority.destinations.ProjectProvenance()
	if err != nil {
		t.Fatal(err)
	}
	wantProjectPath := filepath.Join(projectProvenance.PhysicalRoot(), ".agents", "skill")
	if !projectDestination.IsRooted() || projectDestination.Scope() != target.ScopeProject ||
		projectDestination.HostPath() != wantProjectPath {
		t.Fatalf("project destination = %+v", projectDestination)
	}
	capability, err := authority.destinations.Acquire(projectDestination)
	if err != nil {
		t.Fatalf("acquire project destination: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close project capability: %v", err)
	}

	globalDestination, err := authority.destinations.Resolve(
		target.ScopeGlobal,
		outputtest.Parse(t, "~/managed"),
	)
	wantGlobalPath, canonicalErr := mutation.CanonicalDirectoryEntryPath(allowedGlobalPath)
	if canonicalErr != nil {
		t.Fatalf("canonicalize allowed global path: %v", canonicalErr)
	}
	if err != nil || !globalDestination.IsRooted() || globalDestination.Scope() != target.ScopeGlobal ||
		globalDestination.HostPath() != wantGlobalPath {
		t.Fatalf("allowed global destination = %+v, error = %v", globalDestination, err)
	}
}

func TestMutationAuthorityRejectsScopePathContradictions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	paths := Paths{ManifestRoot: root}
	tests := []scopedDestinationForTest{
		{scope: target.ScopeProject, destination: outputtest.Parse(t, "~/.agent/config")},
		{scope: target.ScopeGlobal, destination: outputtest.Parse(t, ".agent/config")},
		{scope: target.ScopeGlobal},
		{destination: outputtest.Parse(t, ".agent/config")},
	}
	for _, facts := range tests {
		candidate, err := captureScopedDestinationsForTest(
			paths,
			[]scopedDestinationForTest{facts},
			emptyRemovalDemandsForTest(),
			nil,
			destinationResolver(paths),
			testFilesystem(),
		)
		if candidate != nil {
			_ = candidate.close()
		}
		if err == nil {
			t.Fatalf("newMutationAuthorityWithProjectionEffects(%+v) succeeded, want error", facts)
		}
	}
}

func TestMutationAuthorityRetainsProjectRootForNonHostJournalEntry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	effect := recordedPathAuthorityEffectForTest(t, target.ScopeProject, outputtest.Parse(t, "AGENTS.md"))
	paths := Paths{ManifestRoot: root}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		[]effectpreparation.ManagedPathEffect{effect},
		nil,
		emptyRemovalDemandsForTest(),
		nil,
		destinationResolver(paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("newMutationAuthorityWithProjectionEffects returned error: %v", err)
	}
	defer authority.close()
	if authority.capturedRoot == nil {
		t.Fatal("non-host project journal entry did not retain project root witness")
	}
}

func TestMutationAuthorityBindsGlobalRootForNonHostJournalEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	destination := outputtest.Parse(t, "~/.codex/config.toml")
	effect := recordedPathAuthorityEffectForTest(t, target.ScopeGlobal, destination)
	paths := Paths{}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		[]effectpreparation.ManagedPathEffect{effect},
		nil,
		emptyRemovalDemandsForTest(),
		nil,
		destinationResolver(paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("newMutationAuthorityWithProjectionEffects returned error: %v", err)
	}
	defer authority.close()

	resolved, err := authority.destinations.Resolve(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("resolveBoundDestination returned error: %v", err)
	}
	if !resolved.IsRooted() {
		t.Fatal("non-host global journal entry did not retain global root authority")
	}
}

func TestMutationAuthorityHasNoUnboundedBindingFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	paths := Paths{DataDir: filepath.Join(home, ".local", "share", "daem")}
	authority, err := captureMutationAuthority(
		paths,
		false,
		nil,
		destinationResolver(paths),
		testFilesystem(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.close()
	for authority.physicalWorkBudget.AdmitPathComponents(physicalwork.MaximumPhysicalPathDepth) == nil {
	}

	destination := outputtest.Parse(t, "~/.codex/config.toml")
	if err := authority.bindPhysicalAuthority(
		target.ScopeGlobal,
		destination,
		[]target.Target{target.TargetCodex},
	); err == nil || !strings.Contains(err.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("global binding error = %v, want physical-budget refusal", err)
	}
	authority.ownershipRegistryBinder = bindNilOwnershipRegistryStore
	if err := authority.bindOwnershipRegistry(
		filepath.Join(paths.DataDir, "ownership", "claims.json"),
	); err == nil || !strings.Contains(err.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("ownership binding error = %v, want physical-budget refusal", err)
	}
}

func TestRetainedRecoveryAuthoritiesFollowExecutionPhaseBudget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	paths := Paths{DataDir: filepath.Join(home, ".local", "share", "daem")}
	authority, err := captureMutationAuthority(
		paths,
		false,
		nil,
		destinationResolver(paths),
		testFilesystem(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.close()
	destination := outputtest.Parse(t, "~/.codex/config.toml")
	if err := authority.bindPhysicalAuthority(
		target.ScopeGlobal,
		destination,
		[]target.Target{target.TargetCodex},
	); err != nil {
		t.Fatal(err)
	}
	authority.ownershipRegistryBinder = ownershipstore.BindRooted
	if err := authority.bindOwnershipRegistry(
		filepath.Join(paths.DataDir, "ownership", "claims.json"),
	); err != nil {
		t.Fatal(err)
	}
	if err := authority.physicalWorkBudget.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	beginGeneralRecoveryExecutionForTest(t, authority)
	for authority.generalExecutionWorkBudget.AdmitPathComponents(physicalwork.MaximumPhysicalPathDepth) == nil {
	}
	for authority.generalExecutionWorkBudget.AdmitPathComponents(1) == nil {
	}
	for authority.generalTraversalPhase.AdmitPathComponents(physicalwork.MaximumPhysicalPathDepth) == nil {
	}
	for authority.generalTraversalPhase.AdmitPathComponents(1) == nil {
	}

	bound, err := authority.destinations.Resolve(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatal(err)
	}
	if capability, err := authority.destinations.Acquire(bound); err == nil {
		_ = capability.Close()
		t.Fatal("retained global destination escaped exhausted execution budget")
	}
	registry, err := authority.rootedOwnershipRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Load(t.Context()); err == nil {
		t.Fatal("retained ownership registry escaped exhausted execution budget")
	}
}
