package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	daempaths "github.com/isty2e/daem/internal/paths"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestRunInputDiagnosticsAreCredentialSafeAndBounded(t *testing.T) {
	large := strings.Repeat("invalid", 32<<10)
	for _, input := range []struct {
		name     string
		manifest string
	}{
		{name: "credential", manifest: "version=1\ntargets=[\"codex\"]\n[instructions.project]\nsource={s3=\"s3://review-user:review-secret@bucket/key\",version_id=\"test-version\",region=\"us-east-1\"}\n"},
		{name: "oversized credential", manifest: fmt.Sprintf("version=1\ntargets=[\"codex\"]\n[instructions.project]\nsource={s3=%q,version_id=\"test-version\",region=\"us-east-1\"}\n", "s3://review-user:review-secret"+strings.Repeat("x", 32<<10)+"@bucket/key")},
		{name: "transport", manifest: fmt.Sprintf("version=1\ntargets=[\"claude-code\"]\n[[mcp_server]]\nname=\"review\"\ntransport=%q\ncommand=\"must-not-run\"\n", large)},
		{name: "install mode", manifest: fmt.Sprintf("version=1\ntargets=[\"codex\"]\n[defaults]\ninstall_mode=%q\n", large)},
	} {
		for _, args := range [][]string{
			{"apply", "--dry-run"},
			{"apply", "--yes"},
			{"apply"},
			{"apply", "--dry-run", "--verbose"},
			{"apply", "--yes", "--verbose"},
			{"apply", "--dry-run", "--json"},
			{"apply", "--yes", "--json"},
			{"status"},
			{"status", "--json"},
			{"lock", "--dry-run"},
			{"lock", "--dry-run", "--json"},
		} {
			t.Run(input.name+"/"+strings.Join(args, " "), func(t *testing.T) {
				root := isolatedApplyAuthorityRoot(t)
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				manifestPath := filepath.Join(root, "daem.toml")
				if err := os.WriteFile(manifestPath, []byte(input.manifest), 0o600); err != nil {
					t.Fatal(err)
				}
				before := snapshotApplyInputFiles(t, root)
				var stdout, stderr bytes.Buffer
				command := append(append([]string{}, args...), "--manifest", manifestPath)
				var code int
				if len(args) == 1 && args[0] == "apply" {
					code = RunWithOptions(command, interactiveRunOptions(strings.NewReader(""), &stdout, &stderr))
				} else {
					code = runWithoutTerminal(command, &stdout, &stderr)
				}
				if code != 1 || stdout.Len() != 0 {
					t.Fatalf("input refusal code=%d stdout bytes=%d", code, stdout.Len())
				}
				for _, secret := range []string{"review-user", "review-secret"} {
					if strings.Contains(stderr.String(), secret) {
						t.Error("input refusal disclosed a credential-shaped source")
					}
				}
				if !strings.Contains(stderr.String(), "[REDACTED]") {
					t.Error("input refusal did not indicate withheld detail")
				}
				if input.name != "credential" && !strings.Contains(stderr.String(), "[truncated]") {
					t.Error("oversized input refusal did not indicate truncation")
				}
				if count := utf8.RuneCountInString(stderr.String()); count > 4096+1024 {
					t.Errorf("input refusal emitted %d runes, want bounded context and guidance", count)
				}
				if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
					t.Error("input refusal modified its isolated workspace")
				}
			})
		}
	}
}

func TestRunApplyCaptureFailurePreservesRecoveryPrecedence(t *testing.T) {
	seedRoot := isolatedApplyAuthorityRoot(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(seedRoot, "state"))
	const manifest = "version=1\ntargets=[\"codex\"]\n"
	seedManifest := filepath.Join(seedRoot, "daem.toml")
	if err := os.WriteFile(seedManifest, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	var seedOut, seedErr bytes.Buffer
	if code := runWithoutTerminal([]string{"lock", "--manifest", seedManifest}, &seedOut, &seedErr); code != 0 {
		t.Fatalf("seed lock code=%d stderr=%q", code, &seedErr)
	}
	validLock, err := os.ReadFile(filepath.Join(seedRoot, "daem.lock.toml"))
	if err != nil {
		t.Fatal(err)
	}

	for _, barrierKind := range []string{"journal", "file-set", "joint"} {
		for _, selected := range []string{"manifest", "lock"} {
			for _, shape := range []string{"directory", "oversized"} {
				t.Run(barrierKind+"/"+selected+"/"+shape, func(t *testing.T) {
					root := t.TempDir()
					manifestPath := filepath.Join(root, "daem.toml")
					var journalFixture *recoverConfirmationFixture
					if barrierKind != "file-set" {
						fixture := writeRecoverConfirmationFixture(t)
						journalFixture = &fixture
						manifestPath = fixture.manifestPath
						root = filepath.Dir(manifestPath)
					}
					if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
						t.Fatal(err)
					}
					paths, err := daempaths.Resolve(manifestPath)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(paths.LockfilePath, validLock, 0o600); err != nil {
						t.Fatal(err)
					}
					if barrierKind != "journal" {
						if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(filepath.Join(paths.StateDir, ".daem-tmp-"+strings.Repeat("b", 32)), 0o700); err != nil {
							t.Fatal(err)
						}
					}
					badPath := manifestPath
					if selected == "lock" {
						badPath = paths.LockfilePath
					}
					if err := os.Remove(badPath); err != nil {
						t.Fatal(err)
					}
					if shape == "directory" {
						if err := os.Mkdir(badPath, 0o700); err != nil {
							t.Fatal(err)
						}
					} else {
						file, err := os.OpenFile(badPath, os.O_CREATE|os.O_WRONLY, 0o600)
						if err != nil {
							t.Fatal(err)
						}
						truncateErr := file.Truncate(declarationartifact.MaximumBytes + 1)
						closeErr := file.Close()
						if err := errors.Join(truncateErr, closeErr); err != nil {
							t.Fatal(err)
						}
					}
					inputBefore, err := os.Lstat(badPath)
					if err != nil {
						t.Fatal(err)
					}
					stateBefore := snapshotApplyInputFiles(t, paths.StateDir)

					dry, dryErr := applyworkflow.PlanDryRun(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
					if barrierKind != "file-set" && !errors.Is(dryErr, journal.ErrInterruptedApply) {
						t.Fatal("fixture does not retain an active journal barrier")
					}
					if barrierKind != "journal" && !errors.Is(dryErr, fileset.ErrAbandonedFileSetResidue) {
						t.Fatal("fixture does not retain a file-set barrier")
					}
					want := applyworkflow.ClassifyFailure(dryErr, dry.CommandResult)
					prepared, writeErr := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: manifestPath})
					if prepared != nil {
						if err := prepared.Close(); err != nil {
							t.Fatal(err)
						}
					}
					got := applyworkflow.ClassifyFailure(writeErr, applyworkflow.CommandResult{})
					if writeErr == nil || got.Reason() != want.Reason() || got.RecoveryBarrier() != want.RecoveryBarrier() {
						t.Errorf("write capture bypassed barrier facts: got=%q want=%q", got.Reason(), want.Reason())
					}
					if _, marked := applyworkflow.InputErrorCause(writeErr); marked {
						t.Error("barrier refusal admitted a user-facing input cause")
					}

					for _, mode := range [][]string{{"--yes"}, {"--yes", "--json"}, {}} {
						var stdout, stderr bytes.Buffer
						args := append([]string{"apply", "--manifest", manifestPath}, mode...)
						var code int
						if len(mode) == 0 {
							code = RunWithOptions(args, interactiveRunOptions(strings.NewReader(""), &stdout, &stderr))
						} else {
							code = runWithoutTerminal(args, &stdout, &stderr)
						}
						if code != 1 || stdout.Len() != 0 {
							t.Errorf("fenced input code=%d stdout bytes=%d", code, stdout.Len())
						}
						if !strings.Contains(stderr.String(), want.Detail()) || strings.Contains(stderr.String(), "correct the reported manifest") {
							t.Errorf("write-mode input disclosure replaced the recovery disposition: %q", &stderr)
						}
						if strings.Contains(stderr.String(), root) {
							t.Error("recovery refusal disclosed the private input path")
						}
					}

					inputAfter, err := os.Lstat(badPath)
					if err != nil || !os.SameFile(inputBefore, inputAfter) || inputBefore.Size() != inputAfter.Size() || inputBefore.Mode() != inputAfter.Mode() || !inputBefore.ModTime().Equal(inputAfter.ModTime()) {
						t.Error("capture refusal replaced or changed the invalid declaration")
					}
					if stateAfter := snapshotApplyInputFiles(t, paths.StateDir); !reflect.DeepEqual(stateBefore, stateAfter) {
						t.Error("capture refusal modified recovery evidence or state")
					}
					if journalFixture != nil {
						journalFixture.assertUnchanged(t)
					}
				})
			}
		}
	}
}
