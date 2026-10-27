package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeKeyUnicodeGlobalAcquisition(t *testing.T) {
	boundaries := []string{"none", "host", "state", "return-error", "cancel", "return-state-error"}
	if runtime.GOOS == "darwin" {
		boundaries = append(boundaries, "promotion", "return-promotion-error")
	}
	for _, boundary := range boundaries {
		t.Run(boundary, func(t *testing.T) {
			selected := nativeKeyScopes()[2]
			selected.name = "codex-global-unicode"
			selected.filename = "extension-settings/Caf\u00e9.toml"
			fixture := newNativeKeyScopeFixture(t, selected, false)
			workspace := filepath.Dir(fixture.manifest)
			testkit.WriteFile(t, workspace, "preferences.toml", "native_other=true\n"+nativeKeyScopeSource(selected, "after-model"))
			runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "lock")

			if boundary == "host" || boundary == "promotion" || boundary == "state" {
				code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, boundary, "apply", "--yes", "--verbose")
				if code != nativeRecoveryExit || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
					t.Fatalf("Unicode process loss did not follow actual host publication: %d %s %s", code, stdout, stderr)
				}
				if boundary == "promotion" && !strings.Contains(stderr, "native recovery confirmed group promotion") {
					t.Fatal("Unicode promotion boundary was not crossed", stderr)
				}
				requireNativeRecoveryJournalState(t, fixture.nativeRecoveryFixture, map[string]string{"host": "host", "promotion": "host", "state": "state"}[boundary])
				requireNativeUnicodeJournalPhase(t, fixture, boundary)
				recoverNativeUnicodeAcquisition(t, fixture, boundary)
			} else {
				code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, boundary, "apply", "--yes", "--verbose")
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
					if code == 0 || code == nativeRecoveryExit || !strings.Contains(stderr, message) || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) {
						t.Fatalf("Unicode acquisition did not cross the injected boundary: %d %s %s", code, stdout, stderr)
					}
					if boundary == "return-promotion-error" {
						recoverNativeUnicodeAcquisition(t, fixture, boundary)
					}
				}
			}

			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			if boundary == "host" || boundary == "promotion" || boundary == "return-error" || boundary == "cancel" || boundary == "return-promotion-error" {
				requireNativeKeyScopeBefore(t, fixture, false)
				requireNativeSharedValues(t, selected.syntax, testkit.ReadFile(t, fixture.destination), "native_other=true\n", false)
				requireNativeKeyScopeMetadata(t, fixture, false)
			}

			runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
			requireNativeKeyScopeDesired(t, fixture, "after-model")
			requireNativeSharedValues(t, selected.syntax, testkit.ReadFile(t, fixture.destination), "native_other=true\n", true)
			requireNativeUnicodeManagedLeaves(t, fixture)
		})
	}
}

func requireNativeUnicodeJournalPhase(t *testing.T, fixture nativeKeyScopeFixture, boundary string) {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(fixture.paths.RecoveryDir, "*", "journal.json"))
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
	requireNativeRecoveryDryRun(t, fixture.nativeRecoveryFixture, classification)
	runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "recover", "--yes")
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

	budget := recovery.NewPhysicalCaptureBudget(nil)
	state, err := statefile.LoadOptionalWithOptions(t.Context(), fixture.paths.StatefilePath, statefile.ReadOptions{Codec: codec, PhysicalPathBudget: budget})
	if err != nil || len(state.ManagedAggregates()) != 2 {
		t.Fatal("Unicode group did not settle both selected leaves in state", err)
	}
	reader, err := ownershipstore.NewRecoveryReader(fixture.paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := reader.LoadForClaimRemovals(t.Context(), nil, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil || len(registry.Claims()) != 2 || len(registry.DocumentBindings()) != 1 {
		t.Fatal("Unicode group did not settle both selected claims and its binding", err)
	}
}

func (store *nativeRecoveryStore) ReplaceRootedFileAndRefreshParent(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode fs.FileMode, expected, expectedParent mutationfs.EntryIdentity) (mutationfs.CommitOutcome, mutationfs.EntryIdentity, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutationfs.CommitOutcome{}, nil, err
	}
	outcome, parent, err := store.Adapter.ReplaceRootedFileAndRefreshParent(ctx, capability, content, mode, expected, expectedParent)
	if err != nil || filepath.Base(path) != "journal.json" || store.boundary != "promotion" && store.boundary != "return-promotion-error" {
		return outcome, parent, err
	}

	actual, err := os.ReadFile(path)
	if err != nil {
		return outcome, parent, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return outcome, parent, err
	}
	if !bytes.Equal(actual, content) || info.Mode().Perm() != mode.Perm() || outcome.State() != mutationfs.CommitOutcomeComplete || parent == nil {
		return outcome, parent, errors.New("native recovery fixture: group promotion was not confirmed")
	}
	fmt.Fprintln(os.Stderr, "native recovery confirmed group promotion")
	if store.boundary == "promotion" {
		os.Exit(nativeRecoveryExit)
	}
	if !store.failed {
		store.failed = true
		return outcome, parent, errors.New("native recovery fixture: returned promotion error")
	}
	return outcome, parent, nil
}
