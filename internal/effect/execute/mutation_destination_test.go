package execute

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

type phaseBudgetProbe struct {
	remaining int
	admitted  int
}

func (budget *phaseBudgetProbe) AdmitPathComponents(count int) error {
	if count < 0 || count > budget.remaining {
		return errors.New("physical traversal budget exhausted")
	}
	budget.remaining -= count
	budget.admitted += count
	return nil
}

func TestPhysicalTraversalPhaseRoutesRetainedAuthorityToExecutionBudget(t *testing.T) {
	planning := &phaseBudgetProbe{remaining: 10}
	phase, err := newPhysicalTraversalPhase(planning)
	if err != nil {
		t.Fatal(err)
	}
	if err := phase.AdmitPathComponents(3); err != nil {
		t.Fatal(err)
	}
	execution := &phaseBudgetProbe{remaining: 10}
	if err := phase.advance(execution); err != nil {
		t.Fatal(err)
	}
	if err := phase.AdmitPathComponents(4); err != nil {
		t.Fatal(err)
	}
	if planning.admitted != 3 || execution.admitted != 4 {
		t.Fatalf(
			"phase routing admitted planning=%d execution=%d, want 3 and 4",
			planning.admitted,
			execution.admitted,
		)
	}
	if err := phase.advance(&phaseBudgetProbe{remaining: 10}); err == nil {
		t.Fatal("physical traversal phase advanced twice")
	}
}

func TestMutationAuthoritySeparatesProjectAndGlobalDestinations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	dataDir := t.TempDir()
	allowedGlobalPath := filepath.Join(dataDir, "managed")
	t.Setenv("HOME", dataDir)
	t.Setenv("USERPROFILE", dataDir)
	effects := []ManagedPathEffect{
		{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
			scope: target.ScopeProject, destination: outputtest.Parse(t, ".agents/skill"),
		}}},
		{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
			scope: target.ScopeGlobal, destination: outputtest.Parse(t, "~/managed"),
		}}},
	}
	paths := Paths{ManifestRoot: root, DataDir: dataDir}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		effects,
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

	projectDestination, err := authority.resolveBoundDestination(
		target.ScopeProject,
		outputtest.Parse(t, ".agents/skill"),
	)
	if err != nil {
		t.Fatalf("resolve project destination: %v", err)
	}
	wantProjectPath := filepath.Join(authority.projectAuthority.PhysicalRoot(), ".agents", "skill")
	if !projectDestination.isRooted() || projectDestination.scope != target.ScopeProject ||
		projectDestination.hostPath != wantProjectPath {
		t.Fatalf("project destination = %+v", projectDestination)
	}
	capability, err := authority.acquire(projectDestination)
	if err != nil {
		t.Fatalf("acquire project destination: %v", err)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close project capability: %v", err)
	}

	globalDestination, err := authority.resolveBoundDestination(
		target.ScopeGlobal,
		outputtest.Parse(t, "~/managed"),
	)
	wantGlobalPath, canonicalErr := mutation.CanonicalDirectoryEntryPath(allowedGlobalPath)
	if canonicalErr != nil {
		t.Fatalf("canonicalize allowed global path: %v", canonicalErr)
	}
	if err != nil || !globalDestination.isRooted() || globalDestination.scope != target.ScopeGlobal ||
		globalDestination.hostPath != wantGlobalPath {
		t.Fatalf("allowed global destination = %+v, error = %v", globalDestination, err)
	}
}

func TestMutationAuthorityRejectsScopePathContradictions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	paths := Paths{ManifestRoot: root}
	tests := []managedPathEffectFacts{
		{scope: target.ScopeProject, destination: outputtest.Parse(t, "~/.agent/config")},
		{scope: target.ScopeGlobal, destination: outputtest.Parse(t, ".agent/config")},
		{scope: target.ScopeGlobal},
		{destination: outputtest.Parse(t, ".agent/config")},
	}
	for _, facts := range tests {
		effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: facts}}
		candidate, err := newMutationAuthorityWithProjectionEffects(
			paths,
			[]ManagedPathEffect{effect},
			nil,
			emptyRemovalDemandsForTest(),
			nil,
			destinationResolver(paths),
			testFilesystem(),
			nil,
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
	effect := ManagedPathEffect{record: &managedPathRecordEffect{facts: managedPathEffectFacts{
		scope: target.ScopeProject, destination: outputtest.Parse(t, "AGENTS.md"),
	}}}
	paths := Paths{ManifestRoot: root}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		[]ManagedPathEffect{effect},
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
	effect := ManagedPathEffect{record: &managedPathRecordEffect{facts: managedPathEffectFacts{
		scope: target.ScopeGlobal, destination: destination,
	}}}
	paths := Paths{}
	authority, err := newMutationAuthorityWithProjectionEffects(
		paths,
		[]ManagedPathEffect{effect},
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

	resolved, err := authority.resolveBoundDestination(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatalf("resolveBoundDestination returned error: %v", err)
	}
	if !resolved.isRooted() {
		t.Fatal("non-host global journal entry did not retain global root authority")
	}

	for _, mutate := range []func(*mutationDestination){
		func(candidate *mutationDestination) { candidate.scope = target.Scope("") },
		func(candidate *mutationDestination) { candidate.logical = output.Destination{} },
		func(candidate *mutationDestination) { candidate.hostPath = filepath.Join(home, "different") },
	} {
		candidate := resolved
		mutate(&candidate)
		if candidate.isRooted() {
			t.Fatalf("partially initialized mutation destination remained valid: %+v", candidate)
		}
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
	for authority.physicalWorkBudget.AdmitPathComponents(recovery.MaximumPhysicalPathDepth) == nil {
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
	for authority.generalExecutionWorkBudget.AdmitPathComponents(recovery.MaximumPhysicalPathDepth) == nil {
	}
	for authority.generalExecutionWorkBudget.AdmitPathComponents(1) == nil {
	}
	for authority.generalTraversalPhase.AdmitPathComponents(recovery.MaximumPhysicalPathDepth) == nil {
	}
	for authority.generalTraversalPhase.AdmitPathComponents(1) == nil {
	}

	bound, err := authority.resolveBoundDestination(target.ScopeGlobal, destination)
	if err != nil {
		t.Fatal(err)
	}
	if capability, err := authority.acquire(bound); err == nil {
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
