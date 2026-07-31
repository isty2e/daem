//go:build darwin || linux

package apply

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if err := file.Truncate(maximumApplyDeclarationBytes + 1); err != nil {
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
	if !strings.Contains(err.Error(), "exceeds 67108864 bytes") {
		t.Fatalf("PlanWrite error = %v, want declaration byte-limit diagnostic", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PlanWrite error = %v, want size rejection", err)
	}
}
