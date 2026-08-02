package readiness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestBuildAssessmentReportsMissingManagedPathLock(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, environment, "codex")
	result, err := buildAssessment(
		context.Background(),
		resolveTestPaths(t, manifestPath),
		testDestinationResolver(tempDir),
		environment,
		lock.File{Version: lock.CurrentVersion},
		selection,
		testSelectedTargets(t, selection),
		nil,
		durable.EmptySnapshot(),
		durablecarrier.EmptyGlobalCarrierClaims(),
		false,
		aggregatecodec.Catalog(),
		hookcodec.CanonicalHookContribution,
		mcpcodec.CanonicalMCPBindingContribution,
		reconcile.ContextInspect,
		nil,
	)
	if err != nil {
		t.Fatalf("buildAssessment returned error: %v", err)
	}
	if len(result.Reconciliation.ManagedPaths()) != 1 {
		t.Fatalf("reconciliation = %#v, want one managed-path missing lock decision", result.Reconciliation)
	}
	decision := result.Reconciliation.ManagedPaths()[0]
	if !decision.IsBlocked() || decision.Reason() != reconcile.ReasonMissingLock {
		t.Fatalf("decision = %#v, want missing lock block", decision)
	}
}

func TestAssessReportsMissingManagedPathLock(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, environment, "codex")

	result, err := Assess(context.Background(), Input{
		Context:                 reconcile.ContextInspect,
		Paths:                   resolveTestPaths(t, manifestPath),
		Resolver:                testDestinationResolver(tempDir),
		Environment:             environment,
		Lockfile:                lock.File{Version: lock.CurrentVersion},
		Selection:               selection,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		t.Fatalf("Assess returned error: %v", err)
	}
	if len(result.Reconciliation.ManagedPaths()) != 1 {
		t.Fatalf("reconciliation = %#v, want one managed-path missing lock decision", result.Reconciliation)
	}
	decision := result.Reconciliation.ManagedPaths()[0]
	if !decision.IsBlocked() || decision.Reason() != reconcile.ReasonMissingLock {
		t.Fatalf("decision = %#v, want missing lock block", decision)
	}
}

func TestAssessUsesProvidedPersistenceEpochWithoutReloading(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempDir, "xdg-data"))
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "instructions/project.md", "project instructions\n")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	paths := resolveTestPaths(t, manifestPath)
	writeTestFile(t, tempDir, ".daem/state.json", "not a statefile")
	if err := os.MkdirAll(filepath.Dir(paths.CarrierClaimRegistryPath), 0o700); err != nil {
		t.Fatalf("MkdirAll carrier claim registry parent: %v", err)
	}
	if err := os.WriteFile(
		paths.CarrierClaimRegistryPath,
		[]byte("not a carrier claim registry"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile carrier claim registry: %v", err)
	}

	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, environment, "codex")
	epoch := NewPersistenceEpoch(
		durable.EmptySnapshot(),
		durablecarrier.EmptyGlobalCarrierClaims(),
	)
	result, err := Assess(context.Background(), Input{
		Context:                 reconcile.ContextInspect,
		Paths:                   paths,
		Resolver:                testDestinationResolver(tempDir),
		Environment:             environment,
		Lockfile:                lock.File{Version: lock.CurrentVersion},
		Selection:               selection,
		PersistenceEpoch:        &epoch,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		t.Fatalf("Assess with provided persistence epoch returned error: %v", err)
	}
	if !result.CurrentState.Equal(durable.EmptySnapshot()) {
		t.Fatalf("CurrentState = %#v, want provided empty state", result.CurrentState)
	}
	if !result.GlobalCarrierClaims.Equal(durablecarrier.EmptyGlobalCarrierClaims()) {
		t.Fatalf(
			"GlobalCarrierClaims = %#v, want provided empty claims",
			result.GlobalCarrierClaims,
		)
	}
}

func TestAssessObservesMissingClaudeInstalledRelation(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["claude-code"]
`)
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "context7",
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, "context7@market"),
	})
	locked, _ := snapshottest.ExtensionCarrierFile(t, value)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{target.TargetClaudeCode},
		[]string{"claude-code"},
	)
	if err != nil {
		t.Fatalf("targetselection.ForAvailableTargets: %v", err)
	}

	result, err := Assess(context.Background(), Input{
		Context:                 reconcile.ContextInspect,
		Paths:                   resolveTestPaths(t, manifestPath),
		Resolver:                testDestinationResolver(tempDir),
		Environment:             environment,
		Lockfile:                locked,
		Selection:               selection,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		t.Fatalf("Assess returned error: %v", err)
	}
	relations := result.Reconciliation.Relations()
	if len(relations) != 1 {
		t.Fatalf("relation actions = %#v, want one missing create action", relations)
	}
	action := relations[0]
	if action.Kind() != reconcile.ActionCreate ||
		action.CorrelationState() != observerelation.StateMissing ||
		action.BlocksOrdinaryApply() {
		t.Fatalf(
			"action = kind %q state %q blocks %t, want missing create",
			action.Kind(),
			action.CorrelationState(),
			action.BlocksOrdinaryApply(),
		)
	}
}

func TestManagedAggregateReadinessUsesFullHookAssetTopologyForPartialSelection(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "hooks/shared.sh", "#!/bin/sh\nexit 0\n")
	writeTestFile(t, tempDir, "hooks/claude-only.sh", "#!/bin/sh\nexit 0\n")
	writeTestFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex", "claude-code"]

[hook_asset.shared]
source = "hooks/shared.sh"
kind = "file"
scope = "project"
executable = true

[hook_asset.claude-only]
source = "hooks/claude-only.sh"
kind = "file"
scope = "project"
executable = true

[[hook]]
name = "shared"
event = "PreToolUse"
command = "{hook_file:shared} --check"
targets = ["codex", "claude-code"]
scope = "project"

[[hook]]
name = "claude-only"
event = "PreToolUse"
command = "{hook_file:claude-only} --check"
targets = ["claude-code"]
scope = "project"
`)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	paths := resolveTestPaths(t, manifestPath)
	sourceResolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	locked, err := lockbuild.BuildWithOptions(context.Background(), environment, sourceResolver, lockbuild.Options{
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	claudeOnlyDestination := hookAssetDestination(t, locked, "claude-only")
	selection := testSelection(t, environment, "codex")
	resolver := func(destination output.Destination) (string, error) {
		if destination == claudeOnlyDestination {
			return "", fmt.Errorf("resolved unselected-only HookAsset destination %q", destination)
		}
		return filepath.Join(tempDir, filepath.FromSlash(destination.String())), nil
	}

	inputs, err := buildManagedAggregatePlanningInputs(
		context.Background(),
		resolver,
		environment,
		locked.Locked,
		durable.EmptySnapshot(),
		selection,
		hookcodec.CanonicalHookContribution,
		mcpcodec.CanonicalMCPBindingContribution,
		aggregatecodec.Catalog(),
	)
	if err != nil {
		t.Fatalf("buildManagedAggregatePlanningInputs returned error: %v", err)
	}
	if len(inputs.evidence) != 1 {
		t.Fatalf("aggregate evidence count = %d, want only selected Codex document", len(inputs.evidence))
	}
}

func TestBuildProjectionDecisionsReportsMissingManagedPathLock(t *testing.T) {
	environment := parseTestManifest(t, `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	selection := testSelection(t, environment, "codex")
	managedPaths, _, err := buildProjectionDecisions(projectionPlanningInput{
		environment:     environment,
		selectedTargets: testSelectedTargets(t, selection),
	})
	if err != nil {
		t.Fatalf("BuildProjectionDecisions returned error: %v", err)
	}
	if len(managedPaths) != 1 {
		t.Fatalf("managed paths = %#v, want one missing lock decision", managedPaths)
	}
	decision := managedPaths[0]
	if !decision.IsBlocked() || decision.Reason() != reconcile.ReasonMissingLock {
		t.Fatalf("decision = %#v, want missing lock block", decision)
	}
}

func hookAssetDestination(
	t *testing.T,
	locked lock.File,
	assetName string,
) output.Destination {
	t.Helper()
	for _, contract := range locked.Locked.Subjects() {
		if contract.EntityID().Name() != assetName {
			continue
		}
		realization, realized := contract.Realization()
		if !realized {
			continue
		}
		projection, managedPath := realization.ManagedPathProjection()
		if managedPath {
			return projection.Destination()
		}
	}
	t.Fatalf("locked HookAsset %q has no managed path projection", assetName)
	return output.Destination{}
}

func parseTestManifest(t *testing.T, content string) desired.Environment {
	t.Helper()

	environment, err := declarationmanifest.Decode([]byte(content))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	return environment
}

func testSelection(t *testing.T, environment desired.Environment, requested ...string) targetselection.Selection {
	t.Helper()

	availableTargets := fromEnvironment(environment)
	selection, err := targetselection.ForAvailableTargets(availableTargets, requested)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}
	return selection
}

func testSelectedTargets(t *testing.T, selection targetselection.Selection) reconcile.SelectedTargets {
	t.Helper()
	selected, err := reconcile.NewSelectedTargets(selection.Targets())
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	return selected
}

func resolveTestPaths(t *testing.T, manifestPath string) daempaths.Paths {
	t.Helper()

	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("paths.Resolve returned error: %v", err)
	}
	return paths
}

func writeTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q returned error: %v", path, err)
	}
	return content
}

func testDestinationResolver(root string) liveobserve.DestinationResolver {
	return func(destination output.Destination) (string, error) {
		return filepath.Join(root, filepath.FromSlash(destination.String())), nil
	}
}
