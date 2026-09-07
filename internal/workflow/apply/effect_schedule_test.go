package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestApplyForwardEffectScheduleFitsLegacyReservationDemand(t *testing.T) {
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
	if err := requireLegacyApplyDemandDominance(plan.schedule.full, plan.demand); err != nil {
		t.Fatal(err)
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
	root, manifestPath := writePiProviderMCPFixture(t)
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
	writeApplyFile(t, filepath.Join(root, "instructions.md"), "managed instructions\n")
	manifest = append(manifest, []byte(`
[instructions.project]
source = "instructions.md"
targets = ["pi"]
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
	if _, err := equivalentProviderFinalSchedule(
		plan.schedule.finalBinding(),
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
	if !first.full.Equal(second.full) ||
		!first.final.Equal(second.final) ||
		!first.continuation.finalRoutePlan.equal(second.continuation.finalRoutePlan) {
		t.Fatal("identical apply facts produced different effect plans")
	}
}

func allowApplyScheduleRecoveryPreflight(rootedpath.Authority) error { return nil }
