package readiness

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/output/hostpath"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/reconcile"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
)

func TestAssessMCPDelegateDependenciesFollowPhysicalDocument(t *testing.T) {
	const filesystemConflict = `{"mcpServers":{"filesystem":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}}}}`
	tests := []struct {
		name            string
		config          string
		global          bool
		managedContext7 bool
		blocked         map[string]bool
	}{
		{name: "all missing"},
		{name: "mixed", config: filesystemConflict, blocked: map[string]bool{"context7": true, "filesystem": true}},
		{name: "first subject conflict", config: `{"mcpServers":{"context7":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}}}}`, blocked: map[string]bool{"context7": true, "filesystem": true}},
		{name: "all conflicting", config: `{"mcpServers":{"context7":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}},"filesystem":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}}}}`, blocked: map[string]bool{"context7": true, "filesystem": true}},
		{name: "no-op sibling with conflict", config: `{"mcpServers":{"context7":{"type":"stdio","command":"must-not-run-daem-test","args":[],"env":{}},"filesystem":{"type":"stdio","command":"manual-not-executed","args":[],"env":{}}}}`, managedContext7: true, blocked: map[string]bool{"context7": true, "filesystem": true}},
		{name: "distinct documents", config: filesystemConflict, global: true, blocked: map[string]bool{"filesystem": true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
			t.Setenv("PATH", "")

			scope := "project"
			configRoot, configPath := root, ".mcp.json"
			if test.global {
				scope = "global"
				configRoot, configPath = home, ".claude.json"
			}
			if test.config != "" {
				writeTestFile(t, configRoot, configPath, test.config)
			}

			manifest := fmt.Sprintf(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "must-not-run-daem-test"
scope = "project"

[[mcp_server]]
name = "filesystem"
transport = "stdio"
command = "must-not-run-daem-test"
scope = %q
`, scope)
			writeTestFile(t, root, "daem.toml", manifest)
			environment := parseTestManifest(t, manifest)
			paths := resolveTestPaths(t, filepath.Join(root, "daem.toml"))
			selection := testSelection(t, environment, "claude-code")

			resolver, err := sourceresolution.NewResolver(paths)
			if err != nil {
				t.Fatal(err)
			}
			locked, err := lockbuild.BuildWithOptions(context.Background(), environment, resolver, lockbuild.Options{
				HookContributionEncoder: hookcodec.CanonicalHookContribution,
				MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
			})
			if err != nil {
				t.Fatal(err)
			}

			currentState := durable.EmptySnapshot()
			if test.managedContext7 {
				for _, contract := range locked.Locked.Subjects() {
					if contract.SubjectID().Key() == "context7" {
						currentState = aggregateSnapshot(t, contract)
					}
				}
				if len(currentState.ManagedAggregates()) != 1 {
					t.Fatal("fixture requires one managed context7 baseline")
				}
			}
			epoch := NewPersistenceEpoch(currentState, durablecarrier.EmptyGlobalCarrierClaims())
			for _, operation := range []reconcile.OperationContext{reconcile.ContextInspect, reconcile.ContextDryRun, reconcile.ContextApply} {
				t.Run(string(operation), func(t *testing.T) {
					assessment, err := Assess(context.Background(), Input{
						Context: operation, Paths: paths, Environment: environment, Lockfile: locked,
						Resolver:  liveobserve.DestinationResolver(hostpath.NewResolver(root).Resolve),
						Selection: selection, PersistenceEpoch: &epoch, Codecs: aggregatecodec.Catalog(),
						HookContributionEncoder: hookcodec.CanonicalHookContribution,
						MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
					})
					if err != nil {
						t.Fatalf("Assess must return a complete plan: %v", err)
					}

					if test.managedContext7 {
						noOpSibling := false
						for _, decision := range assessment.Reconciliation.Decisions() {
							projection, present := decision.Aggregate()
							if !present || projection.Subject().Key() != "context7" {
								continue
							}
							if !projection.IsNoOp() || projection.MutatesHost() || projection.MutatesState() {
								t.Fatalf("managed context7 projection = %#v, want no-op without mutations", projection)
							}
							noOpSibling = true
						}
						if !noOpSibling {
							t.Fatal("missing managed no-op sibling")
						}
					}

					blocks := make(map[string]bool)
					for _, block := range blockedProjectionDependencies(assessment.Reconciliation.ManagedPaths(), assessment.Reconciliation.Aggregates()) {
						blocks[block.Subject.Key()] = true
					}
					if !maps.Equal(blocks, test.blocked) {
						t.Errorf("blocked prerequisites = %v, want %v", blocks, test.blocked)
					}

					actions := assessment.Reconciliation.Delegates()
					if operation == reconcile.ContextInspect {
						if len(actions) != 0 {
							t.Fatalf("inspect delegates = %v", actions)
						}
						return
					}

					wantDelegates := 2
					if test.global {
						wantDelegates = 1
					}
					if len(actions) != wantDelegates {
						t.Fatalf("delegate count = %d, want %d admitted routes", len(actions), wantDelegates)
					}

					for _, action := range actions {
						blocked := test.blocked[action.Subject().Key()]
						want := reconcile.DelegateScheduled
						if operation == reconcile.ContextDryRun {
							want = reconcile.DelegateSkipped
						} else if blocked {
							want = reconcile.DelegateBlocked
						}
						if action.Disposition() != want || action.SchedulesAttempt() != (want == reconcile.DelegateScheduled) {
							t.Fatalf("delegate %s disposition=%s schedules=%t, want %s", action.Subject(), action.Disposition(), action.SchedulesAttempt(), want)
						}

						blocks := 0
						for _, risk := range action.Risks() {
							if risk.Code != reconcile.DelegateRiskPreconditionBlocked {
								continue
							}
							blocks++
							if risk.Severity != reconcile.DelegateRiskBlock || risk.Subject != "projection:"+action.Subject().String() {
								t.Fatalf("delegate %s prerequisite risk = %#v", action.Subject(), risk)
							}
						}

						if (blocks == 1) != blocked || blocks > 1 {
							t.Fatalf("delegate %s prerequisite risks=%d, want blocked=%t", action.Subject(), blocks, blocked)
						}
					}
				})
			}

			if test.config != "" && string(mustReadTestFile(t, filepath.Join(configRoot, configPath))) != test.config {
				t.Fatal("assessment changed the unmanaged configuration")
			}
			if _, err := os.Stat(paths.StatefilePath); !os.IsNotExist(err) {
				t.Fatalf("assessment created state or failed to establish its absence: %v", err)
			}
		})
	}
}
