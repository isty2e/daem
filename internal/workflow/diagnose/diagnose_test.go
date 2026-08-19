package diagnoseworkflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/platformsupport"
	"github.com/isty2e/daem/test/testkit/doctorenv"
	"github.com/isty2e/daem/test/testkit/metadatatx"
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

func TestRunRefusesMalformedRecoveryBeforeDiagnosticChecks(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "recovery inventory is blocked") {
		t.Fatalf("Run error = %v, want malformed recovery refusal", err)
	}
	if len(result.Checks) != 0 {
		t.Fatalf("blocked recovery ran diagnostic checks: %#v", result.Checks)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{})
}

func TestRunUnsupportedPlatformReportsNamedRemainingChecks(t *testing.T) {
	configureDoctorEnvironment(t)
	admission, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "missing.toml")
	result, err := Run(context.Background(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
		TargetExplicit:   true,
		TargetValues:     []string{"codex"},
	}, testPlatformAssessment(admission))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertCheckStatus(t, result.Checks, "platform", findings.CheckError)
	assertCheckStatus(t, result.Checks, "file_set", findings.CheckUnsupported)
	assertCheckStatus(t, result.Checks, "recovery", findings.CheckUnsupported)
	assertCheckStatus(t, result.Checks, "manifest", findings.CheckError)
	git, ok := checkNamed(result.Checks, "git")
	if !ok {
		t.Fatalf("checks = %#v, want git", result.Checks)
	}
	if git.Status == findings.CheckSkipped || git.Status == findings.CheckUnsupported {
		t.Fatalf("git = %#v, want an attempted tool-presence check", git)
	}
	assertCheckStatus(t, result.Checks, "cache", findings.CheckUnsupported)
	assertCheckStatus(t, result.Checks, "symlink", findings.CheckUnsupported)
	assertCheckStatus(t, result.Checks, "target=codex capability=hook", findings.CheckOK)
	assertCheckStatus(t, result.Checks, "target=codex config_dir", findings.CheckUnsupported)
	assertCheckStatus(t, result.Checks, "codex_plugin", findings.CheckUnsupported)
	if hasCheckNamed(result.Checks, "paths") {
		t.Fatalf("path failure leaked into remaining checks: %#v", result.Checks)
	}
	if hasCheckNamed(result.Checks, "skill_observation") {
		t.Fatalf("missing manifest invented skill observation: %#v", result.Checks)
	}
	absoluteManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.ManifestPath != absoluteManifestPath {
		t.Fatalf(
			"ManifestPath = %q, want %q",
			result.ManifestPath,
			absoluteManifestPath,
		)
	}
	if !result.HasErrors {
		t.Fatal("HasErrors = false")
	}
}

func TestRunUnsupportedPlatformSkipsStorageGatesAndRunsManifestSyntax(t *testing.T) {
	configureDoctorEnvironment(t)
	admission, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}

	tests := []struct {
		name  string
		setup func(*testing.T, daempaths.Paths)
	}{
		{
			name: "interrupted metadata transaction",
			setup: func(t *testing.T, paths daempaths.Paths) {
				t.Helper()
				metadatatx.WriteInterrupted(t, paths.StateDir)
			},
		},
		{
			name: "blocked recovery inventory",
			setup: func(t *testing.T, paths daempaths.Paths) {
				t.Helper()
				if err := os.MkdirAll(
					filepath.Join(paths.RecoveryDir, "active-operation"),
					0o700,
				); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := filepath.Join(t.TempDir(), "daem.toml")
			if err := os.WriteFile(manifestPath, []byte("targets = [\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			paths, err := daempaths.Resolve(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			test.setup(t, paths)
			counts, _ := installCountingManifestLoader(t)

			result, err := Run(context.Background(), Input{
				ManifestPath:     manifestPath,
				ManifestExplicit: true,
				TargetExplicit:   true,
				TargetValues:     []string{"codex"},
			}, testPlatformAssessment(admission))
			if err != nil {
				t.Fatalf("Run returned error: %v, storage gates must not abort doctor", err)
			}
			assertManifestStageCounts(t, counts, manifestStageCounts{read: 1, normalize: 1})
			if result.ManifestPath != paths.ManifestPath {
				t.Fatalf(
					"ManifestPath = %q, want resolved path %q",
					result.ManifestPath,
					paths.ManifestPath,
				)
			}
			assertCheckStatus(t, result.Checks, "platform", findings.CheckError)
			assertCheckStatus(t, result.Checks, "file_set", findings.CheckUnsupported)
			assertCheckStatus(t, result.Checks, "recovery", findings.CheckUnsupported)
			assertCheckStatus(t, result.Checks, "manifest", findings.CheckError)
			manifest, _ := checkNamed(result.Checks, "manifest")
			if !strings.Contains(manifest.Detail, "parse "+paths.ManifestPath) {
				t.Fatalf("manifest check = %#v, want parse error", manifest)
			}
			if !result.HasErrors {
				t.Fatal("HasErrors = false")
			}
		})
	}
}

func TestRunUnsupportedPlatformDistinguishesManifestSyntaxFromHostObservation(t *testing.T) {
	configureDoctorEnvironment(t)
	admission, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeDoctorManifest(t, manifestPath, `
version = 1
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex"]
`)
	if err := os.MkdirAll(filepath.Join(root, ".daem", "recovery", "active-operation"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
		TargetExplicit:   true,
		TargetValues:     []string{"codex"},
	}, testPlatformAssessment(admission))
	if err != nil {
		t.Fatalf("Run returned error: %v, blocked recovery must not abort unsupported-platform doctor", err)
	}
	assertCheckStatus(t, result.Checks, "platform", findings.CheckError)
	assertCheckStatus(t, result.Checks, "manifest", findings.CheckOK)
	assertCheckStatus(t, result.Checks, "skill_observation", findings.CheckSkipped)
	assertCheckStatus(t, result.Checks, "cache", findings.CheckUnsupported)
	assertCheckStatus(t, result.Checks, "codex_plugin", findings.CheckUnsupported)
	if hasCheckNamed(result.Checks, "target=codex skill=review compatibility") {
		t.Fatalf("compatibility check ran on unsupported platform: %#v", result.Checks)
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
	}, testPlatformAssessment(admission))
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	platform, platformFound := checkNamed(result.Checks, "platform")
	paths, pathsFound := checkNamed(result.Checks, "paths")
	if !platformFound || platform.Status != findings.CheckError || !strings.Contains(platform.Detail, "windows/amd64") {
		t.Fatalf("platform check = %#v", platform)
	}
	if !pathsFound || paths.Status != findings.CheckError || !strings.Contains(paths.Detail, "XDG_DATA_HOME must be an absolute path") {
		t.Fatalf("paths check = %#v", paths)
	}
	if len(result.Checks) != 2 || !result.HasErrors {
		t.Fatalf("result = %#v, want two independent errors", result)
	}
}

func runCurrent(ctx context.Context, input Input) (Result, error) {
	return Run(ctx, input, testPlatformAssessment(platformsupport.Current()))
}

func testPlatformAssessment(admission platformsupport.Admission) platformsupport.PlatformAssessment {
	minimum, required := admission.RuntimeRequirement()
	if !required {
		return platformsupport.AssessRuntime(admission, platformsupport.RuntimeObservation{})
	}
	observation, err := platformsupport.NewRuntimeObservation(minimum)
	if err != nil {
		panic(err)
	}
	return platformsupport.AssessRuntime(admission, observation)
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

func assertCheckStatus(t *testing.T, checks []findings.Check, name string, want findings.CheckStatus) {
	t.Helper()
	check, ok := checkNamed(checks, name)
	if !ok {
		t.Fatalf("checks = %#v, want %s", checks, name)
	}
	if check.Status != want {
		t.Fatalf("%s status = %s, want %s (%#v)", name, check.Status, want, check)
	}
}
