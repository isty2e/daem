package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeOrdinaryGlobalAcquisitionPromotesBothFamilies(t *testing.T) {
	boundaries := []string{"none", "host", "state"}
	if runtime.GOOS == "darwin" {
		boundaries = append(boundaries, "promotion", "ordinary-promotion", "return-ordinary-promotion-error")
	}
	for _, boundary := range boundaries {
		t.Run(boundary, func(t *testing.T) {
			selected := nativeKeyScopes()[2]
			selected.filename = "extension-settings/Caf\u00e9.toml"
			fixture := newNativeKeyScopeFixture(t, selected, false)
			workspace := filepath.Dir(fixture.manifest)
			source := filepath.Join(workspace, "skills", "sample")
			testkit.WriteFile(t, source, "SKILL.md", "---\nname: Caf\u00e9\ndescription: unicode acquisition\n---\n")
			testkit.WriteFile(t, source, "references/detail.txt", "global acquisition\n")
			manifest := string(testkit.ReadFile(t, fixture.manifest)) + fmt.Sprintf("\n[[skill]]\nname='Caf\u00e9'\nscope='global'\nsource={path=%q,mode='vendor'}\n", source)
			testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), manifest)
			runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "lock")
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, boundary, "apply", "--yes", "--verbose")
			skill := filepath.Join(os.Getenv("HOME"), ".agents", "skills", "Caf\u00e9")
			if boundary == "none" {
				if code != 0 {
					t.Fatalf("joint global acquisition failed: %d %s %s", code, stdout, stderr)
				}
			} else {
				if boundary == "return-ordinary-promotion-error" {
					if code == 0 || !strings.Contains(stderr, "confirmed ordinary promotion") {
						t.Fatalf("ordinary promotion fault not reached: %d %s %s", code, stdout, stderr)
					}
				} else if code != nativeRecoveryExit {
					t.Fatalf("process-loss checkpoint not reached: %d %s %s", code, stdout, stderr)
				}
				if boundary == "ordinary-promotion" && !strings.Contains(stderr, "confirmed ordinary promotion") {
					t.Fatal("did not confirm the complete promoted record", stderr)
				}
				classification := "needs_rollback"
				if boundary == "state" {
					classification = "needs_finalize"
				}
				requireNativeRecoveryDryRun(t, fixture.nativeRecoveryFixture, classification)
				runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "recover", "--yes")
				if boundary != "state" {
					requireNativeKeyScopeBefore(t, fixture, false)
					testkit.AssertPathMissing(t, skill)
					requireNativeKeyScopeMetadata(t, fixture, false)
				}
			}
			testkit.AssertNoRecoveryArtifacts(t, workspace)
			runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
			if testkit.HashDirectory(t, source) != testkit.HashDirectory(t, skill) {
				t.Fatal("promotion lost the ordinary tree")
			}
			requireNativeSharedValues(t, selected.syntax, testkit.ReadFile(t, fixture.destination), nativeKeyScopeSource(selected, "after-model"), true)
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
			if err != nil || len(state.ManagedPaths()) != 1 || len(state.ManagedAggregates()) != 1 {
				t.Fatal("promoted state lost one family", err)
			}
			reader, err := ownershipstore.NewRecoveryReader(fixture.paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := reader.LoadForClaimRemovals(t.Context(), nil, recovery.MaximumPhysicalPathDepth, budget)
			if err != nil || len(registry.Claims()) != 2 || len(registry.DocumentBindings()) != 1 {
				t.Fatal("both acquisitions did not settle exactly", err)
			}
			runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "status", "--check", "--json")
		})
	}
}
