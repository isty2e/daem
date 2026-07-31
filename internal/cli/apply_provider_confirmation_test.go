package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestRunApplyYesStopsOnProviderOrderRiskExpansion(t *testing.T) {
	fixture := writeApplyProviderConfirmationFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithoutTerminal(
		[]string{"lock", "--manifest", fixture.manifestPath},
		&stdout,
		&stderr,
	); exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"lock exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	stdout.Reset()
	stderr.Reset()

	runnerCalls := 0
	options := RunOptions{
		Context: t.Context(),
		Stdout:  &stdout,
		Stderr:  &stderr,
		ApplyExecuteOptions: applyworkflow.ExecuteOptions{
			HostRouteExecutor: fixture.providerExecutor(t, &runnerCalls),
		},
	}

	exitCode := RunWithOptions(
		[]string{
			"apply", "--manifest", fixture.manifestPath, "--manage-existing", "--yes",
		},
		options,
	)
	if exitCode != 1 || runnerCalls != 1 {
		t.Fatalf(
			"exitCode=%d runnerCalls=%d stdout=%q stderr=%q",
			exitCode,
			runnerCalls,
			stdout.String(),
			stderr.String(),
		)
	}
	for _, want := range []string{
		"extension order risk expanded after carrier changes",
		"inspect a fresh dry-run",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr lacks %q:\n%s", want, stderr.String())
		}
	}
	if strings.Contains(stdout.String(), "Proceed with") ||
		strings.Contains(stderr.String(), "Proceed with") {
		t.Fatalf(
			"--yes prompted for expanded consent: stdout=%q stderr=%q",
			stdout.String(),
			stderr.String(),
		)
	}
	fixture.assertSettings(t, fixture.postProviderContent)
}

func TestRunApplyInteractiveRenewsConsentAfterProviderOrderRiskExpansion(t *testing.T) {
	fixture := writeApplyProviderConfirmationFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runWithoutTerminal(
		[]string{"lock", "--manifest", fixture.manifestPath},
		&stdout,
		&stderr,
	); exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"lock exitCode=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	stdout.Reset()
	stderr.Reset()

	runnerCalls := 0
	options := interactiveRunOptions(
		strings.NewReader("yes\nno\n"),
		&stdout,
		&stderr,
	)
	options.ApplyExecuteOptions = applyworkflow.ExecuteOptions{
		HostRouteExecutor: fixture.providerExecutor(t, &runnerCalls),
	}
	exitCode := RunWithOptions(
		[]string{
			"apply", "--manifest", fixture.manifestPath, "--manage-existing",
		},
		options,
	)
	if exitCode != 1 || runnerCalls != 1 {
		t.Fatalf(
			"exitCode=%d runnerCalls=%d stdout=%q stderr=%q",
			exitCode,
			runnerCalls,
			stdout.String(),
			stderr.String(),
		)
	}
	for _, want := range []string{
		"extension order changed after carrier updates: 2 new precedence risks",
		`managed="host_relation/pi.package-carrier/beta" foreign="redacted:sha256:`,
		`managed="host_relation/pi.package-carrier/pi-mcp-adapter-project" foreign="redacted:sha256:`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "foreign-extension") ||
		strings.Contains(stdout.String(), filepath.Join(fixture.root, "foreign-extension")) {
		t.Fatalf("stdout discloses local foreign identity:\n%s", stdout.String())
	}
	for _, want := range []string{
		"Proceed with apply? [y/N]:",
		"Proceed with updated apply plan? [y/N]:",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr lacks %q:\n%s", want, stderr.String())
		}
	}
	fixture.assertSettings(t, fixture.postProviderContent)
}

type applyProviderConfirmationFixture struct {
	root                string
	manifestPath        string
	settingsPath        string
	postProviderContent string
}

func writeApplyProviderConfirmationFixture(t *testing.T) applyProviderConfirmationFixture {
	t.Helper()

	root := t.TempDir()
	const providerSource = "npm:pi-mcp-adapter@^2.13.0"
	const betaSource = "npm:@acme/beta@1.0.0"
	const foreignSource = "../foreign-extension"
	writeApplyConfirmationFile(t, root, "daem.toml", `version = 1
targets = ["pi"]

[[extension]]
id = "pi-mcp-adapter-project"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

[[extension]]
id = "beta"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:@acme/beta@1.0.0" }

[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
	writeApplyConfirmationFile(
		t,
		root,
		".pi/settings.json",
		`{"packages":["`+betaSource+`","`+foreignSource+`"]}`,
	)
	return applyProviderConfirmationFixture{
		root:         root,
		manifestPath: filepath.Join(root, "daem.toml"),
		settingsPath: filepath.Join(root, ".pi", "settings.json"),
		postProviderContent: `{"packages":["` + betaSource + `","` +
			foreignSource + `","` + providerSource + `"]}`,
	}
}

func (fixture applyProviderConfirmationFixture) providerExecutor(
	t *testing.T,
	calls *int,
) subprocess.CommandExecutor {
	t.Helper()

	return subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			*calls = *calls + 1
			writeApplyConfirmationFile(
				t,
				fixture.root,
				".pi/settings.json",
				fixture.postProviderContent,
			)
			writeApplyConfirmationFile(
				t,
				fixture.root,
				".pi/npm/node_modules/pi-mcp-adapter/package.json",
				`{"name":"pi-mcp-adapter","version":"2.15.0"}`,
			)
			return subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 0,
			}
		},
	})
}

func (fixture applyProviderConfirmationFixture) assertSettings(t *testing.T, want string) {
	t.Helper()

	content, err := os.ReadFile(fixture.settingsPath)
	if err != nil {
		t.Fatalf("read Pi settings: %v", err)
	}
	if string(content) != want {
		t.Fatalf("Pi settings = %s, want %s", content, want)
	}
}
