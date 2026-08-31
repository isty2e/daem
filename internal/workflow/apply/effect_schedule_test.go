package apply

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestApplyForwardEffectScheduleMatchesLegacyReservationDemand(t *testing.T) {
	_, manifestPath := writePiProviderMCPFixture(t)
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	planned := prepared.lifecycle.planned
	providerActions, err := prepareMCPProviderPrerequisiteActions(
		planned,
		allowApplyScheduleRecoveryPreflight,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := stateDirEffectPlanFor(planned, providerActions)
	if err != nil {
		t.Fatal(err)
	}
	structural, err := plan.schedule.full.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	if !sameApplyDemandCounts(structural, plan.demand) {
		t.Fatalf("structural demand = %#v, legacy demand = %#v", structural, plan.demand)
	}
	if plan.demand.DescendantPath() != planned.assessment.StatePath {
		t.Fatalf(
			"descendant path = %q, want %q",
			plan.demand.DescendantPath(),
			planned.assessment.StatePath,
		)
	}
}

func TestProviderFinalEffectScheduleRejectsChangedManagedInput(t *testing.T) {
	_, manifestPath := writePiProviderMCPFixture(t)
	initial, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	planned := initial.lifecycle.planned
	providerActions, err := prepareMCPProviderPrerequisiteActions(
		planned,
		allowApplyScheduleRecoveryPreflight,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := stateDirEffectPlanFor(planned, providerActions)
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}

	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest = append(manifest, []byte(`
[[mcp_server]]
name = "filesystem"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["filesystem.js"]
`)...)
	writeApplyFile(t, manifestPath, string(manifest))
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := changed.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if err := requireEquivalentProviderFinalSchedule(
		plan.schedule.final,
		changed.lifecycle.planned,
		providerActions,
	); err == nil {
		t.Fatal("changed managed input retained the reserved final schedule")
	}
}

func TestApplyForwardEffectScheduleIsDeterministic(t *testing.T) {
	_, manifestPath := writePiProviderMCPFixture(t)
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	planned := prepared.lifecycle.planned
	providerActions, err := prepareMCPProviderPrerequisiteActions(
		planned,
		allowApplyScheduleRecoveryPreflight,
	)
	if err != nil {
		t.Fatal(err)
	}
	applyInput, err := applyEffectInput(planned)
	if err != nil {
		t.Fatal(err)
	}
	first, err := compileApplyForwardEffectSchedule(planned, providerActions, applyInput)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compileApplyForwardEffectSchedule(planned, providerActions, applyInput)
	if err != nil {
		t.Fatal(err)
	}
	if !first.full.Equal(second.full) || !first.final.Equal(second.final) {
		t.Fatal("identical apply facts produced different effect schedules")
	}
}

func allowApplyScheduleRecoveryPreflight(rootedpath.Authority) error { return nil }
