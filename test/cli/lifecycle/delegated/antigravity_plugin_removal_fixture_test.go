package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

type antigravityRemovalLifecycleFixture struct {
	root               string
	manifestPath       string
	importManifestPath string
	selectedPluginRoot string
	siblingPluginRoot  string
	credentialCanary   string
	requests           []subprocess.CommandRequest
}

func newAntigravityRemovalLifecycleFixture(
	t *testing.T,
) *antigravityRemovalLifecycleFixture {
	t.Helper()
	root := t.TempDir()
	testkit.SetDataRootEnv(t, root)
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(
		t,
		root,
		"daem.toml",
		"version = 1\ntargets = [\"antigravity-cli\"]\n",
	)
	runExtensionAuthoringCLI(
		t,
		"add", "extension", "guidance-managed", "modern-web-guidance@google",
		"--manifest", manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
	)
	runExtensionAuthoringLock(t, manifestPath, filepath.Join(root, "daem.lock.toml"))

	configRoot := filepath.Join(home, ".gemini", "config")
	return &antigravityRemovalLifecycleFixture{
		root:               root,
		manifestPath:       manifestPath,
		importManifestPath: filepath.Join(configRoot, "import_manifest.json"),
		selectedPluginRoot: filepath.Join(configRoot, "plugins", "modern-web-guidance"),
		siblingPluginRoot:  filepath.Join(configRoot, "plugins", "sibling-plugin"),
		credentialCanary:   filepath.Join(configRoot, "credentials.json"),
	}
}

func (fixture *antigravityRemovalLifecycleFixture) install(t *testing.T) {
	t.Helper()
	fixture.runApply(t)
	if len(fixture.requests) != 1 {
		t.Fatalf("install requests = %#v, want one", fixture.requests)
	}
	fixture.assertRequest(t, fixture.requests[0], "install")
}

func (fixture *antigravityRemovalLifecycleFixture) removeDeclaration(t *testing.T) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"remove", "extension", "guidance-managed",
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

func (fixture *antigravityRemovalLifecycleFixture) runApply(t *testing.T) {
	t.Helper()
	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			switch {
			case slices.Equal(
				request.Args,
				[]string{"plugin", "install", "modern-web-guidance@google"},
			):
				fixture.writeHostState(t, true)
			case slices.Equal(
				request.Args,
				[]string{"plugin", "uninstall", "modern-web-guidance"},
			):
				fixture.writeHostState(t, false)
			default:
				t.Fatalf("unexpected Antigravity request: %#v", request)
			}
			return subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    0,
			}
		},
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func (fixture *antigravityRemovalLifecycleFixture) runApplyWithRunner(
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

func (fixture *antigravityRemovalLifecycleFixture) writeHostState(
	t *testing.T,
	selected bool,
) {
	t.Helper()
	if err := os.MkdirAll(fixture.siblingPluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(fixture.siblingPluginRoot, "plugin.json"),
		[]byte(`{"name":"sibling-plugin"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		fixture.credentialCanary,
		[]byte("retain credentials\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	imports := `{"imports":[{"name":"sibling-plugin","source":"antigravity"}]}`
	if selected {
		if err := os.MkdirAll(fixture.selectedPluginRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(fixture.selectedPluginRoot, "plugin.json"),
			[]byte(`{"name":"modern-web-guidance"}`),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		imports = `{"imports":[{"name":"modern-web-guidance","source":"antigravity"},{"name":"sibling-plugin","source":"antigravity"}]}`
	} else if err := os.RemoveAll(fixture.selectedPluginRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.importManifestPath, []byte(imports), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (fixture *antigravityRemovalLifecycleFixture) assertRequest(
	t *testing.T,
	request subprocess.CommandRequest,
	operation string,
) {
	t.Helper()
	wantArgs := []string{"plugin", operation, "modern-web-guidance@google"}
	if operation == "uninstall" {
		wantArgs = []string{"plugin", operation, "modern-web-guidance"}
	}
	if request.Command != "agy" ||
		!slices.Equal(request.Args, wantArgs) ||
		request.WorkDir != fixture.root {
		t.Fatalf("request = %#v, want agy %v in %q", request, wantArgs, fixture.root)
	}
	for _, forbidden := range []string{"--help", "modern-web-guidance@google"} {
		if operation == "uninstall" && slices.Contains(request.Args, forbidden) {
			t.Fatalf("uninstall request contains forbidden argument %q: %#v", forbidden, request)
		}
	}
}

func (fixture *antigravityRemovalLifecycleFixture) assertClaimCount(
	t *testing.T,
	want int,
) {
	t.Helper()
	if got := len(loadCLIGlobalCarrierClaims(t, fixture.manifestPath)); got != want {
		t.Fatalf("global claims = %d, want %d", got, want)
	}
}

func (fixture *antigravityRemovalLifecycleFixture) assertPendingRemovalCount(
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

func (fixture *antigravityRemovalLifecycleFixture) assertSelectedPresent(t *testing.T) {
	t.Helper()
	for _, path := range []string{
		fixture.selectedPluginRoot,
		filepath.Join(fixture.selectedPluginRoot, "plugin.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("selected Antigravity path %q: %v", path, err)
		}
	}
}

func (fixture *antigravityRemovalLifecycleFixture) assertRemovalBoundaries(t *testing.T) {
	t.Helper()
	testkit.AssertPathMissing(t, fixture.selectedPluginRoot)
	for _, path := range []string{
		fixture.siblingPluginRoot,
		filepath.Join(fixture.siblingPluginRoot, "plugin.json"),
		fixture.credentialCanary,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("retained Antigravity path %q: %v", path, err)
		}
	}
	content, err := os.ReadFile(fixture.importManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte("modern-web-guidance")) ||
		!bytes.Contains(content, []byte("sibling-plugin")) {
		t.Fatalf("retained Antigravity import manifest = %q", content)
	}
}
