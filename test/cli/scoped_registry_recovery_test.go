package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	workflowrecover "github.com/isty2e/daem/internal/workflow/recover"
	"github.com/isty2e/daem/test/ownershiptest"
	"github.com/isty2e/daem/test/testkit"
)

func TestPreparedJointRecoveryPreservesDisjointProtectionAfterProcessLoss(t *testing.T) {
	for _, boundary := range []string{"rollback", "finalize", "retained-native"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, instruction := nativeOrdinaryRecoveryFixture(t)
			watched, stage := fixture, "host"
			if boundary == "finalize" {
				stage = "state"
			}
			if boundary == "retained-native" {
				testkit.WriteFile(t, filepath.Dir(fixture.manifest), "preferences.toml", "model='before-model'\n")
				runNativeRecoveryCommand(t, fixture, "none", "lock")
				watched.destination = instruction
			}
			code, stdout, stderr := invokeNativeRecoveryCommand(t, watched, stage, "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("mixed process did not stop after publication: %d %s %s", code, stdout, stderr)
			}
			prepared, err := workflowrecover.Plan(t.Context(), workflowrecover.PlanInput{ManifestPath: fixture.manifest})
			if err != nil {
				t.Fatal(err)
			}
			protected := filepath.Join(filepath.Dir(fixture.manifest), "foreign.json")
			if boundary == "retained-native" {
				protected = fixture.destination
			}
			pending := ownershiptest.WritePendingDocumentProtection(t, fixture.manifest, protected)
			if _, err := workflowrecover.Execute(t.Context(), prepared, workflowrecover.ExecuteOptions{}); err != nil {
				t.Fatalf("disjoint protection invalidated prepared %s recovery: %v", boundary, err)
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			store, err := ownershipstore.New(fixture.paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.Load(t.Context())
			if err != nil || len(registry.PendingProtections()) != 1 || !registry.PendingProtections()[0].Equal(pending) {
				t.Fatal("recovery lost the unrelated complete safety record", err)
			}
			for _, binding := range pending.Documents()[0].Bindings() {
				retained, present := registry.DocumentBinding(binding.Owner().Key(), binding.Destination())
				if !present || !retained.Equal(binding) {
					t.Fatal("recovery lost a protected binding cohort member")
				}
			}
			expectedModel, expectedInstruction := "model='before-model'\n", "before instructions\n"
			if boundary == "finalize" {
				expectedModel, expectedInstruction = "model='after-model'\n", "after instructions\n"
			}
			requireNativeSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.destination), expectedModel, true)
			if actual := string(testkit.ReadFile(t, instruction)); actual != expectedInstruction {
				t.Fatalf("recovery restored the wrong ordinary phase: %q", actual)
			}
		})
	}
}

func TestPreparedJointRecoveryRefusesNewRelevantProtectionWithoutEffects(t *testing.T) {
	for _, protectedFamily := range []string{"native", "ordinary"} {
		t.Run(protectedFamily, func(t *testing.T) {
			fixture, instruction := nativeOrdinaryRecoveryFixture(t)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "host", "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("mixed process did not stop after publication: %d %s %s", code, stdout, stderr)
			}
			prepared, err := workflowrecover.Plan(t.Context(), workflowrecover.PlanInput{ManifestPath: fixture.manifest})
			if err != nil {
				t.Fatal(err)
			}
			protected := fixture.destination
			if protectedFamily == "ordinary" {
				protected = instruction
			}
			ownershiptest.WritePendingDocumentProtection(t, fixture.manifest, protected)
			before := snapshotNativeRecoveryPaths(t, fixture.destination, instruction, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath, fixture.paths.RecoveryDir)
			if _, err := workflowrecover.Execute(t.Context(), prepared, workflowrecover.ExecuteOptions{}); err == nil {
				t.Fatal("new relevant protection did not refuse prepared recovery")
			}
			if after := snapshotNativeRecoveryPaths(t, fixture.destination, instruction, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath, fixture.paths.RecoveryDir); !reflect.DeepEqual(before, after) {
				t.Fatal("refused recovery changed host, Registry, state or journal")
			}
			if _, err := os.Stat(fixture.paths.RecoveryDir); err != nil {
				t.Fatal("refused recovery discarded its retry evidence", err)
			}
		})
	}
}
