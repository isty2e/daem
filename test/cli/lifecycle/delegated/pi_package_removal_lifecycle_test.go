package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

func TestPiPackageDesiredAbsenceRemovesExactManagedSource(t *testing.T) {
	tests := []struct {
		name       string
		sourceKind string
		source     func(string) string
	}{
		{
			name:       "npm",
			sourceKind: "npm",
			source:     func(string) string { return "npm:pi-tools@1.2.3" },
		},
		{
			name:       "git",
			sourceKind: "git",
			source:     func(string) string { return "git:github.com/acme/pi-tools@v1" },
		},
		{
			name:       "local",
			sourceKind: "local",
			source: func(root string) string {
				return filepath.Join(root, "local-pi-tools")
			},
		},
	}

	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, test := range tests {
			t.Run(string(scope)+"/"+test.name, func(t *testing.T) {
				fixture := newPiRemovalLifecycleFixture(
					t,
					scope,
					test.sourceKind,
					test.source,
				)
				fixture.install(t)
				fixture.assertClaimCount(t, 1)
				fixture.removeDeclaration(t)
				fixture.remove(t)
				fixture.assertClaimCount(t, 0)
				fixture.assertRetainedBoundaries(t)
			})
		}
	}
}

type piRemovalLifecycleFixture struct {
	root              string
	manifestPath      string
	scope             target.Scope
	sourceKind        string
	source            string
	lockedSource      string
	selectedSettings  string
	otherSettings     string
	selectedStoredRef string
	artifactPath      string
	localCanary       string
	requests          []subprocess.CommandRequest
}

func newPiRemovalLifecycleFixture(
	t *testing.T,
	scope target.Scope,
	sourceKind string,
	source func(string) string,
) *piRemovalLifecycleFixture {
	t.Helper()
	root := t.TempDir()
	testkit.SetDataRootEnv(t, root)
	agentRoot := filepath.Join(root, "pi-global")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
	sourceRef := source(root)
	if sourceKind == "local" {
		localCanary := filepath.Join(sourceRef, "canary.txt")
		if err := os.MkdirAll(sourceRef, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(localCanary, []byte("retain me\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runExtensionAuthoringCLI(
		t,
		"add", "extension", "tools-managed", sourceRef,
		"--manifest", manifestPath,
		"--target", "pi",
		"--scope", string(scope),
	)
	runExtensionAuthoringLock(t, manifestPath, filepath.Join(root, "daem.lock.toml"))

	projectSettings := filepath.Join(root, ".pi", "settings.json")
	globalSettings := filepath.Join(agentRoot, "settings.json")
	selectedSettings := projectSettings
	otherSettings := globalSettings
	if scope == target.ScopeGlobal {
		selectedSettings, otherSettings = globalSettings, projectSettings
	}
	selectedStoredRef := sourceRef
	lockedSource := sourceRef
	if sourceKind == "local" {
		relative, err := filepath.Rel(filepath.Dir(selectedSettings), sourceRef)
		if err != nil {
			t.Fatal(err)
		}
		selectedStoredRef = relative
		if scope == target.ScopeProject {
			lockedSource, err = filepath.Rel(filepath.Dir(manifestPath), sourceRef)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	writePiPackageSettings(t, otherSettings, []string{sourceRef})

	fixture := &piRemovalLifecycleFixture{
		root:              root,
		manifestPath:      manifestPath,
		scope:             scope,
		sourceKind:        sourceKind,
		source:            sourceRef,
		lockedSource:      lockedSource,
		selectedSettings:  selectedSettings,
		otherSettings:     otherSettings,
		selectedStoredRef: selectedStoredRef,
	}
	fixture.artifactPath = fixture.managedArtifactPath()
	if sourceKind == "local" {
		fixture.localCanary = filepath.Join(sourceRef, "canary.txt")
	}
	return fixture
}

func (fixture *piRemovalLifecycleFixture) install(t *testing.T) {
	t.Helper()
	fixture.runApply(t)
	if len(fixture.requests) != 1 {
		t.Fatalf("install requests = %#v, want one", fixture.requests)
	}
	fixture.assertRequest(t, fixture.requests[0], "install")
	if fixture.sourceKind != "local" {
		if _, err := os.Stat(fixture.artifactPath); err != nil {
			t.Fatalf("installed artifact %q: %v", fixture.artifactPath, err)
		}
	}
}

func (fixture *piRemovalLifecycleFixture) removeDeclaration(t *testing.T) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"remove", "extension", "tools-managed",
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

func (fixture *piRemovalLifecycleFixture) remove(t *testing.T) {
	t.Helper()
	fixture.runApply(t)
	if len(fixture.requests) != 2 {
		t.Fatalf("lifecycle requests = %#v, want install and remove", fixture.requests)
	}
	fixture.assertRequest(t, fixture.requests[1], "remove")
	if fixture.artifactPath != "" {
		if _, err := os.Stat(fixture.artifactPath); !os.IsNotExist(err) {
			t.Fatalf("removed artifact stat error = %v, want absent", err)
		}
	}
}

func (fixture *piRemovalLifecycleFixture) runApply(t *testing.T) {
	t.Helper()
	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			fixture.applyNativeEffect(t, request)
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

func (fixture *piRemovalLifecycleFixture) runApplyWithRunner(
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

func (fixture *piRemovalLifecycleFixture) applyNativeEffect(
	t *testing.T,
	request subprocess.CommandRequest,
) {
	t.Helper()
	switch {
	case len(request.Args) >= 2 && request.Args[0] == "install":
		writePiPackageSettings(
			t,
			fixture.selectedSettings,
			[]string{fixture.selectedStoredRef, "npm:unrelated@9"},
		)
		if fixture.artifactPath != "" {
			if err := os.MkdirAll(fixture.artifactPath, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				filepath.Join(fixture.artifactPath, "package.json"),
				[]byte("{}\n"),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
		}
	case len(request.Args) >= 2 && request.Args[0] == "remove":
		fixture.makeSelectedSourceAbsent(t)
	default:
		t.Fatalf("unexpected Pi request: %#v", request)
	}
}

func (fixture *piRemovalLifecycleFixture) makeSelectedSourceAbsent(t *testing.T) {
	t.Helper()
	writePiPackageSettings(
		t,
		fixture.selectedSettings,
		[]string{"npm:unrelated@9"},
	)
	if fixture.artifactPath != "" {
		if err := os.RemoveAll(fixture.artifactPath); err != nil {
			t.Fatal(err)
		}
	}
}

func (fixture *piRemovalLifecycleFixture) assertRequest(
	t *testing.T,
	request subprocess.CommandRequest,
	operation string,
) {
	t.Helper()
	wantArgs := []string{operation, fixture.lockedSource}
	if fixture.scope == target.ScopeProject {
		wantArgs = append(wantArgs, "-l")
	}
	if request.Command != "pi" ||
		!slices.Equal(request.Args, wantArgs) ||
		request.WorkDir != fixture.root {
		t.Fatalf(
			"request = %#v, want pi %v in %q",
			request,
			wantArgs,
			fixture.root,
		)
	}
}

func (fixture *piRemovalLifecycleFixture) assertClaimCount(t *testing.T, want int) {
	t.Helper()
	switch fixture.scope {
	case target.ScopeProject:
		snapshot, err := statefile.Load(t.Context(), filepath.Join(fixture.root, ".daem", "state.json"))
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
		t.Fatalf("unsupported test scope %q", fixture.scope)
	}
}

func (fixture *piRemovalLifecycleFixture) assertPendingRemovalCount(t *testing.T, want int) {
	t.Helper()
	snapshot, err := statefile.Load(t.Context(), filepath.Join(fixture.root, ".daem", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(snapshot.PendingCarrierRemovals()); got != want {
		t.Fatalf("pending removals = %d, want %d", got, want)
	}
}

func (fixture *piRemovalLifecycleFixture) assertRetainedBoundaries(t *testing.T) {
	t.Helper()
	selected := readPiPackageSettings(t, fixture.selectedSettings)
	if !slices.Equal(selected, []string{"npm:unrelated@9"}) {
		t.Fatalf("selected settings packages = %#v, want sibling only", selected)
	}
	other := readPiPackageSettings(t, fixture.otherSettings)
	if !slices.Equal(other, []string{fixture.source}) {
		t.Fatalf("other-scope settings packages = %#v, want exact shadow retained", other)
	}
	if fixture.localCanary != "" {
		content, err := os.ReadFile(fixture.localCanary)
		if err != nil {
			t.Fatalf("read retained local source canary: %v", err)
		}
		if string(content) != "retain me\n" {
			t.Fatalf("local source canary = %q, want unchanged", content)
		}
	}
	snapshot, err := statefile.Load(t.Context(), filepath.Join(fixture.root, ".daem", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if pending := snapshot.PendingCarrierRemovals(); len(pending) != 0 {
		t.Fatalf("pending removals = %#v, want none", pending)
	}
}

func (fixture *piRemovalLifecycleFixture) managedArtifactPath() string {
	base := filepath.Dir(fixture.selectedSettings)
	switch fixture.sourceKind {
	case "npm":
		return filepath.Join(base, "npm", "node_modules", "pi-tools")
	case "git":
		return filepath.Join(base, "git", "github.com", "acme", "pi-tools")
	case "local":
		return ""
	default:
		panic("unsupported test Pi source kind " + fixture.sourceKind)
	}
}

func writePiPackageSettings(t *testing.T, path string, packages []string) {
	t.Helper()
	content, err := json.Marshal(map[string][]string{"packages": packages})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readPiPackageSettings(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	return document.Packages
}
