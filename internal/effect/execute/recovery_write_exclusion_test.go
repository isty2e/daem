package execute

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestOrdinaryProjectRestorationExcludesForeignClaimWithoutRecordedOwnership(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		name := "active"
		if reserved {
			name = "reserved"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec("codex-project", target.TargetCodex, "AGENTS.md"))
			if fixture.plan.RequiresOwnershipSettlement() || !fixture.plan.RequiresHostWriteExclusion() {
				t.Fatal("fixture does not distinguish restoration reads from recorded ownership work")
			}
			claim := seedRestorationForeignClaim(t, fixture.paths, filepath.Join(fixture.projectRoot, "AGENTS.md"), output.WholePathFootprint(), reserved)
			_, err := journal.LoadActivePlanWithOptions(t.Context(), fixture.paths.JournalPaths(), testPlanLoadOptions(fixture.paths))
			var conflict *ownership.ForeignClaimError
			if !errors.As(err, &conflict) || !conflict.Existing.Equal(claim) {
				t.Fatalf("initial ownership refusal = %v, want the current foreign claim", err)
			}
			assertRecoveryTestContent(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), fixture.after["AGENTS.md"])
			assertRecoveryJournalRetained(t, fixture.plan)
		})
	}
}

func TestOrdinaryProjectRestorationRechecksForeignClaimAtTheWriteGate(t *testing.T) {
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec("codex-project", target.TargetCodex, "AGENTS.md"))
	options := testRecoveryOptions(fixture.paths)
	var claim ownership.Claim
	options.beforeHostAction = func(int) error {
		claim = seedRestorationForeignClaim(t, fixture.paths, filepath.Join(fixture.projectRoot, "AGENTS.md"), output.WholePathFootprint(), false)
		return nil
	}
	err := executeRecoveryPlanWithOptionsForTest(t.Context(), fixture.plan, fixture.paths, options)
	var conflict *ownership.ForeignClaimError
	if !errors.As(err, &conflict) || !conflict.Existing.Equal(claim) {
		t.Fatalf("late ownership refusal = %v, want the newly published foreign claim", err)
	}
	assertRecoveryTestContent(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), fixture.after["AGENTS.md"])
	assertRecoveryJournalRetained(t, fixture.plan)

	store, err := ownershipstore.New(fixture.paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	change, err := ownership.NewClaimChange(claim.Address(), value, ownership.NoClaim())
	if err != nil {
		t.Fatal(err)
	}
	convergence, err := ownership.NewClaimConvergence([]ownership.ClaimChange{change})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Converge(t.Context(), convergence); err != nil {
		t.Fatal(err)
	}
	plan, err := journal.LoadActivePlanWithOptions(t.Context(), fixture.paths.JournalPaths(), testPlanLoadOptions(fixture.paths))
	if err != nil {
		t.Fatal(err)
	}
	if err := executeRecoveryPlanWithOptionsForTest(t.Context(), plan, fixture.paths, testRecoveryOptions(fixture.paths)); err != nil {
		t.Fatal("legitimate foreign release did not unblock recovery", err)
	}
	assertRecoveryTestContent(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), fixture.before["AGENTS.md"])
	if _, err := os.Stat(plan.OperationDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("successful retry retained the active journal", err)
	}
}

func TestOrdinaryProjectHostRestorationRequiresReadableRegistry(t *testing.T) {
	for _, contents := range []string{"not JSON", `{"version":999,"claims":[]}`} {
		fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec("codex-project", target.TargetCodex, "AGENTS.md"))
		if err := os.WriteFile(fixture.paths.OwnershipRegistryPath, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := journal.LoadActivePlanWithOptions(t.Context(), fixture.paths.JournalPaths(), testPlanLoadOptions(fixture.paths)); err == nil {
			t.Fatal("unreadable or future Registry became known-empty authority")
		}
		assertRecoveryTestContent(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), fixture.after["AGENTS.md"])
		assertRecoveryJournalRetained(t, fixture.plan)
	}
	fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec("codex-project", target.TargetCodex, "AGENTS.md"))
	options := testPlanLoadOptions(fixture.paths)
	options.OwnershipRegistry = nil
	if _, err := journal.LoadActivePlanWithOptions(t.Context(), fixture.paths.JournalPaths(), options); err == nil {
		t.Fatal("missing Registry reader bypassed initial restoration exclusion")
	}
}

type restorationUnreadableRegistry struct {
	reads *int
}

func (reader restorationUnreadableRegistry) Load(context.Context) (ownership.Registry, error) {
	*reader.reads++
	return ownership.Registry{}, errors.New("Registry must not be read without host work")
}

func (reader restorationUnreadableRegistry) LoadForClaimRemovals(ctx context.Context, _ []ownership.Claim, _ int, _ rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	return reader.Load(ctx)
}

func TestOrdinaryNoHostCleanupHasNoNewRegistryDependency(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "before"
		if committed {
			name = "after"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newProjectPathRecoveryFixture(t, projectInstructionRecoverySpec("codex-project", target.TargetCodex, "AGENTS.md"))
			classification := recovery.ClassificationCleanBefore
			if committed {
				classification = recovery.ClassificationCleanAfter
				writeRecoveryTestStatefile(t, fixture.paths.StatefilePath, fixture.afterState)
			} else {
				writeRecoveryTestFile(t, filepath.Join(fixture.projectRoot, "AGENTS.md"), fixture.before["AGENTS.md"])
			}
			reads := 0
			load := testPlanLoadOptions(fixture.paths)
			load.OwnershipRegistry = restorationUnreadableRegistry{reads: &reads}
			plan, err := journal.LoadActivePlanWithOptions(t.Context(), fixture.paths.JournalPaths(), load)
			if err != nil || plan.Classification() != classification || plan.RequiresHostWriteExclusion() || plan.RequiresOwnershipSettlement() || reads != 0 {
				t.Fatalf("no-host classification acquired a Registry dependency: %v, reads=%d", err, reads)
			}
			options := testRecoveryOptions(fixture.paths)
			binds := 0
			options.OwnershipRegistryBinder = func(*rootedpath.CapturedRoot, rootedpath.Destination, int, rootedpath.PhysicalTraversalBudget) (ownershipmutation.RegistryStore, error) {
				binds++
				return nil, errors.New("no-host cleanup must not bind the Registry")
			}
			if err := executeRecoveryPlanWithOptionsForTest(t.Context(), plan, fixture.paths, options); err != nil || binds != 0 {
				t.Fatalf("no-host cleanup depends on the Registry: %v, binds=%d", err, binds)
			}
		})
	}
}

func TestOrdinarySelectedRestorationPreservesForeignSiblingOwnership(t *testing.T) {
	for _, name := range []string{"selected", "foreign", "whole", "legacy absent root"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			paths := managedAggregateTestPaths(root, filepath.Join(root, ".daem"))
			path := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
			before := aggregate.AbsentDocument()
			if name != "legacy absent root" {
				before = aggregate.ExistingDocument([]byte(`{"mcpServers":{"prior":{"type":"stdio","command":"prior-command"}}}`))
				if err := os.WriteFile(path, before.Content(), aggregate.DocumentFileMode); err != nil {
					t.Fatal(err)
				}
			}
			effects := managedMCPAggregateEffects(t, before, []mcpEffectSpec{{serverID: "selected", command: "selected-command"}})
			_, applyErr := ApplyWithOptions(t.Context(), ApplyInput{
				Paths: paths, Resolver: destinationResolver(paths), AggregateEffects: effects,
				CurrentState: durable.EmptySnapshot(), Codecs: testAggregateCodecs(),
				StateCodec: testStateCodec(), Filesystem: testFilesystem(), OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			}, ApplyOptions{commitStatefile: func(context.Context, string, []byte, os.FileMode) error {
				return errors.New("indeterminate state commit")
			}})
			if applyErr == nil {
				t.Fatal("fixture did not retain an interrupted ordinary apply")
			}
			if _, err := os.Stat(paths.StatefilePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("fixture must have an exact selected namespace but no persisted statefile", err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document, servers map[string]json.RawMessage
			if err := json.Unmarshal(content, &document); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(document["mcpServers"], &servers); err != nil {
				t.Fatal(err)
			}
			servers["foreign"] = json.RawMessage(`{"type":"stdio","command":"keep"}`)
			document["mcpServers"], err = json.Marshal(servers)
			if err != nil {
				t.Fatal(err)
			}
			content, err = json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, content, aggregate.DocumentFileMode); err != nil {
				t.Fatal(err)
			}
			footprint := output.WholePathFootprint()
			if name != "whole" {
				keyName := name
				if name == "legacy absent root" {
					keyName = "foreign"
				}
				key, err := output.NewKeyPath("mcpServers", keyName)
				if err != nil {
					t.Fatal(err)
				}
				footprint, err = output.NewKeyFootprint(key)
				if err != nil {
					t.Fatal(err)
				}
			}
			claim := seedRestorationForeignClaim(t, paths, path, footprint, false)
			plan, err := journal.LoadActivePlanWithOptions(t.Context(), paths.JournalPaths(), testPlanLoadOptions(paths))
			if name == "selected" || name == "whole" {
				var conflict *ownership.ForeignClaimError
				if !errors.As(err, &conflict) || !conflict.Existing.Equal(claim) {
					t.Fatal("actual selected/root effects did not refuse foreign ownership", err)
				}
				assertRecoveryTestContent(t, path, content)
				return
			}
			if err != nil || plan.RequiresOwnershipSettlement() || !plan.RequiresHostWriteExclusion() {
				t.Fatal("retained foreign sibling was mistaken for whole-document deletion", err)
			}
			if name == "legacy absent root" {
				err := executeRecoveryPlanWithOptionsForTest(t.Context(), plan, paths, testRecoveryOptions(paths))
				if err == nil || !strings.Contains(err.Error(), "does not match an admitted removable state") {
					t.Fatal("current ownership synthesized a new legacy root/parent Before state", err)
				}
				assertRecoveryTestContent(t, path, content)
				assertRecoveryJournalRetained(t, plan)
				return
			}
			if err := executeRecoveryPlanWithOptionsForTest(t.Context(), plan, paths, testRecoveryOptions(paths)); err != nil {
				t.Fatal("disjoint foreign ownership blocked the selected restoration", err)
			}
			restored, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("restoration removed the retained foreign root", err)
			}
			assertJSONMCPString(t, restored, "mcpServers", "foreign", "command", "keep")
			assertJSONMCPString(t, restored, "mcpServers", "prior", "command", "prior-command")
			if err := json.Unmarshal(restored, &document); err != nil {
				t.Fatal(err)
			}
			servers = nil
			if err := json.Unmarshal(document["mcpServers"], &servers); err != nil {
				t.Fatal(err)
			}
			if _, retained := servers["selected"]; retained {
				t.Fatal("selected projection was not restored to absence")
			}
		})
	}
}

func seedRestorationForeignClaim(t *testing.T, paths Paths, hostPath string, footprint output.Footprint, reserved bool) ownership.Claim {
	t.Helper()
	statefile := filepath.Join(filepath.Dir(paths.StatefilePath), "foreign", "state.json")
	writeRecoveryTestStatefile(t, statefile, durable.EmptySnapshot())
	owner, err := stateauthority.New(mustObservedPathAuthority(t, statefile), filepath.Join(filepath.Dir(statefile), "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(mustObservedPathAuthority(t, hostPath), footprint)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if reserved {
		claim, err = ownership.NewReservedClaim(address, owner, "another-operation")
	}
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	change, err := ownership.NewClaimChange(address, ownership.NoClaim(), value)
	if err != nil {
		t.Fatal(err)
	}
	convergence, err := ownership.NewClaimConvergence([]ownership.ClaimChange{change})
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Converge(t.Context(), convergence); err != nil {
		t.Fatal(err)
	}
	return claim
}
