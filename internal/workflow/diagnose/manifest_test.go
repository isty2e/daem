package diagnoseworkflow

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

func TestRunLoadsValidManifestPipelineExactlyOnce(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeDoctorManifest(t, manifestPath, "version = 1\ntargets = [\"codex\"]\n")
	configureDoctorEnvironment(t)
	counts, _ := installCountingManifestLoader(t)

	result, err := runCurrent(t.Context(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{read: 1, normalize: 1, buildFacts: 1})
	if got := result.Selection.Targets(); len(got) != 1 || got[0] != target.TargetCodex {
		t.Fatalf("Selection = %#v, want codex", got)
	}
	manifest, ok := checkNamed(result.Checks, "manifest")
	if !ok || manifest.Status != findings.CheckOK {
		t.Fatalf("manifest check = %#v, want parseable manifest", result.Checks)
	}
	if result.HasErrors {
		t.Fatalf("HasErrors = true, checks = %#v", result.Checks)
	}
}

func TestRunMissingImplicitManifestStopsAfterOneRead(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "missing.toml")
	configureDoctorEnvironment(t)
	counts, _ := installCountingManifestLoader(t)

	result, err := runCurrent(t.Context(), Input{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{read: 1})
	manifest, ok := checkNamed(result.Checks, "manifest")
	if !ok || manifest.Status != findings.CheckWarn {
		t.Fatalf("manifest check = %#v, want missing-manifest warning", result.Checks)
	}
	if !strings.Contains(manifest.Detail, "not found; running general diagnostics") {
		t.Fatalf("manifest detail = %q", manifest.Detail)
	}
	if got := result.Selection.Targets(); len(got) != len(target.SupportedTargets()) {
		t.Fatalf("Selection = %#v, want all supported targets", got)
	}
	if result.HasErrors {
		t.Fatalf("HasErrors = true, checks = %#v", result.Checks)
	}
}

func TestRunMalformedManifestStopsBeforeFactConstruction(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	writeDoctorManifest(t, manifestPath, "version = \"wrong\"\n")
	configureDoctorEnvironment(t)
	counts, _ := installCountingManifestLoader(t)

	result, err := runCurrent(t.Context(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{read: 1, normalize: 1})
	manifest, ok := checkNamed(result.Checks, "manifest")
	if !ok || manifest.Status != findings.CheckError {
		t.Fatalf("manifest check = %#v, want manifest error", result.Checks)
	}
	if !strings.HasPrefix(manifest.Detail, "parse "+manifestPath+":") {
		t.Fatalf("manifest detail = %q, want parse prefix", manifest.Detail)
	}
	if got := result.Selection.Targets(); len(got) != len(target.SupportedTargets()) {
		t.Fatalf("Selection = %#v, want all supported targets", got)
	}
	if !hasCheckNamed(result.Checks, "git") {
		t.Fatalf("general diagnostics did not continue: %#v", result.Checks)
	}
}

func TestRunFactConstructionFailureUsesOneParseFailureBasis(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	writeDoctorManifest(t, manifestPath, `
version = 1
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex"]

[[hook]]
name = "stop"
event = "Stop"
type = "command"
command = "true"
targets = ["codex"]

[[mcp_server]]
name = "tools"
transport = "stdio"
command = "node"
targets = ["codex"]
`)
	configureDoctorEnvironment(t)
	counts, _ := installCountingManifestLoader(t)
	doctorManifestLoader.buildFacts = func(desired.Environment) (diagnose.ManifestFacts, error) {
		counts.buildFacts++
		return diagnose.ManifestFacts{}, errors.New("injected fact construction failure")
	}

	result, err := runCurrent(t.Context(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{read: 1, normalize: 1, buildFacts: 1})
	manifest, ok := checkNamed(result.Checks, "manifest")
	if !ok || !strings.Contains(manifest.Detail, "injected fact construction failure") {
		t.Fatalf("manifest check = %#v, want injected failure", result.Checks)
	}
	for _, check := range result.Checks {
		if strings.Contains(check.Name, " skill=") || strings.Contains(check.Name, " hook=") || strings.Contains(check.Name, " mcp_server=") {
			t.Fatalf("manifest-backed check ran after fact failure: %#v", check)
		}
	}
	if !hasCheckNamed(result.Checks, "git") {
		t.Fatalf("general diagnostics did not continue: %#v", result.Checks)
	}
}

func TestManifestLoaderClassifiesPostStatReadFailureAsReadEvidence(t *testing.T) {
	postStatReadFailure := manifestLoader{
		readFile: func(context.Context, string) ([]byte, error) {
			return nil, fs.ErrNotExist
		},
		normalize: func([]byte) (desired.Environment, error) {
			panic("normalize called after read failure")
		},
		buildFacts: func(desired.Environment) (diagnose.ManifestFacts, error) {
			panic("buildFacts called after read failure")
		},
	}.load(t.Context(), "replaced.toml")
	if postStatReadFailure.stage != manifestLoadStageReadFailure {
		t.Fatalf("stage = %d, want read failure", postStatReadFailure.stage)
	}
	if check := manifestCheck("replaced.toml", true, postStatReadFailure); check.Status != findings.CheckError || !strings.HasPrefix(check.Detail, "read replaced.toml:") {
		t.Fatalf("check = %#v, want read failure", check)
	}

	readFailure := manifestLoader{
		readFile: func(context.Context, string) ([]byte, error) {
			return nil, fs.ErrPermission
		},
	}.load(t.Context(), "blocked.toml")
	if readFailure.stage != manifestLoadStageReadFailure {
		t.Fatalf("stage = %d, want read failure", readFailure.stage)
	}
	if check := manifestCheck("blocked.toml", true, readFailure); check.Status != findings.CheckError || !strings.HasPrefix(check.Detail, "read blocked.toml:") {
		t.Fatalf("check = %#v, want read failure", check)
	}
}

func TestManifestCheckDistinguishesImplicitAndExplicitMissing(t *testing.T) {
	missing := manifestLoad{stage: manifestLoadStageReadFailure, err: fs.ErrNotExist}

	implicit := manifestCheck("missing.toml", false, missing)
	if implicit.Status != findings.CheckWarn || !strings.Contains(implicit.Detail, "running general diagnostics") {
		t.Fatalf("implicit check = %#v, want warning", implicit)
	}
	explicit := manifestCheck("missing.toml", true, missing)
	if explicit.Status != findings.CheckError || !strings.HasPrefix(explicit.Detail, "read missing.toml:") {
		t.Fatalf("explicit check = %#v, want read error", explicit)
	}
}

func BenchmarkDoctorManifestLoadLarge(b *testing.B) {
	manifestPath := filepath.Join(b.TempDir(), "daem.toml")
	if err := os.WriteFile(manifestPath, largeDoctorManifest(512), 0o600); err != nil {
		b.Fatalf("write manifest: %v", err)
	}
	loader := defaultDoctorManifestLoader()

	for _, benchmark := range []struct {
		name   string
		passes int
	}{
		{name: "single-pass", passes: 1},
		{name: "six-pass-reference", passes: 6},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for iteration := 0; iteration < b.N; iteration++ {
				for pass := 0; pass < benchmark.passes; pass++ {
					loaded := loader.load(b.Context(), manifestPath)
					if !loaded.ready() {
						b.Fatalf("load failed at stage %d: %v", loaded.stage, loaded.err)
					}
				}
			}
		})
	}
}

type manifestStageCounts struct {
	read       int
	normalize  int
	buildFacts int
}

func installCountingManifestLoader(t *testing.T) (*manifestStageCounts, manifestLoader) {
	t.Helper()

	original := doctorManifestLoader
	counts := &manifestStageCounts{}
	doctorManifestLoader = manifestLoader{
		readFile: func(ctx context.Context, path string) ([]byte, error) {
			counts.read++
			return original.readFile(ctx, path)
		},
		normalize: func(content []byte) (desired.Environment, error) {
			counts.normalize++
			return original.normalize(content)
		},
		buildFacts: func(normalized desired.Environment) (diagnose.ManifestFacts, error) {
			counts.buildFacts++
			return original.buildFacts(normalized)
		},
	}
	t.Cleanup(func() {
		doctorManifestLoader = original
	})

	return counts, original
}

func assertManifestStageCounts(t *testing.T, got *manifestStageCounts, want manifestStageCounts) {
	t.Helper()
	if *got != want {
		t.Fatalf("manifest stage counts = %+v, want %+v", *got, want)
	}
}

func configureDoctorEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	doctorenv.WithFakeGit(t, "git version test")
}

func writeDoctorManifest(t testing.TB, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func largeDoctorManifest(skillCount int) []byte {
	var manifest strings.Builder
	manifest.WriteString("version = 1\ntargets = [\"codex\"]\n")
	for index := range skillCount {
		fmt.Fprintf(&manifest, `

[[skill]]
id = "skill-%04d"
name = "skill-%04d"
source = { path = "skills/skill-%04d", mode = "vendor" }
targets = ["codex"]
`, index, index, index)
	}
	return []byte(manifest.String())
}
