package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/output/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/readiness"
	"github.com/isty2e/daem/test/outputtest"
)

func run(
	t *testing.T,
	ctx context.Context,
	paths daempaths.Paths,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
	assessment readiness.Assessment,
) (runResult, error) {
	return runWithOptions(
		ctx,
		paths,
		environment,
		locked,
		selection,
		assessment,
		applyDelegateRunOptions(t, paths, runOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				LookupEnv: func(string) (string, bool) { return "test-value", true },
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					return subprocess.CommandResult{}
				},
			}),
		}),
	)
}

func assessmentWithDelegates(
	t testing.TB,
	assessment readiness.Assessment,
	context reconcile.OperationContext,
	actions []reconcile.DelegateAction,
) readiness.Assessment {
	t.Helper()
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      context,
		ManagedPaths: assessment.Reconciliation.ManagedPaths(),
		Aggregates:   assessment.Reconciliation.Aggregates(),
		Relations:    assessment.Reconciliation.Relations(),
		Delegates:    actions,
	})
	if err != nil {
		t.Fatalf("assemble test reconciliation result: %v", err)
	}
	assessment.Reconciliation = result
	return assessment
}

func captureApplyProjectRootForTest(t *testing.T, planned *commandPlan) {
	t.Helper()
	root, captureErr := captureProjectRootAuthorityBeforeLoad(planned.context.Paths)
	if err := retainProjectRootAuthority(planned, root, captureErr); err != nil {
		t.Fatalf("capture and retain project root: %v", err)
	}
}

func testApplyExecutionGuard(
	t *testing.T,
	paths daempaths.Paths,
) applyExecutionGuard {
	t.Helper()
	manifestPath := paths.ManifestPath
	lockfilePath := paths.LockfilePath
	if manifestPath == "" {
		manifestPath = lockfilePath
	}
	if lockfilePath == "" {
		lockfilePath = manifestPath
	}
	if manifestPath == "" {
		manifestPath = filepath.Join(t.TempDir(), "stable-declaration")
		lockfilePath = manifestPath
	}
	revisions, err := captureDeclarationRevisions(
		t.Context(),
		manifestPath,
		lockfilePath,
	)
	if err != nil {
		t.Fatalf("capture test apply execution guard: %v", err)
	}
	return newApplyExecutionGuard(revisions, false)
}

func applyTestPaths(t *testing.T, root string) daempaths.Paths {
	t.Helper()

	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	return paths
}

func buildManagedApplyAssessment(
	t *testing.T,
	paths daempaths.Paths,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
	manageUnmanagedMatches bool,
) readiness.Assessment {
	t.Helper()
	result, err := readiness.Assess(context.Background(), readiness.Input{
		Context:                 reconcile.ContextApply,
		Paths:                   paths,
		Resolver:                liveobserve.DestinationResolver(hostpath.NewResolver(paths.ManifestRoot).Resolve),
		Environment:             environment,
		Lockfile:                locked,
		Selection:               selection,
		ManageUnmanagedMatches:  manageUnmanagedMatches,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		t.Fatalf("build managed apply plan: %v", err)
	}
	return result
}

func buildAggregateApplyAssessment(
	t *testing.T,
	paths daempaths.Paths,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
	manageUnmanagedMatches bool,
) readiness.Assessment {
	t.Helper()
	result, err := readiness.Assess(context.Background(), readiness.Input{
		Context: reconcile.ContextApply,
		Paths:   paths,
		Resolver: liveobserve.DestinationResolver(
			hostpath.NewResolverWithManagedDataRoot(
				paths.ManifestRoot,
				paths.DataDir,
			).Resolve,
		),
		Environment:             environment,
		Lockfile:                locked,
		Selection:               selection,
		ManageUnmanagedMatches:  manageUnmanagedMatches,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		t.Fatalf("build aggregate apply plan: %v", err)
	}
	return result
}

func applyInstructionConfig(t *testing.T, name string, sourcePath string, renderTo string, targets ...targetpkg.Target) desired.Environment {
	t.Helper()
	renderings := make(map[targetpkg.Target]instructions.Rendering)
	if renderTo != "" {
		renderings[targetpkg.TargetCodex] = desiredtest.Rendering(t, renderTo, instructions.RenderModeCopy)
	}
	return desiredtest.Environment(t, desired.Spec{
		Targets:  targets,
		Defaults: desiredtest.Defaults(t, targetpkg.ScopeProject, skill.InstallModeCopy),
		Instructions: []instructions.Instructions{
			desiredtest.Instructions(t, instructions.Spec{
				Name:       name,
				Source:     sourcetest.Local(t, sourcePath, sourcepkg.LocalSourceModeVendor),
				Targets:    targets,
				Scope:      targetpkg.ScopeProject,
				Renderings: renderings,
			}),
		},
	})
}

func applyEmptyEnvironment(t *testing.T, selected targetpkg.Target) desired.Environment {
	t.Helper()
	return desiredtest.Environment(t, desired.Spec{
		Targets:  []targetpkg.Target{selected},
		Defaults: desiredtest.Defaults(t, targetpkg.ScopeProject, skill.InstallModeCopy),
	})
}

func applyInstructionLockfile(
	t *testing.T,
	name string,
	sourceID string,
	contentHash string,
	targets ...targetpkg.Target,
) lock.File {
	t.Helper()
	if len(targets) == 0 {
		targets = []targetpkg.Target{targetpkg.TargetCodex}
	}
	value := desiredtest.Instructions(t, instructions.Spec{
		Name:    name,
		Source:  sourcetest.Local(t, "instructions/"+name+".md", sourcepkg.LocalSourceModeVendor),
		Targets: targets,
		Scope:   targetpkg.ScopeProject,
	})
	fileUse, err := lock.NewExactFileUse(value.Scope(), false)
	if err != nil {
		t.Fatalf("build Instructions exact file use: %v", err)
	}
	contract := snapshottest.ExactSupplyContract(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindInstructions,
		Name:         name,
		SourceID:     artifact.SourceID(sourceID),
		ArtifactKind: artifact.ArtifactKindFile,
		ContentHash:  artifact.ContentHash(contentHash),
		ExactFileUse: &fileUse,
	})
	projections, err := refine.InstructionsPathProjections(value)
	if err != nil {
		t.Fatalf("build Instructions projection fixtures: %v", err)
	}
	return snapshottest.File(t, append([]lock.LockedSubjectContract{contract}, projections...)...)
}

func applySelection(t *testing.T, requested []string) targetselection.Selection {
	t.Helper()

	selection, err := targetselection.ForAvailableTargets(
		[]targetpkg.Target{targetpkg.TargetCodex},
		requested,
	)
	if err != nil {
		t.Fatalf("build selection: %v", err)
	}
	return selection
}

func writeApplyFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func writeApplyManifestFile(t *testing.T, path string) {
	t.Helper()

	writeApplyFile(t, path, `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
}

func writeApplyLockfile(t *testing.T, path string, file lock.File) {
	t.Helper()

	content, err := lockfile.Marshal(file)
	if err != nil {
		t.Fatalf("marshal lockfile: %v", err)
	}
	writeApplyFile(t, path, string(content))
}

func writeApplyStatefile(t *testing.T, path string, snapshot durable.Snapshot) {
	t.Helper()

	content, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal statefile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create statefile directory: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write statefile: %v", err)
	}
}

func hashApplyPath(t *testing.T, path string) string {
	t.Helper()

	contentHash, _, err := access.HashPath(context.Background(), path)
	if err != nil {
		t.Fatalf("hash path: %v", err)
	}
	return string(contentHash)
}

func assertApplyFileContent(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != want {
		t.Fatalf("%s content = %q, want %q", path, content, want)
	}
}

func applyStateSnapshot(t *testing.T, input durable.SnapshotInput) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(input)
	if err != nil {
		t.Fatalf("build durable snapshot: %v", err)
	}
	return snapshot
}

func assertApplyStateResource(t *testing.T, snapshot durable.Snapshot, name string, target string, scope string, path string, contentHash string) {
	t.Helper()
	for _, stateResource := range snapshot.ManagedPaths() {
		subject := stateResource.Subject()
		entityID, entityBacked := topologyprojection.EntityID(subject)
		if !entityBacked || entityID.Kind() != entity.KindInstructions || entityID.Name() != name ||
			string(stateResource.Scope()) != scope || stateResource.Destination().String() != path {
			continue
		}
		consumers := stateResource.ConsumerTargets()
		if len(consumers) != 1 || string(consumers[0]) != target ||
			stateResource.ContentKind() != realization.PathProjectionFile || string(stateResource.ContentHash()) != contentHash {
			t.Fatalf("managed Instructions state = %#v", stateResource)
		}
		return
	}
	t.Fatalf("managed Instructions state %s/%s/%s/%s not found in %#v", name, target, scope, path, snapshot.ManagedPaths())
}

func assertApplyStateResourceMissing(t *testing.T, snapshot durable.Snapshot, name string, target string, scope string, path string) {
	t.Helper()
	for _, stateResource := range snapshot.ManagedPaths() {
		subject := stateResource.Subject()
		entityID, entityBacked := topologyprojection.EntityID(subject)
		if entityBacked && entityID.Kind() == entity.KindInstructions && entityID.Name() == name &&
			string(stateResource.Scope()) == scope && stateResource.Destination().String() == path {
			t.Fatalf("managed Instructions state unexpectedly present in %#v", snapshot.ManagedPaths())
		}
	}
}

func applyInstructionPathState(
	t *testing.T,
	name string,
	consumerValues []string,
	scopeValue string,
	destination string,
	contentHash string,
) durable.ManagedPathState {
	t.Helper()
	consumers := make([]targetpkg.Target, 0, len(consumerValues))
	for _, value := range consumerValues {
		consumer, err := targetpkg.ParseTarget(value)
		if err != nil {
			t.Fatalf("parse Instructions state target: %v", err)
		}
		consumers = append(consumers, consumer)
	}
	scope, err := targetpkg.ParseScope(scopeValue)
	if err != nil {
		t.Fatalf("parse Instructions state scope: %v", err)
	}
	destinationValue := outputtest.Parse(t, destination)
	var placement profile.SelectedManagedPathPlacement
	for index, consumer := range consumers {
		candidate, err := profile.ManagedFilePlacementFor(entity.KindInstructions, consumer, scope, destinationValue)
		if err != nil {
			t.Fatalf("derive Instructions state placement: %v", err)
		}
		if index == 0 {
			placement = candidate
			continue
		}
		placement, err = profile.MergeManagedPathPlacements(placement, candidate)
		if err != nil {
			t.Fatalf("merge Instructions state placement: %v", err)
		}
	}
	entityID, err := entity.New(entity.KindInstructions, name)
	if err != nil {
		t.Fatalf("build Instructions state entity: %v", err)
	}
	subject, err := topologyprojection.Subject(entityID, placement.ID())
	if err != nil {
		t.Fatalf("build Instructions state subject: %v", err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		t.Fatalf("resolve Instructions state write route: %v", err)
	}
	spec, err := placement.Realize(destinationValue, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		t.Fatalf("realize Instructions state placement: %v", err)
	}
	projection, ok := spec.ManagedPathProjection()
	if !ok {
		t.Fatal("Instructions state placement did not produce a managed-path projection")
	}
	state, err := durable.NewManagedPathState(
		subject,
		consumers,
		scope,
		destinationValue,
		artifact.ContentHash(contentHash),
		projection.ContentKind(),
		projection.PermissionPolicy(),
		0,
	)
	if err != nil {
		t.Fatalf("build Instructions managed path state: %v", err)
	}
	return state
}

func assertNoApplyRecoveryArtifacts(t *testing.T, paths daempaths.Paths) {
	t.Helper()

	entries, err := os.ReadDir(paths.RecoveryDir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read recovery directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("recovery artifacts remain: %#v", entries)
	}
}
