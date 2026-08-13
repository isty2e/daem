//go:build darwin || linux

package apply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declarationartifact"
	"golang.org/x/sys/unix"
)

func TestPlanWriteRejectsDirectoryManifestWithoutTraversingChildren(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest")
	if err := os.Mkdir(manifestPath, 0o700); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(manifestPath, "must-not-be-visited")
	if err := unix.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err == nil {
		t.Fatal("PlanWrite accepted a directory manifest")
	}
	if !strings.Contains(err.Error(), "must be absent or resolve to a regular file") {
		t.Fatalf("PlanWrite error = %v, want bounded regular-file diagnostic", err)
	}
	if strings.Contains(err.Error(), fifoPath) {
		t.Fatalf("PlanWrite traversed nested FIFO: %v", err)
	}
}

func TestPlanWriteRejectsOversizedManifestBeforeLoading(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	file, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(declarationartifact.MaximumBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err == nil {
		t.Fatal("PlanWrite accepted an oversized manifest")
	}
	if !strings.Contains(err.Error(), "67108864 bytes") {
		t.Fatalf("PlanWrite error = %v, want declaration byte-limit diagnostic", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PlanWrite error = %v, want size rejection", err)
	}
}

func TestExecuteRejectsDirectoryDeclarationWithoutTraversingChildren(t *testing.T) {
	for _, selected := range []string{"manifest", "lockfile"} {
		t.Run(selected, func(t *testing.T) {
			root := t.TempDir()
			paths := applyTestPaths(t, root)
			writeApplyManifestFile(t, paths.ManifestPath)
			sourcePath := filepath.Join(root, "instructions", "AGENTS.md")
			writeApplyFile(t, sourcePath, "unchanged\n")
			writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(
				t,
				"project",
				"local:instructions/AGENTS.md?mode=vendor",
				hashApplyPath(t, sourcePath),
			))
			prepared, err := PlanWrite(t.Context(), CommandInput{
				ManifestPath: paths.ManifestPath,
				LockfilePath: paths.LockfilePath,
			})
			if err != nil {
				t.Fatal(err)
			}

			selectedPath := paths.ManifestPath
			if selected == "lockfile" {
				selectedPath = paths.LockfilePath
			}
			if err := os.Remove(selectedPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(selectedPath, 0o700); err != nil {
				t.Fatal(err)
			}
			fifoPath := filepath.Join(selectedPath, "must-not-be-visited")
			if err := unix.Mkfifo(fifoPath, 0o600); err != nil {
				t.Fatal(err)
			}

			_, err = ExecuteWithOptions(
				t.Context(),
				prepared,
				ExecuteOptions{PlanWasDisclosed: true},
			)
			if err == nil {
				t.Fatal("ExecuteWithOptions accepted a directory declaration")
			}
			if !strings.Contains(err.Error(), "must be absent or resolve to a regular file") {
				t.Fatalf(
					"ExecuteWithOptions error = %v, want bounded regular-file diagnostic",
					err,
				)
			}
			if strings.Contains(err.Error(), fifoPath) {
				t.Fatalf("ExecuteWithOptions traversed nested FIFO: %v", err)
			}
		})
	}
}
