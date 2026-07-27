package ownership

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestBuildCanonicalizesAliasesAndFindsOverlappingClaim(t *testing.T) {
	root := canonicalRoot(t)
	realHome := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(realHome, ".codex"), 0o700); err != nil {
		t.Fatalf("create home: %v", err)
	}
	aliasHome := filepath.Join(root, "home-alias")
	if err := os.Symlink(realHome, aliasHome); err != nil {
		t.Skipf("create home alias: %v", err)
	}
	paths := testPaths(root)
	selection, _ := targetselection.ForDiagnostics([]string{"codex"})
	physical := filepath.Join(realHome, ".codex", "config.toml")
	canonical, err := mutation.CanonicalDirectoryEntryKey(physical)
	if err != nil {
		t.Fatalf("canonicalize fixture: %v", err)
	}
	parentAddress, _ := outputownership.NewManagedAddress(canonical, "/mcp_servers")
	foreign, _ := stateauthority.New(filepath.Join(root, "foreign", "state.json"), filepath.Join(root, "foreign.toml"))
	claim, _ := outputownership.NewActiveClaim(parentAddress, foreign)
	registry, _ := outputownership.NewRegistry([]outputownership.Claim{claim})
	operations, admitted := mcpcodec.ImplementedMCPPlacementOperationsForID(
		aggregate.MCPPlacementCodexGlobal,
	)
	if !admitted {
		t.Fatal("Codex global MCP placement is not admitted")
	}
	contract, err := operations.Placement().ProjectionContract("alpha")
	if err != nil {
		t.Fatalf("ProjectionContract returned error: %v", err)
	}

	result, err := Build(Input{
		Paths: paths,
		Resolver: func(output.Destination) (string, error) {
			return filepath.Join(aliasHome, ".codex", "config.toml"), nil
		},
		Aggregates: []aggregate.ProjectionAddress{contract.Address()},
		Selection:  selection,
		Registry:   registry,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(result.Observations) != 1 {
		t.Fatalf("observation count = %d, want 1", len(result.Observations))
	}
	observation := result.Observations[0]
	if observation.Address.Path() != canonical || observation.Address.ContentPath() != "/mcp_servers/alpha" {
		t.Fatalf("unexpected canonical address: %#v", observation.Address)
	}
	observedClaim, present := observation.Claim.Get()
	if !present || !observedClaim.Equal(claim) {
		t.Fatal("overlapping parent claim was not observed")
	}
}

func TestBuildDeduplicatesManagedPathAndStateAndFiltersSelection(t *testing.T) {
	root := canonicalRoot(t)
	paths := testPaths(root)
	selection, _ := targetselection.ForDiagnostics([]string{"codex"})
	destination := outputtest.Parse(t, "~/.agents/skills/reviewer")
	resolverCalls := 0
	result, err := Build(Input{
		Paths: paths,
		Resolver: func(output.Destination) (string, error) {
			resolverCalls++
			return filepath.Join(root, "home", "reviewer"), nil
		},
		ManagedPaths: []ManagedPathInput{{
			Scope: target.ScopeGlobal, Destination: destination, ConsumerTargets: []target.Target{target.TargetCodex},
		}},
		StatePaths: []durable.ManagedPathState{
			testManagedPathState(t, "selected", target.TargetCodex, target.ScopeGlobal, destination),
			testManagedPathState(t, "other-target", target.TargetClaudeCode, target.ScopeGlobal, outputtest.Parse(t, "~/.claude/ignored")),
			testManagedPathState(t, "project", target.TargetCodex, target.ScopeProject, outputtest.Parse(t, "project")),
		},
		Selection: selection,
		Registry:  outputownership.EmptyRegistry(),
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(result.Observations) != 1 || resolverCalls != 1 {
		t.Fatalf("observations=%d resolverCalls=%d, want 1/1", len(result.Observations), resolverCalls)
	}
}

func testManagedPathState(
	t *testing.T,
	name string,
	selected target.Target,
	scope target.Scope,
	destination output.Destination,
) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "skill."+string(scope)+"."+string(selected))
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{selected},
		scope,
		destination,
		artifact.HashFileContent([]byte(name)),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestBuildObservesSelectedGlobalAggregateProjectionAtExactContentPath(t *testing.T) {
	root := canonicalRoot(t)
	paths := testPaths(root)
	selection, _ := targetselection.ForDiagnostics([]string{"claude-code"})
	operations, admitted := mcpcodec.ImplementedMCPPlacementOperationsForID(
		aggregate.MCPPlacementClaudeGlobal,
	)
	if !admitted {
		t.Fatal("Claude global MCP placement is not admitted")
	}
	contract, err := operations.Placement().ProjectionContract("context7")
	if err != nil {
		t.Fatalf("ProjectionContract returned error: %v", err)
	}
	physical := filepath.Join(root, "home", ".claude.json")
	canonical, err := mutation.CanonicalDirectoryEntryKey(physical)
	if err != nil {
		t.Fatalf("canonicalize aggregate fixture: %v", err)
	}
	resolverCalls := 0
	result, err := Build(Input{
		Paths: paths,
		Resolver: func(destination output.Destination) (string, error) {
			resolverCalls++
			if destination != contract.Address().Document().AggregateRoot() {
				t.Fatalf("resolver destination = %q, want aggregate root", destination)
			}
			return physical, nil
		},
		Aggregates: []aggregate.ProjectionAddress{contract.Address()},
		Selection:  selection,
		Registry:   outputownership.EmptyRegistry(),
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(result.Observations) != 1 || resolverCalls != 1 {
		t.Fatalf("observations=%d resolverCalls=%d, want 1/1", len(result.Observations), resolverCalls)
	}
	observation := result.Observations[0]
	if observation.Address.Path() != canonical ||
		observation.Address.ContentPath() != string(contract.Address().ContentPath()) {
		t.Fatalf("aggregate ownership address = %#v, want exact projection address", observation.Address)
	}
}

func testPaths(root string) daempaths.Paths {
	return daempaths.Paths{
		ManifestPath:  filepath.Join(root, "project", "daem.toml"),
		StatefilePath: filepath.Join(root, "project", ".daem", "state.json"),
	}
}

func canonicalRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize root: %v", err)
	}
	return root
}
