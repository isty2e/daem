package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestCodexPluginDesiredAbsenceRemovesExactManagedRelationAndCache(t *testing.T) {
	fixture := newCodexRemovalLifecycleFixture(t)
	fixture.install(t)
	fixture.assertClaimCount(t, 1)
	fixture.removeDeclaration(t)
	fixture.runApply(t)

	if len(fixture.requests) != 2 {
		t.Fatalf("lifecycle requests = %#v, want install and remove", fixture.requests)
	}
	fixture.assertRequest(t, fixture.requests[1], "remove")
	fixture.assertClaimCount(t, 0)
	fixture.assertPendingRemovalCount(t, 0)
	fixture.assertRemovalBoundaries(t)

	fixture.runApply(t)
	if len(fixture.requests) != 2 {
		t.Fatalf("converged retry requests = %#v, want no reinvocation", fixture.requests)
	}
}

func TestCodexPluginFailedCommandWithVerifiedAbsenceConverges(t *testing.T) {
	results := []struct {
		name   string
		result subprocess.CommandResult
	}{
		{
			name: "nonzero",
			result: subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    17,
				Stderr:      "host failed after converging",
			},
		},
		{
			name: "timeout",
			result: subprocess.CommandResult{
				Started:  true,
				TimedOut: true,
				Err:      context.DeadlineExceeded,
			},
		},
	}

	for _, result := range results {
		t.Run(result.name, func(t *testing.T) {
			fixture := newCodexRemovalLifecycleFixture(t)
			fixture.install(t)
			fixture.removeDeclaration(t)

			exitCode, stdout, stderr := fixture.runApplyWithRunner(
				t,
				func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					fixture.assertRequest(t, request, "remove")
					fixture.removeSelectedCache(t)
					fixture.writeConfig(t, false)
					return result.result
				},
			)
			if exitCode != 0 || stderr != "" {
				t.Fatalf("apply exitCode=%d stdout=%q stderr=%q, want verified convergence", exitCode, stdout, stderr)
			}
			fixture.assertClaimCount(t, 0)
			fixture.assertPendingRemovalCount(t, 0)
			fixture.assertRemovalBoundaries(t)
		})
	}
}

func TestCodexPluginRemovalRetriesIncompleteEffects(t *testing.T) {
	tests := []struct {
		name           string
		firstApply     func(*codexRemovalLifecycleFixture, *testing.T)
		reinvokesRoute bool
	}{
		{
			name: "native cache first leaves config relation",
			firstApply: func(fixture *codexRemovalLifecycleFixture, t *testing.T) {
				fixture.removeSelectedCache(t)
			},
			reinvokesRoute: true,
		},
		{
			name: "concurrent config removal leaves cache",
			firstApply: func(fixture *codexRemovalLifecycleFixture, t *testing.T) {
				fixture.writeConfig(t, false)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCodexRemovalLifecycleFixture(t)
			fixture.install(t)
			fixture.removeDeclaration(t)

			exitCode, stdout, stderr := fixture.runApplyWithRunner(
				t,
				func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					fixture.assertRequest(t, request, "remove")
					test.firstApply(fixture, t)
					return subprocess.CommandResult{
						Started:     true,
						HasExitCode: true,
						ExitCode:    0,
					}
				},
			)
			if exitCode != 1 ||
				stderr != "" ||
				(!strings.Contains(stdout, "postcondition") &&
					!strings.Contains(stdout, "observed")) {
				t.Fatalf("incomplete apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			fixture.assertClaimCount(t, 1)
			fixture.assertPendingRemovalCount(t, 1)
			if len(fixture.requests) != 2 {
				t.Fatalf("first removal requests = %#v, want install and remove", fixture.requests)
			}

			if !test.reinvokesRoute {
				exitCode, stdout, stderr = fixture.runApplyWithRunner(
					t,
					func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
						t.Fatal("observation-only pending settlement reinvoked Codex")
						return subprocess.CommandResult{}
					},
				)
				if exitCode != 1 ||
					stderr != "" ||
					!strings.Contains(stdout, "effect_postcondition_unsatisfied") {
					t.Fatalf("pending settlement exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
				}
				if len(fixture.requests) != 2 {
					t.Fatalf("pending settlement requests = %#v, want no reinvocation", fixture.requests)
				}
				fixture.assertClaimCount(t, 1)
				fixture.assertPendingRemovalCount(t, 1)

				fixture.removeSelectedCache(t)
				exitCode, stdout, stderr = fixture.runApplyWithRunner(
					t,
					func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
						t.Fatal("freshly satisfied pending settlement reinvoked Codex")
						return subprocess.CommandResult{}
					},
				)
				if exitCode != 0 || stderr != "" {
					t.Fatalf("settled apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
				}
				if len(fixture.requests) != 2 {
					t.Fatalf("settled requests = %#v, want no reinvocation", fixture.requests)
				}
				fixture.assertClaimCount(t, 0)
				fixture.assertPendingRemovalCount(t, 0)
				fixture.assertRemovalBoundaries(t)
				return
			}

			exitCode, stdout, stderr = fixture.runApplyWithRunner(
				t,
				func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					fixture.assertRequest(t, request, "remove")
					fixture.removeSelectedCache(t)
					fixture.writeConfig(t, false)
					return subprocess.CommandResult{
						Started:     true,
						HasExitCode: true,
						ExitCode:    0,
					}
				},
			)
			if exitCode != 0 || stderr != "" {
				t.Fatalf("retry apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			if len(fixture.requests) != 3 {
				t.Fatalf("retry requests = %#v, want exact removal reinvocation", fixture.requests)
			}
			fixture.assertClaimCount(t, 0)
			fixture.assertPendingRemovalCount(t, 0)
			fixture.assertRemovalBoundaries(t)
		})
	}
}

func TestCodexPluginAlreadyAbsentRetiresClaimWithoutPruningOrphanCache(t *testing.T) {
	fixture := newCodexRemovalLifecycleFixture(t)
	fixture.install(t)
	fixture.removeDeclaration(t)
	fixture.writeConfig(t, false)

	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			t.Fatal("already-absent Codex relation invoked host removal")
			return subprocess.CommandResult{}
		},
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("already-absent apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if len(fixture.requests) != 1 {
		t.Fatalf("already-absent requests = %#v, want install only", fixture.requests)
	}
	fixture.assertClaimCount(t, 0)
	fixture.assertPendingRemovalCount(t, 0)
	testkit.AssertFileContent(t, fixture.selectedCacheFile, "selected cache\n")
	fixture.assertRetainedCanaries(t)
}

func TestCodexGlobalRemovalBlocksWhileAnotherDaemManifestConsumesCarrier(t *testing.T) {
	fixture := newCodexRemovalLifecycleFixture(t)
	fixture.install(t)
	claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath)
	if len(claims) != 1 {
		t.Fatalf("initial global claims = %#v, want one", claims)
	}
	otherRoot := filepath.Join(fixture.root, "other-project")
	owner, err := stateauthority.New(
		filepath.Join(otherRoot, ".daem", "state.json"),
		filepath.Join(otherRoot, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	sharedClaim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		claims[0].Identity(),
		claims[0].InstallRequest(),
		claims[0].Provenance(),
	)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(fixture.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(t.Context(), sharedClaim); err != nil {
		t.Fatal(err)
	}
	fixture.removeDeclaration(t)

	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			t.Fatal("shared global carrier removal invoked Codex")
			return subprocess.CommandResult{}
		},
	)
	if exitCode != 1 ||
		stderr != "" ||
		!strings.Contains(stdout, "remaining_daem_known_consumers") {
		t.Fatalf("shared-carrier apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if len(fixture.requests) != 1 {
		t.Fatalf("shared-carrier requests = %#v, want install only", fixture.requests)
	}
	fixture.assertClaimCount(t, 2)
	fixture.assertPendingRemovalCount(t, 0)
}

func TestCodexPluginUnmanageRetainsHostRelationAndCache(t *testing.T) {
	fixture := newCodexRemovalLifecycleFixture(t)
	fixture.install(t)
	canary := execcheck.New(t, "codex")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"unmanage", "extension", "documents",
			"--manifest", fixture.manifestPath,
			"--target", "codex",
			"--scope", "global",
			"--json",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("unmanage exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	fixture.assertClaimCount(t, 0)
	fixture.assertPendingRemovalCount(t, 0)
	fixture.assertSelectedPresent(t)
	fixture.assertRetainedCanaries(t)
	execcheck.AssertClean(t, canary, "Codex unmanage")
}

type codexRemovalLifecycleFixture struct {
	root              string
	manifestPath      string
	codexHome         string
	configPath        string
	selectedCacheRoot string
	selectedCacheFile string
	requests          []subprocess.CommandRequest
	canaries          map[string]string
}

func newCodexRemovalLifecycleFixture(t *testing.T) *codexRemovalLifecycleFixture {
	t.Helper()
	root := t.TempDir()
	testkit.SetDataRootEnv(t, root)
	codexHome := filepath.Join(root, "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", codexGlobalRemovalManifest())
	runExtensionAuthoringLock(t, manifestPath, filepath.Join(root, "daem.lock.toml"))

	selectedCacheRoot := filepath.Join(
		codexHome,
		"plugins",
		"cache",
		"openai-primary-runtime",
		"documents",
	)
	selectedCacheFile := filepath.Join(selectedCacheRoot, "1.0.0", "plugin.txt")
	canaries := map[string]string{
		filepath.Join(codexHome, "plugins", "cache", "private", "documents", "2.0.0", "plugin.txt"):              "other marketplace cache\n",
		filepath.Join(codexHome, "plugins", "cache", "openai-primary-runtime", "sibling", "3.0.0", "plugin.txt"): "sibling cache\n",
		filepath.Join(codexHome, "plugins", "marketplaces", "openai-primary-runtime", "marketplace.json"):        "marketplace snapshot\n",
		filepath.Join(codexHome, "credentials.json"):                                                             "credentials\n",
	}
	for path, content := range canaries {
		testkit.WriteFile(t, filepath.Dir(path), filepath.Base(path), content)
	}
	fixture := &codexRemovalLifecycleFixture{
		root:              root,
		manifestPath:      manifestPath,
		codexHome:         codexHome,
		configPath:        filepath.Join(codexHome, "config.toml"),
		selectedCacheRoot: selectedCacheRoot,
		selectedCacheFile: selectedCacheFile,
		canaries:          canaries,
	}
	fixture.writeConfig(t, false)
	return fixture
}

func codexGlobalRemovalManifest() string {
	return `
version = 1
targets = ["codex"]

[[extension]]
id = "documents"
carrier = "codex-plugin"
scope = "global"
source = { marketplace = "documents@openai-primary-runtime" }
`
}

func (fixture *codexRemovalLifecycleFixture) install(t *testing.T) {
	t.Helper()
	fixture.runApply(t)
	if len(fixture.requests) != 1 {
		t.Fatalf("install requests = %#v, want one", fixture.requests)
	}
	fixture.assertRequest(t, fixture.requests[0], "add")
}

func (fixture *codexRemovalLifecycleFixture) removeDeclaration(t *testing.T) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"remove", "extension", "documents",
			"--manifest", fixture.manifestPath,
			"--json",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"remove declaration exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
}

func (fixture *codexRemovalLifecycleFixture) runApply(t *testing.T) {
	t.Helper()
	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			switch {
			case len(request.Args) >= 2 && request.Args[1] == "add":
				fixture.writeConfig(t, true)
				testkit.WriteFile(
					t,
					filepath.Dir(fixture.selectedCacheFile),
					filepath.Base(fixture.selectedCacheFile),
					"selected cache\n",
				)
			case len(request.Args) >= 2 && request.Args[1] == "remove":
				fixture.removeSelectedCache(t)
				fixture.writeConfig(t, false)
			default:
				t.Fatalf("unexpected Codex request: %#v", request)
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func (fixture *codexRemovalLifecycleFixture) runApplyWithRunner(
	t *testing.T,
	runner subprocess.CommandRunner,
) (int, string, string) {
	t.Helper()
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			fixture.requests = append(fixture.requests, request)
			return runner(ctx, request)
		},
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLIWithOptions(
		[]string{"apply", "--manifest", fixture.manifestPath, "--yes", "--json"},
		clipkg.RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				HostRouteExecutor: executor,
			},
		},
	)
	return exitCode, stdout.String(), stderr.String()
}

func (fixture *codexRemovalLifecycleFixture) writeConfig(
	t *testing.T,
	selectedPresent bool,
) {
	t.Helper()
	selected := ""
	if selectedPresent {
		selected = `
[plugins."documents@openai-primary-runtime"]
enabled = true
`
	}
	content := `model = "gpt-test"

[marketplaces.openai-primary-runtime]
source = "https://example.invalid/openai-primary-runtime.git"
` + selected + `
[plugins."documents@private"]
enabled = false

[plugins."sibling@openai-primary-runtime"]
`
	testkit.WriteFile(t, fixture.codexHome, "config.toml", content)
}

func (fixture *codexRemovalLifecycleFixture) removeSelectedCache(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(fixture.selectedCacheRoot); err != nil {
		t.Fatal(err)
	}
}

func (fixture *codexRemovalLifecycleFixture) assertRequest(
	t *testing.T,
	request subprocess.CommandRequest,
	operation string,
) {
	t.Helper()
	wantArgs := []string{
		"plugin",
		operation,
		"documents@openai-primary-runtime",
		"--json",
	}
	if request.Command != "codex" ||
		!slices.Equal(request.Args, wantArgs) ||
		request.WorkDir != fixture.root {
		t.Fatalf("request = %#v, want codex %v in %q", request, wantArgs, fixture.root)
	}
}

func (fixture *codexRemovalLifecycleFixture) assertClaimCount(t *testing.T, want int) {
	t.Helper()
	if got := len(loadCLIGlobalCarrierClaims(t, fixture.manifestPath)); got != want {
		t.Fatalf("global claims = %d, want %d", got, want)
	}
}

func (fixture *codexRemovalLifecycleFixture) assertPendingRemovalCount(
	t *testing.T,
	want int,
) {
	t.Helper()
	snapshot, err := statefile.Load(
		t.Context(),
		filepath.Join(fixture.root, ".daem", "state.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(snapshot.PendingCarrierRemovals()); got != want {
		t.Fatalf("pending removals = %d, want %d", got, want)
	}
}

func (fixture *codexRemovalLifecycleFixture) assertRemovalBoundaries(t *testing.T) {
	t.Helper()
	testkit.AssertPathMissing(t, fixture.selectedCacheRoot)
	fixture.assertRetainedCanaries(t)

	var document map[string]any
	if _, err := toml.DecodeFile(fixture.configPath, &document); err != nil {
		t.Fatal(err)
	}
	plugins, ok := document["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("Codex plugins config = %#v, want table", document["plugins"])
	}
	if _, present := plugins["documents@openai-primary-runtime"]; present {
		t.Fatalf("selected Codex plugin relation remains: %#v", plugins)
	}
	for _, key := range []string{
		"documents@private",
		"sibling@openai-primary-runtime",
	} {
		if _, present := plugins[key]; !present {
			t.Fatalf("retained Codex plugin relation %q is absent: %#v", key, plugins)
		}
	}
	if document["model"] != "gpt-test" {
		t.Fatalf("unrelated Codex config model = %#v, want retained", document["model"])
	}
	marketplaces, ok := document["marketplaces"].(map[string]any)
	if !ok {
		t.Fatalf("Codex marketplaces config = %#v, want retained table", document["marketplaces"])
	}
	if _, present := marketplaces["openai-primary-runtime"]; !present {
		t.Fatalf("selected marketplace registration is absent: %#v", marketplaces)
	}
}

func (fixture *codexRemovalLifecycleFixture) assertSelectedPresent(t *testing.T) {
	t.Helper()
	testkit.AssertFileContent(t, fixture.selectedCacheFile, "selected cache\n")

	var document map[string]any
	if _, err := toml.DecodeFile(fixture.configPath, &document); err != nil {
		t.Fatal(err)
	}
	plugins, ok := document["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("Codex plugins config = %#v, want table", document["plugins"])
	}
	if _, present := plugins["documents@openai-primary-runtime"]; !present {
		t.Fatalf("selected Codex plugin relation is absent: %#v", plugins)
	}
}

func (fixture *codexRemovalLifecycleFixture) assertRetainedCanaries(t *testing.T) {
	t.Helper()
	for path, content := range fixture.canaries {
		testkit.AssertFileContent(t, path, content)
	}
}
