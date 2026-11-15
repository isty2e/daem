package cli_test

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
	"github.com/isty2e/daem/test/testkit/stateimage"
)

func TestNativeKeyUnicodeGlobalAcquisition(t *testing.T) {
	boundaries := []string{"none", "host", "state", "return-error", "cancel", "return-state-error"}
	if runtime.GOOS == "darwin" {
		boundaries = append(boundaries, "promotion", "return-promotion-error")
	}
	for _, boundary := range boundaries {
		t.Run(boundary, func(t *testing.T) {
			selected := nativeKeyScopeNamed(t, "codex-global")
			selected.name = "codex-global-unicode"
			selected.filename = "extension-settings/Caf\u00e9.toml"
			fixture := newNativeKeyScopeFixture(t, selected, false)
			workspace := filepath.Dir(fixture.Manifest)
			testkit.WriteFile(t, workspace, "preferences.toml", "native_other=true\n"+nativeKeyScopeSource(selected, "after-model"))
			nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "lock")

			if boundary == "host" || boundary == "promotion" || boundary == "state" {
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, boundary, "apply", "--yes", "--verbose")
				if code != nativefixture.RecoveryExit || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
					t.Fatalf("Unicode process loss did not follow actual host publication: %d %s %s", code, stdout, stderr)
				}
				if boundary == "promotion" && !strings.Contains(stderr, "native recovery confirmed group promotion") {
					t.Fatal("Unicode promotion boundary was not crossed", stderr)
				}
				nativefixture.RequireRecoveryJournalState(t, fixture.RecoveryFixture, map[string]string{"host": "host", "promotion": "host", "state": "state"}[boundary])
				requireNativeUnicodeJournalPhase(t, fixture, boundary)
				recoverNativeUnicodeAcquisition(t, fixture, boundary)
			} else {
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, boundary, "apply", "--yes", "--verbose")
				if boundary == "none" {
					if code != 0 {
						t.Fatalf("Unicode acquisition failed: %d %s %s", code, stdout, stderr)
					}
				} else {
					message := "native recovery fixture: returned storage error"
					if boundary == "cancel" {
						message = "native recovery requested cancellation"
					}
					if boundary == "return-promotion-error" {
						message = "native recovery fixture: returned promotion error"
					}
					if code == 0 || code == nativefixture.RecoveryExit || !strings.Contains(stderr, message) || !strings.Contains(stderr, nativefixture.RecoveryReceipt(fixture.Destination)) {
						t.Fatalf("Unicode acquisition did not cross the injected boundary: %d %s %s", code, stdout, stderr)
					}
					if boundary == "return-promotion-error" {
						recoverNativeUnicodeAcquisition(t, fixture, boundary)
					}
				}
			}

			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
			if boundary == "host" || boundary == "promotion" || boundary == "return-error" || boundary == "cancel" || boundary == "return-promotion-error" {
				requireNativeKeyScopeBefore(t, fixture, false)
				nativefixture.RequireSharedValues(t, selected.syntax, testkit.ReadFile(t, fixture.Destination), "native_other=true\n", false)
				requireNativeKeyScopeMetadata(t, fixture, false)
			}

			nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes")
			requireNativeKeyScopeDesired(t, fixture, "after-model")
			nativefixture.RequireSharedValues(t, selected.syntax, testkit.ReadFile(t, fixture.Destination), "native_other=true\n", true)
			requireNativeUnicodeManagedLeaves(t, fixture)
		})
	}
}

func requireNativeUnicodeJournalPhase(t *testing.T, fixture nativeKeyScopeFixture, boundary string) {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(fixture.Paths.RecoveryDir, "*", "journal.json"))
	if err != nil || len(entries) != 1 {
		t.Fatal("missing actual Unicode acquisition journal", err)
	}
	var record struct {
		Groups []struct {
			Target struct {
				Kind string `json:"kind"`
			} `json:"captured_target"`
			Claims struct {
				Kind string `json:"kind"`
			} `json:"exclusive_claims"`
		} `json:"document_groups"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, entries[0]), &record); err != nil || len(record.Groups) != 1 {
		t.Fatal("invalid actual Unicode acquisition journal", err)
	}
	t.Logf("actual namespace=%s claims=%s", record.Groups[0].Target.Kind, record.Groups[0].Claims.Kind)
	if boundary != "host" && record.Groups[0].Claims.Kind != "exact" {
		t.Fatal("promotion/state boundary retains pending Unicode claims")
	}
}

func recoverNativeUnicodeAcquisition(t *testing.T, fixture nativeKeyScopeFixture, boundary string) {
	t.Helper()
	classification := "needs_rollback"
	if boundary == "state" {
		classification = "needs_finalize"
	}
	nativefixture.RequireRecoveryDryRun(t, fixture.RecoveryFixture, classification)
	nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "recover", "--yes")
}

func requireNativeUnicodeManagedLeaves(t *testing.T, fixture nativeKeyScopeFixture) {
	t.Helper()
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}

	budget := physicalwork.NewCaptureBudget(nil)
	state, err := statefile.LoadOptionalWithOptions(t.Context(), fixture.Paths.StatefilePath, statefile.ReadOptions{Codec: stateimage.Physical(t, codec), PhysicalPathBudget: budget})
	if err != nil || len(state.ManagedAggregates()) != 2 {
		t.Fatal("Unicode group did not settle both selected leaves in state", err)
	}
	reader, err := ownershipstore.NewRecoveryReader(fixture.Paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := reader.LoadForClaimRemovals(t.Context(), nil, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil || len(registry.Claims()) != 2 || len(registry.DocumentBindings()) != 1 {
		t.Fatal("Unicode group did not settle both selected claims and its binding", err)
	}
}
