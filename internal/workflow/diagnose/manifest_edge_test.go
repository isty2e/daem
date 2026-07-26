package diagnoseworkflow

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/target"
)

func TestRunDirectoryManifestIsClassifiedAsReadFailure(t *testing.T) {
	manifestPath := t.TempDir()
	configureDoctorEnvironment(t)
	counts, _ := installCountingManifestLoader(t)

	result, err := runCurrent(t.Context(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{stat: 1, read: 1})
	manifest, ok := checkNamed(result.Checks, "manifest")
	if !ok || manifest.Severity != findings.SeverityError || !strings.HasPrefix(manifest.Detail, "read "+manifestPath+":") {
		t.Fatalf("manifest check = %#v, want read failure", result.Checks)
	}
	if !hasCheckNamed(result.Checks, "git") {
		t.Fatalf("general diagnostics did not continue: %#v", result.Checks)
	}
}

func TestRunManifestStatFailureDoesNotRead(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "blocked.toml")
	configureDoctorEnvironment(t)
	counts, _ := installCountingManifestLoader(t)
	doctorManifestLoader.stat = func(string) (fs.FileInfo, error) {
		counts.stat++
		return nil, fs.ErrPermission
	}

	result, err := runCurrent(t.Context(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{stat: 1})
	manifest, ok := checkNamed(result.Checks, "manifest")
	if !ok || !strings.HasPrefix(manifest.Detail, "read "+manifestPath+":") {
		t.Fatalf("manifest check = %#v, want read failure", result.Checks)
	}
}

func TestRunManifestRemovalBetweenStatAndReadUsesOneReadFailure(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	writeDoctorManifest(t, manifestPath, "version = 1\ntargets = [\"codex\"]\n")
	configureDoctorEnvironment(t)
	counts, original := installCountingManifestLoader(t)
	doctorManifestLoader.readFile = func(path string) ([]byte, error) {
		counts.read++
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		return original.readFile(path)
	}

	result, err := runCurrent(t.Context(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{stat: 1, read: 1})
	manifest, ok := checkNamed(result.Checks, "manifest")
	if !ok || !strings.HasPrefix(manifest.Detail, "read "+manifestPath+":") {
		t.Fatalf("manifest check = %#v, want read failure", result.Checks)
	}
	if !hasCheckNamed(result.Checks, "git") {
		t.Fatalf("general diagnostics did not continue: %#v", result.Checks)
	}
}

func TestRunUsesSingleByteSnapshotWhenManifestChangesAfterRead(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	writeDoctorManifest(t, manifestPath, "version = 1\ntargets = [\"codex\"]\n")
	configureDoctorEnvironment(t)
	counts, original := installCountingManifestLoader(t)
	doctorManifestLoader.normalize = func(content []byte) (desired.Environment, error) {
		counts.normalize++
		writeDoctorManifest(t, manifestPath, "version = 1\ntargets = [\"claude-code\"]\n")
		return original.normalize(content)
	}

	result, err := runCurrent(t.Context(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{stat: 1, read: 1, normalize: 1, buildFacts: 1})
	if got := result.Selection.Targets(); len(got) != 1 || got[0] != target.TargetCodex {
		t.Fatalf("Selection = %#v, want original codex snapshot", got)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read replaced manifest: %v", err)
	}
	if !strings.Contains(string(content), "claude-code") {
		t.Fatalf("manifest was not replaced during normalization: %q", content)
	}
}
