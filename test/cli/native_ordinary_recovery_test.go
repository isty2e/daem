package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func nativeOrdinaryRecoveryFixture(t *testing.T) (nativefixture.RecoveryFixture, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	fixture := nativefixture.RecoveryFixture{
		Manifest:    filepath.Join(workspace, "daem.toml"),
		Destination: filepath.Join(workspace, ".codex", "config.toml"), Kind: "ordinary",
	}
	testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['codex']\n[instructions.project]\nsource='./instructions.md'\n[[host_config]]\nid='preferences'\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n")
	testkit.WriteFile(t, workspace, "instructions.md", "before instructions\n")
	testkit.WriteFile(t, workspace, "preferences.toml", "model='before-model'\n")
	testkit.WriteFile(t, workspace, ".codex/config.toml", nativefixture.RecoveryOutside)
	if err := os.Chmod(fixture.Destination, 0o644); err != nil {
		t.Fatal(err)
	}
	nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
	nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
	fixture.Paths, err = daempaths.Resolve(fixture.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, workspace, "instructions.md", "after instructions\n")
	testkit.WriteFile(t, workspace, "preferences.toml", "model='after-model'\n")
	nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
	return fixture, filepath.Join(workspace, "AGENTS.md")
}

func TestNativeOrdinaryRecoveryAfterProcessLoss(t *testing.T) {
	for _, boundary := range []string{"native", "ordinary", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, instruction := nativeOrdinaryRecoveryFixture(t)
			before := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, instruction, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)
			watched, stage := fixture, "host"
			if boundary == "ordinary" {
				watched.Destination = instruction
			}
			if boundary == "state" {
				stage = "state"
			}
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, watched, stage, "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("real mixed Apply did not reach %s: code=%d stdout=%s stderr=%s", boundary, code, stdout, stderr)
			}
			classification := "needs_rollback"
			if boundary == "state" {
				classification = "clean_after"
			}
			nativefixture.RequireRecoveryDryRun(t, fixture, classification)
			committed := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, instruction)
			identities := make(map[string]os.FileInfo)
			for _, path := range []string{fixture.Destination, instruction} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				identities[path] = info
			}
			nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
			if boundary != "state" {
				actual := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, instruction, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)
				if !reflect.DeepEqual(before, actual) {
					t.Fatal("Before-side recovery did not restore both families and complete metadata")
				}
			} else if !reflect.DeepEqual(committed, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, instruction)) {
				t.Fatal("After-side metadata settlement replayed host writes")
			}
			if boundary == "state" {
				for path, before := range identities {
					after, err := os.Stat(path)
					if err != nil || !os.SameFile(before, after) {
						t.Fatal("After-side recovery replaced a committed host file", path, err)
					}
				}
			}
			nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
			nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "model='after-model'\n", true)
			if !strings.Contains(string(testkit.ReadFile(t, instruction)), "after instructions\n") {
				t.Fatal("retry omitted the ordinary declaration")
			}
			nativefixture.RunRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
		})
	}
}

func TestNativeProjectGlobalOnlyRecoveryAfterProcessLoss(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, instruction := nativeOrdinaryRecoveryFixture(t)
			workspace := filepath.Dir(fixture.Manifest)
			native := fixture.Destination
			testkit.WriteFile(t, workspace, "instructions.md", "before instructions\n")
			testkit.WriteFile(t, workspace, "preferences.toml", "model='before-model'\n")
			globalSource := filepath.Join(workspace, "global.md")
			testkit.WriteFile(t, workspace, "global.md", "global instructions\n")
			manifest := string(testkit.ReadFile(t, fixture.Manifest)) + fmt.Sprintf("\n[instructions.global]\nsource=%q\n", globalSource)
			testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), manifest)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
			fixture.Destination = filepath.Join(os.Getenv("HOME"), ".codex", "AGENTS.md")
			before := nativefixture.SnapshotRecoveryPaths(t, native, instruction, fixture.Destination, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)
			nativeBefore := testkit.ReadFile(t, native)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("global-only operation did not reach %s publication: %d %s %s", boundary, code, stdout, stderr)
			}
			classification := "needs_rollback"
			if boundary == "state" {
				classification = "needs_finalize"
			}
			nativefixture.RequireRecoveryDryRun(t, fixture, classification)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
			if !reflect.DeepEqual(nativeBefore, testkit.ReadFile(t, native)) {
				t.Fatal("recovery changed the unchanged project-native document")
			}
			if boundary == "host" && !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, native, instruction, fixture.Destination, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)) {
				t.Fatal("Before recovery did not restore the complete mixed snapshot")
			}
			if boundary == "state" && string(testkit.ReadFile(t, fixture.Destination)) != "global instructions\n" {
				t.Fatal("After recovery lost the committed global output")
			}
			nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
			nativefixture.RunRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
		})
	}
}

func TestNativeOrdinaryRecoveryRejectsChangedEvidence(t *testing.T) {
	for _, fault := range []string{"native third value", "ordinary third value", "corrupt ordinary backup while already Before"} {
		t.Run(fault, func(t *testing.T) {
			fixture, instruction := nativeOrdinaryRecoveryFixture(t)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "host", "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("fault did not follow actual native publication: %d %s %s", code, stdout, stderr)
			}
			switch fault {
			case "native third value":
				testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), "model='third-model'\n"+nativefixture.RecoveryOutside)
			case "ordinary third value":
				testkit.WriteFile(t, filepath.Dir(instruction), filepath.Base(instruction), "third instructions\n")
			default:
				journals, err := filepath.Glob(filepath.Join(fixture.Paths.RecoveryDir, "*", "journal.json"))
				if err != nil || len(journals) != 1 {
					t.Fatal("actual joint journal is missing", err)
				}
				var record struct {
					Entries []struct {
						Path   string `json:"path"`
						Before struct {
							Backup string `json:"backup_path"`
						} `json:"before"`
					} `json:"entries"`
				}
				if err := json.Unmarshal(testkit.ReadFile(t, journals[0]), &record); err != nil {
					t.Fatal(err)
				}
				backup := ""
				for _, entry := range record.Entries {
					if entry.Path == "AGENTS.md" {
						backup = entry.Before.Backup
					}
				}
				if backup == "" {
					t.Fatal("actual ordinary Before backup is missing")
				}
				testkit.WriteFile(t, filepath.Dir(journals[0]), backup, "corrupt backup\n")
			}
			before := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, instruction, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath, fixture.Paths.RecoveryDir)
			for _, mode := range []string{"--dry-run", "--yes"} {
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "recover", mode, "--json")
				if code == 0 || !json.Valid([]byte(stdout)) {
					t.Fatalf("invalid joint evidence was accepted or not reportable: %d %s %s", code, stdout, stderr)
				}
				if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, instruction, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath, fixture.Paths.RecoveryDir)) {
					t.Fatal("refused joint recovery changed a host, metadata or recovery artifact")
				}
			}
		})
	}
}

func TestNativeOrdinaryAutomaticRecovery(t *testing.T) {
	for _, family := range []string{"native", "ordinary"} {
		for _, boundary := range []string{"return-error", "cancel"} {
			t.Run(family+"/"+boundary, func(t *testing.T) {
				fixture, instruction := nativeOrdinaryRecoveryFixture(t)
				before := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, instruction, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)
				watched := fixture
				if family == "ordinary" {
					watched.Destination = instruction
				}
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, watched, boundary, "apply", "--yes")
				if code == 0 || !strings.Contains(stderr, nativefixture.RecoveryReceipt(watched.Destination)) {
					t.Fatalf("fault did not follow actual %s publication: code=%d stdout=%s stderr=%s", family, code, stdout, stderr)
				}
				if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, instruction, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)) {
					t.Fatal("automatic recovery did not restore the complete Before phase", stderr)
				}
				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
				nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
				nativefixture.RunRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
			})
		}
	}
}
