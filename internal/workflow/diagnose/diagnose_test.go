package diagnoseworkflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

func TestRunContinuesEnvironmentChecksWhenExplicitManifestIsMissing(t *testing.T) {
	doctorenv.WithFakeGit(t, "git version test")

	result, err := runCurrent(context.Background(), Input{
		ManifestPath:     filepath.Join(t.TempDir(), "missing.toml"),
		ManifestExplicit: true,
		TargetExplicit:   true,
		TargetValues:     []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !findings.HasCheckErrors(result.Checks) {
		t.Fatalf("expected missing explicit manifest to report an error check")
	}
	if !result.HasErrors {
		t.Fatalf("HasErrors = false, want true for missing explicit manifest")
	}
	if !hasCheckNamed(result.Checks, "git") {
		t.Fatalf("expected environment checks to continue after missing manifest, got %#v", result.Checks)
	}
}

func TestRunRefusesActiveRecoveryBeforeDiagnosticChecks(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.MkdirAll(filepath.Join(root, ".daem", "recovery", "active-operation"), 0o700); err != nil {
		t.Fatal(err)
	}
	counts, _ := installCountingManifestLoader(t)

	result, err := runCurrent(context.Background(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
		TargetExplicit:   true,
		TargetValues:     []string{"codex"},
	})
	if err == nil || !strings.Contains(err.Error(), "daem recover --dry-run") {
		t.Fatalf("Run error = %v, want active recovery refusal", err)
	}
	if len(result.Checks) != 0 {
		t.Fatalf("active recovery ran diagnostic checks: %#v", result.Checks)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{})
}

func TestRunKeepsPlatformAdmissionIndependentFromManifestFailure(t *testing.T) {
	admission, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	result, err := Run(context.Background(), Input{
		ManifestPath:     filepath.Join(t.TempDir(), "missing.toml"),
		ManifestExplicit: true,
		TargetExplicit:   true,
		TargetValues:     []string{"codex"},
	}, admission)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	platform, ok := checkNamed(result.Checks, "platform")
	if !ok || platform.Severity != findings.SeverityError || !strings.Contains(platform.Detail, "windows/amd64") {
		t.Fatalf("platform check = %#v", platform)
	}
	manifest, ok := checkNamed(result.Checks, "manifest")
	if !ok || manifest.Severity != findings.SeverityError {
		t.Fatalf("manifest check = %#v", manifest)
	}
	if !result.HasErrors {
		t.Fatal("HasErrors = false")
	}
}

func TestRunKeepsPlatformAdmissionIndependentFromMalformedManifest(t *testing.T) {
	configureDoctorEnvironment(t)
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("targets = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	admission, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	result, err := Run(context.Background(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
		TargetExplicit:   true,
		TargetValues:     []string{"codex"},
	}, admission)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	platform, platformFound := checkNamed(result.Checks, "platform")
	manifest, manifestFound := checkNamed(result.Checks, "manifest")
	if !platformFound || platform.Severity != findings.SeverityError || !strings.Contains(platform.Detail, "windows/amd64") {
		t.Fatalf("platform check = %#v", platform)
	}
	if !manifestFound || manifest.Severity != findings.SeverityError || !strings.Contains(manifest.Detail, "parse "+manifestPath) {
		t.Fatalf("manifest check = %#v", manifest)
	}
	if !result.HasErrors {
		t.Fatal("HasErrors = false")
	}
}

func TestRunKeepsPlatformAdmissionIndependentFromPathResolutionFailure(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative-data-root")
	admission, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	result, err := Run(context.Background(), Input{
		ManifestPath:     filepath.Join(t.TempDir(), "missing.toml"),
		ManifestExplicit: true,
		TargetExplicit:   true,
		TargetValues:     []string{"codex"},
	}, admission)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	platform, platformFound := checkNamed(result.Checks, "platform")
	paths, pathsFound := checkNamed(result.Checks, "paths")
	if !platformFound || platform.Severity != findings.SeverityError || !strings.Contains(platform.Detail, "windows/amd64") {
		t.Fatalf("platform check = %#v", platform)
	}
	if !pathsFound || paths.Severity != findings.SeverityError || !strings.Contains(paths.Detail, "XDG_DATA_HOME must be an absolute path") {
		t.Fatalf("paths check = %#v", paths)
	}
	if len(result.Checks) != 2 || !result.HasErrors {
		t.Fatalf("result = %#v, want two independent errors", result)
	}
}

func runCurrent(ctx context.Context, input Input) (Result, error) {
	return Run(ctx, input, platformsupport.Current())
}

func hasCheckNamed(checks []findings.Check, name string) bool {
	_, ok := checkNamed(checks, name)
	return ok
}

func checkNamed(checks []findings.Check, name string) (findings.Check, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return findings.Check{}, false
}
