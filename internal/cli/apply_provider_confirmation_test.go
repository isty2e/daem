package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	options.ReadConfirmationLine = confirmationAnswerSequence("yes", "no")
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

func TestRunApplyInteractiveAcceptsRenewedProviderOrderRisk(t *testing.T) {
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
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	options.ReadConfirmationLine = confirmationAnswerSequence("yes", "yes")
	options.ApplyExecuteOptions = applyworkflow.ExecuteOptions{
		HostRouteExecutor: fixture.providerExecutor(t, &runnerCalls),
	}
	exitCode := RunWithOptions(
		[]string{
			"apply", "--manifest", fixture.manifestPath, "--manage-existing",
		},
		options,
	)
	if exitCode != 0 || runnerCalls != 1 {
		t.Fatalf(
			"exitCode=%d runnerCalls=%d stdout=%q stderr=%q",
			exitCode,
			runnerCalls,
			stdout.String(),
			stderr.String(),
		)
	}
	if !strings.Contains(stdout.String(), "applied:") ||
		!strings.Contains(stderr.String(), "Proceed with updated apply plan? [y/N]:") {
		t.Fatalf(
			"renewed success output incomplete: stdout=%q stderr=%q",
			stdout.String(),
			stderr.String(),
		)
	}
	fixture.assertSettings(t, fixture.convergedContent)
}

func TestRunApplyYesJSONReportsProviderOrderRiskExpansion(t *testing.T) {
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
	exitCode := RunWithOptions(
		[]string{
			"apply",
			"--manifest",
			fixture.manifestPath,
			"--manage-existing",
			"--yes",
			"--json",
		},
		RunOptions{
			Context: t.Context(),
			Stdout:  &stdout,
			Stderr:  &stderr,
			ApplyExecuteOptions: applyworkflow.ExecuteOptions{
				HostRouteExecutor: fixture.providerExecutor(t, &runnerCalls),
			},
		},
	)
	if exitCode != 1 || runnerCalls != 1 || stderr.Len() != 0 {
		t.Fatalf(
			"exitCode=%d runnerCalls=%d stdout=%q stderr=%q",
			exitCode,
			runnerCalls,
			stdout.String(),
			stderr.String(),
		)
	}
	var payload struct {
		HasErrors bool `json:"has_errors"`
		Errors    []struct {
			Message string `json:"message"`
		} `json:"errors"`
		HostRouteAttempts []json.RawMessage `json:"host_route_attempts"`
		RelationOrders    []struct {
			Risks []struct {
				ForeignIdentity string `json:"foreign_identity"`
			} `json:"risks"`
		} `json:"relation_order_actions"`
		DelegateAttempts []json.RawMessage `json:"delegate_attempts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode apply JSON: %v\n%s", err, stdout.String())
	}
	if !payload.HasErrors ||
		len(payload.Errors) != 1 ||
		!strings.Contains(
			payload.Errors[0].Message,
			"extension order risk expanded after carrier changes",
		) ||
		len(payload.HostRouteAttempts) != 1 ||
		len(payload.RelationOrders) != 1 ||
		len(payload.RelationOrders[0].Risks) != 2 ||
		len(payload.DelegateAttempts) != 0 {
		t.Fatalf("apply JSON payload = %#v", payload)
	}
	for _, risk := range payload.RelationOrders[0].Risks {
		if !strings.HasPrefix(risk.ForeignIdentity, "redacted:sha256:") {
			t.Fatalf("unredacted provider order risk = %#v", risk)
		}
	}
	if strings.Contains(stdout.String(), "foreign-extension") {
		t.Fatalf("apply JSON disclosed local identity: %s", stdout.String())
	}
	fixture.assertSettings(t, fixture.postProviderContent)
}

func TestRunApplyRejectsDeclarationChangeDuringRenewedConsent(t *testing.T) {
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

	confirmationReads := 0
	options := interactiveRunOptions(
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	answers := confirmationAnswerSequence("yes", "yes")
	options.ReadConfirmationLine = func(
		ctx context.Context,
		input io.Reader,
		maximumBytes int,
	) (string, error) {
		line, err := answers(ctx, input, maximumBytes)
		confirmationReads++
		if confirmationReads == 2 {
			content, readErr := os.ReadFile(fixture.manifestPath)
			if readErr != nil {
				t.Fatalf("read manifest during confirmation: %v", readErr)
			}
			writeApplyConfirmationFile(
				t,
				fixture.root,
				"daem.toml",
				string(content)+"\n# changed during renewed consent\n",
			)
		}
		return line, err
	}
	runnerCalls := 0
	options.ApplyExecuteOptions = applyworkflow.ExecuteOptions{
		HostRouteExecutor: fixture.providerExecutor(t, &runnerCalls),
	}
	exitCode := RunWithOptions(
		[]string{
			"apply", "--manifest", fixture.manifestPath, "--manage-existing",
		},
		options,
	)
	if exitCode != 1 || runnerCalls != 1 || confirmationReads != 2 {
		t.Fatalf(
			"exitCode=%d runnerCalls=%d confirmations=%d stdout=%q stderr=%q",
			exitCode,
			runnerCalls,
			confirmationReads,
			stdout.String(),
			stderr.String(),
		)
	}
	if !strings.Contains(stderr.String(), "stale_plan") ||
		strings.Contains(stdout.String(), "applied:") {
		t.Fatalf(
			"stale renewed consent output mismatch: stdout=%q stderr=%q",
			stdout.String(),
			stderr.String(),
		)
	}
	fixture.assertSettings(t, fixture.postProviderContent)
}

func confirmationAnswerSequence(
	answers ...string,
) func(context.Context, io.Reader, int) (string, error) {
	next := 0
	return func(ctx context.Context, _ io.Reader, maximumBytes int) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if next >= len(answers) {
			return "", io.EOF
		}
		answer := answers[next]
		next++
		if len(answer) > maximumBytes {
			return "", io.ErrShortBuffer
		}
		return answer, nil
	}
}

type applyProviderConfirmationFixture struct {
	root                string
	manifestPath        string
	settingsPath        string
	postProviderContent string
	convergedContent    string
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
		convergedContent: `{"packages":["` + providerSource + `","` +
			foreignSource + `","` + betaSource + `"]}`,
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
