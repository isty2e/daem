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
)

func nativeOrdinaryRecoveryFixture(t *testing.T) (nativeRecoveryFixture, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setNativeRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	fixture := nativeRecoveryFixture{
		manifest:    filepath.Join(workspace, "daem.toml"),
		destination: filepath.Join(workspace, ".codex", "config.toml"), kind: "ordinary",
	}
	testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['codex']\n[instructions.project]\nsource='./instructions.md'\n[[host_config]]\nid='preferences'\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n")
	testkit.WriteFile(t, workspace, "instructions.md", "before instructions\n")
	testkit.WriteFile(t, workspace, "preferences.toml", "model='before-model'\n")
	testkit.WriteFile(t, workspace, ".codex/config.toml", nativeRecoveryOutside)
	if err := os.Chmod(fixture.destination, 0o644); err != nil {
		t.Fatal(err)
	}
	runNativeRecoveryCommand(t, fixture, "none", "lock")
	runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
	fixture.paths, err = daempaths.Resolve(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, workspace, "instructions.md", "after instructions\n")
	testkit.WriteFile(t, workspace, "preferences.toml", "model='after-model'\n")
	runNativeRecoveryCommand(t, fixture, "none", "lock")
	return fixture, filepath.Join(workspace, "AGENTS.md")
}

func TestNativeOrdinaryRecoveryAfterProcessLoss(t *testing.T) {
	for _, boundary := range []string{"native", "ordinary", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, instruction := nativeOrdinaryRecoveryFixture(t)
			before := snapshotNativeRecoveryPaths(t, fixture.destination, instruction, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)
			watched, stage := fixture, "host"
			if boundary == "ordinary" {
				watched.destination = instruction
			}
			if boundary == "state" {
				stage = "state"
			}
			code, stdout, stderr := invokeNativeRecoveryCommand(t, watched, stage, "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("real mixed Apply did not reach %s: code=%d stdout=%s stderr=%s", boundary, code, stdout, stderr)
			}
			classification := "needs_rollback"
			if boundary == "state" {
				classification = "clean_after"
			}
			requireNativeRecoveryDryRun(t, fixture, classification)
			committed := snapshotNativeRecoveryPaths(t, fixture.destination, instruction)
			identities := make(map[string]os.FileInfo)
			for _, path := range []string{fixture.destination, instruction} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				identities[path] = info
			}
			runNativeRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			if boundary != "state" {
				actual := snapshotNativeRecoveryPaths(t, fixture.destination, instruction, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)
				if !reflect.DeepEqual(before, actual) {
					t.Fatal("Before-side recovery did not restore both families and complete metadata")
				}
			} else if !reflect.DeepEqual(committed, snapshotNativeRecoveryPaths(t, fixture.destination, instruction)) {
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
			runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
			requireNativeSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.destination), "model='after-model'\n", true)
			if !strings.Contains(string(testkit.ReadFile(t, instruction)), "after instructions\n") {
				t.Fatal("retry omitted the ordinary declaration")
			}
			runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
		})
	}
}

func TestNativeProjectGlobalOnlyRecoveryAfterProcessLoss(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, instruction := nativeOrdinaryRecoveryFixture(t)
			workspace := filepath.Dir(fixture.manifest)
			native := fixture.destination
			testkit.WriteFile(t, workspace, "instructions.md", "before instructions\n")
			testkit.WriteFile(t, workspace, "preferences.toml", "model='before-model'\n")
			globalSource := filepath.Join(workspace, "global.md")
			testkit.WriteFile(t, workspace, "global.md", "global instructions\n")
			manifest := string(testkit.ReadFile(t, fixture.manifest)) + fmt.Sprintf("\n[instructions.global]\nsource=%q\n", globalSource)
			testkit.WriteFile(t, workspace, filepath.Base(fixture.manifest), manifest)
			runNativeRecoveryCommand(t, fixture, "none", "lock")
			fixture.destination = filepath.Join(os.Getenv("HOME"), ".codex", "AGENTS.md")
			before := snapshotNativeRecoveryPaths(t, native, instruction, fixture.destination, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)
			nativeBefore := testkit.ReadFile(t, native)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("global-only operation did not reach %s publication: %d %s %s", boundary, code, stdout, stderr)
			}
			classification := "needs_rollback"
			if boundary == "state" {
				classification = "needs_finalize"
			}
			requireNativeRecoveryDryRun(t, fixture, classification)
			runNativeRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
			if !reflect.DeepEqual(nativeBefore, testkit.ReadFile(t, native)) {
				t.Fatal("recovery changed the unchanged project-native document")
			}
			if boundary == "host" && !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, native, instruction, fixture.destination, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)) {
				t.Fatal("Before recovery did not restore the complete mixed snapshot")
			}
			if boundary == "state" && string(testkit.ReadFile(t, fixture.destination)) != "global instructions\n" {
				t.Fatal("After recovery lost the committed global output")
			}
			runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
			runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
		})
	}
}

func TestNativeOrdinaryRecoveryRejectsChangedEvidence(t *testing.T) {
	for _, fault := range []string{"native third value", "ordinary third value", "corrupt ordinary backup while already Before"} {
		t.Run(fault, func(t *testing.T) {
			fixture, instruction := nativeOrdinaryRecoveryFixture(t)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "host", "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("fault did not follow actual native publication: %d %s %s", code, stdout, stderr)
			}
			switch fault {
			case "native third value":
				testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), "model='third-model'\n"+nativeRecoveryOutside)
			case "ordinary third value":
				testkit.WriteFile(t, filepath.Dir(instruction), filepath.Base(instruction), "third instructions\n")
			default:
				journals, err := filepath.Glob(filepath.Join(fixture.paths.RecoveryDir, "*", "journal.json"))
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
			before := snapshotNativeRecoveryPaths(t, fixture.destination, instruction, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath, fixture.paths.RecoveryDir)
			for _, mode := range []string{"--dry-run", "--yes"} {
				code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "none", "recover", mode, "--json")
				if code == 0 || !json.Valid([]byte(stdout)) {
					t.Fatalf("invalid joint evidence was accepted or not reportable: %d %s %s", code, stdout, stderr)
				}
				if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.destination, instruction, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath, fixture.paths.RecoveryDir)) {
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
				before := snapshotNativeRecoveryPaths(t, fixture.destination, instruction, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)
				watched := fixture
				if family == "ordinary" {
					watched.destination = instruction
				}
				code, stdout, stderr := invokeNativeRecoveryCommand(t, watched, boundary, "apply", "--yes")
				if code == 0 || !strings.Contains(stderr, nativeRecoveryReceipt(watched.destination)) {
					t.Fatalf("fault did not follow actual %s publication: code=%d stdout=%s stderr=%s", family, code, stdout, stderr)
				}
				if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.destination, instruction, fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath)) {
					t.Fatal("automatic recovery did not restore the complete Before phase", stderr)
				}
				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
				runNativeRecoveryCommand(t, fixture, "none", "apply", "--yes")
				runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
			})
		}
	}
}
