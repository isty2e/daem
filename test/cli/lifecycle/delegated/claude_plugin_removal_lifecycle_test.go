package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

func TestClaudePluginDesiredAbsenceRemovesExactScopedManagedRelation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newClaudeRemovalLifecycleFixture(t, scope)
			fixture.install(t)
			fixture.assertClaimCount(t, 1)
			fixture.removeDeclaration(t)
			fixture.remove(t)
			fixture.assertClaimCount(t, 0)
			fixture.assertRetainedBoundaries(t)

			fixture.runApply(t)
			if len(fixture.requests) != 2 {
				t.Fatalf("converged retry requests = %#v, want no reinvocation", fixture.requests)
			}
		})
	}
}

func TestClaudePluginFailedAttemptWithVerifiedAbsenceConverges(t *testing.T) {
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
				Stderr:      "host failed after removing relation",
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
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, result := range results {
			t.Run(string(scope)+"/"+result.name, func(t *testing.T) {
				fixture := newClaudeRemovalLifecycleFixture(t, scope)
				fixture.install(t)
				fixture.removeDeclaration(t)

				exitCode, stdout, stderr := fixture.runApplyWithRunner(
					t,
					func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
						fixture.assertRequest(t, request, "uninstall", true)
						fixture.writeInventory(t, false)
						return result.result
					},
				)
				if exitCode != 0 || stderr != "" {
					t.Fatalf(
						"apply exitCode=%d stdout=%q stderr=%q, want verified convergence",
						exitCode,
						stdout,
						stderr,
					)
				}
				if len(fixture.requests) != 2 {
					t.Fatalf("requests = %#v, want install and one uninstall", fixture.requests)
				}
				fixture.assertClaimCount(t, 0)
				fixture.assertPendingRemovalCount(t, 0)
				fixture.assertRetainedBoundaries(t)
			})
		}
	}
}

func TestClaudePluginMalformedPostObservationSettlesAfterRepairWithoutReinvocation(
	t *testing.T,
) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newClaudeRemovalLifecycleFixture(t, scope)
			fixture.install(t)
			fixture.removeDeclaration(t)

			exitCode, stdout, stderr := fixture.runApplyWithRunner(
				t,
				func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					fixture.assertRequest(t, request, "uninstall", true)
					testkit.WriteFile(
						t,
						filepath.Join(fixture.configRoot, "plugins"),
						"installed_plugins.json",
						`{"version":2,"plugins":null}`,
					)
					return subprocess.CommandResult{
						Started:     true,
						HasExitCode: true,
						ExitCode:    0,
					}
				},
			)
			if exitCode != 1 ||
				stderr != "" ||
				(!strings.Contains(stdout, "observation_unavailable") &&
					!strings.Contains(stdout, "postcondition")) {
				t.Fatalf(
					"malformed-observation apply exitCode=%d stdout=%q stderr=%q",
					exitCode,
					stdout,
					stderr,
				)
			}
			fixture.assertClaimCount(t, 1)
			fixture.assertPendingRemovalCount(t, 1)
			if len(fixture.requests) != 2 {
				t.Fatalf("requests = %#v, want install and one uninstall", fixture.requests)
			}

			fixture.writeInventory(t, false)
			exitCode, stdout, stderr = fixture.runApplyWithRunner(
				t,
				func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					t.Fatal("pending removal settlement reinvoked Claude")
					return subprocess.CommandResult{}
				},
			)
			if exitCode != 0 || stderr != "" {
				t.Fatalf(
					"settlement apply exitCode=%d stdout=%q stderr=%q",
					exitCode,
					stdout,
					stderr,
				)
			}
			if len(fixture.requests) != 2 {
				t.Fatalf("settlement requests = %#v, want no reinvocation", fixture.requests)
			}
			fixture.assertClaimCount(t, 0)
			fixture.assertPendingRemovalCount(t, 0)
			fixture.assertRetainedBoundaries(t)
		})
	}
}

func TestClaudeGlobalRemovalBlocksWhileAnotherDaemManifestConsumesCarrier(t *testing.T) {
	fixture := newClaudeRemovalLifecycleFixture(t, target.ScopeGlobal)
	fixture.install(t)
	claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath)
	if len(claims) != 1 {
		t.Fatalf("initial global claims = %#v, want one", claims)
	}
	otherRoot := filepath.Join(fixture.root, "other-project")
	owner, err := stateauthority.New(
		testkit.MustObservedPathAuthority(t, filepath.Join(otherRoot, ".daem", "state.json")),
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
			t.Fatal("shared global carrier removal invoked Claude")
			return subprocess.CommandResult{}
		},
	)
	if exitCode != 1 ||
		stderr != "" ||
		!strings.Contains(stdout, "remaining_daem_known_consumers") {
		t.Fatalf(
			"shared-carrier apply exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout,
			stderr,
		)
	}
	if len(fixture.requests) != 1 {
		t.Fatalf("shared-carrier requests = %#v, want install only", fixture.requests)
	}
	fixture.assertClaimCount(t, 2)
	fixture.assertPendingRemovalCount(t, 0)
}

type claudeRemovalLifecycleFixture struct {
	root         string
	manifestPath string
	configRoot   string
	scope        target.Scope
	hostScope    string
	requests     []subprocess.CommandRequest
	canaries     map[string]string
}

func newClaudeRemovalLifecycleFixture(
	t *testing.T,
	scope target.Scope,
) *claudeRemovalLifecycleFixture {
	t.Helper()
	root := t.TempDir()
	testkit.SetDataRootEnv(t, root)
	configRoot := filepath.Join(root, "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	manifestPath := filepath.Join(root, "daem.toml")
	manifest := claudeExtensionManifest()
	hostScope := "project"
	if scope == target.ScopeGlobal {
		manifest = claudeGlobalExtensionManifest()
		hostScope = "user"
	}
	testkit.WriteFile(t, root, "daem.toml", manifest)
	runExtensionAuthoringLock(t, manifestPath, filepath.Join(root, "daem.lock.toml"))

	canaries := map[string]string{
		filepath.Join(configRoot, "plugins", "known_marketplaces.json"):        "marketplace\n",
		filepath.Join(configRoot, "plugins", "cache", "context7", "plugin"):    "cached plugin\n",
		filepath.Join(configRoot, "plugins", "data", "context7", "state.json"): "persistent data\n",
		filepath.Join(configRoot, "plugins", "dependencies", "shared", "dep"):  "dependency\n",
	}
	for path, content := range canaries {
		testkit.WriteFile(t, filepath.Dir(path), filepath.Base(path), content)
	}

	return &claudeRemovalLifecycleFixture{
		root:         root,
		manifestPath: manifestPath,
		configRoot:   configRoot,
		scope:        scope,
		hostScope:    hostScope,
		canaries:     canaries,
	}
}

func (fixture *claudeRemovalLifecycleFixture) install(t *testing.T) {
	t.Helper()
	fixture.runApply(t)
	if len(fixture.requests) != 1 {
		t.Fatalf("install requests = %#v, want one", fixture.requests)
	}
	fixture.assertRequest(t, fixture.requests[0], "install", false)
}

func (fixture *claudeRemovalLifecycleFixture) removeDeclaration(t *testing.T) {
	t.Helper()
	declarationID := "context7-managed"
	if fixture.scope == target.ScopeGlobal {
		declarationID = "context7-global"
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"remove", "extension", declarationID,
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

func (fixture *claudeRemovalLifecycleFixture) remove(t *testing.T) {
	t.Helper()
	fixture.runApply(t)
	if len(fixture.requests) != 2 {
		t.Fatalf("lifecycle requests = %#v, want install and remove", fixture.requests)
	}
	fixture.assertRequest(t, fixture.requests[1], "uninstall", true)
}

func (fixture *claudeRemovalLifecycleFixture) runApply(t *testing.T) {
	t.Helper()
	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			switch {
			case len(request.Args) >= 2 && request.Args[1] == "install":
				fixture.writeInventory(t, true)
			case len(request.Args) >= 2 && request.Args[1] == "uninstall":
				fixture.writeInventory(t, false)
			default:
				t.Fatalf("unexpected Claude request: %#v", request)
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf(
			"apply exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout,
			stderr,
		)
	}
}

func (fixture *claudeRemovalLifecycleFixture) runApplyWithRunner(
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

func (fixture *claudeRemovalLifecycleFixture) writeInventory(
	t *testing.T,
	selectedPresent bool,
) {
	t.Helper()
	projectRow := map[string]string{
		"scope":       "project",
		"projectPath": fixture.root,
	}
	userRow := map[string]string{"scope": "user"}
	selectedRows := []map[string]string{userRow}
	if fixture.scope == target.ScopeGlobal {
		selectedRows = []map[string]string{projectRow}
		if selectedPresent {
			selectedRows = append(selectedRows, userRow)
		}
	} else if selectedPresent {
		selectedRows = append(selectedRows, projectRow)
	}
	document := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"context7@market": selectedRows,
			"sibling@market": []map[string]string{
				{"scope": "user"},
			},
		},
	}
	content, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(
		t,
		filepath.Join(fixture.configRoot, "plugins"),
		"installed_plugins.json",
		string(append(content, '\n')),
	)
}

func (fixture *claudeRemovalLifecycleFixture) assertRequest(
	t *testing.T,
	request subprocess.CommandRequest,
	operation string,
	keepData bool,
) {
	t.Helper()
	wantArgs := []string{
		"plugin",
		operation,
		"context7@market",
		"--scope",
		fixture.hostScope,
	}
	if keepData {
		wantArgs = append(wantArgs, "--keep-data")
	}
	if request.Command != "claude" ||
		!slices.Equal(request.Args, wantArgs) ||
		request.WorkDir != fixture.root {
		t.Fatalf("request = %#v, want claude %v in %q", request, wantArgs, fixture.root)
	}
}

func (fixture *claudeRemovalLifecycleFixture) assertClaimCount(t *testing.T, want int) {
	t.Helper()
	switch fixture.scope {
	case target.ScopeProject:
		snapshot, err := statefile.Load(
			t.Context(),
			filepath.Join(fixture.root, ".daem", "state.json"),
		)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(snapshot.ManagedCarrierClaims()); got != want {
			t.Fatalf("project claims = %d, want %d", got, want)
		}
	case target.ScopeGlobal:
		if got := len(loadCLIGlobalCarrierClaims(t, fixture.manifestPath)); got != want {
			t.Fatalf("global claims = %d, want %d", got, want)
		}
	default:
		t.Fatalf("unsupported Claude removal scope %q", fixture.scope)
	}
}

func (fixture *claudeRemovalLifecycleFixture) assertPendingRemovalCount(
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

func (fixture *claudeRemovalLifecycleFixture) assertRetainedBoundaries(t *testing.T) {
	t.Helper()
	for path, want := range fixture.canaries {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read retained Claude path %q: %v", path, err)
		}
		if string(content) != want {
			t.Fatalf("retained Claude path %q = %q, want %q", path, content, want)
		}
	}
	content, err := os.ReadFile(
		filepath.Join(fixture.configRoot, "plugins", "installed_plugins.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Plugins map[string][]map[string]string `json:"plugins"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	selected := document.Plugins["context7@market"]
	if len(selected) != 1 || selected[0]["scope"] == fixture.hostScope {
		t.Fatalf(
			"selected plugin residual rows = %#v, want only other scope",
			selected,
		)
	}
	if len(document.Plugins["sibling@market"]) != 1 {
		t.Fatalf("sibling plugin rows = %#v, want retained", document.Plugins["sibling@market"])
	}
	snapshot, err := statefile.Load(
		t.Context(),
		filepath.Join(fixture.root, ".daem", "state.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if pending := snapshot.PendingCarrierRemovals(); len(pending) != 0 {
		t.Fatalf("pending removals = %#v, want none", pending)
	}
}
