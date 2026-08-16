package hostroute

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestBuildDelegatedRemovalAttemptBuildsExactPiCommands(t *testing.T) {
	tests := []struct {
		name     string
		scope    target.Scope
		source   string
		wantArgs []string
	}{
		{
			name:     "project npm",
			scope:    target.ScopeProject,
			source:   "npm:@acme/pi-tools@1.2.3",
			wantArgs: []string{"remove", "npm:@acme/pi-tools@1.2.3", "-l"},
		},
		{
			name:     "global npm",
			scope:    target.ScopeGlobal,
			source:   "npm:@acme/pi-tools@1.2.3",
			wantArgs: []string{"remove", "npm:@acme/pi-tools@1.2.3"},
		},
		{
			name:     "project git",
			scope:    target.ScopeProject,
			source:   "git:github.com/acme/pi-tools.git@v2",
			wantArgs: []string{"remove", "git:github.com/acme/pi-tools.git@v2", "-l"},
		},
		{
			name:     "global git",
			scope:    target.ScopeGlobal,
			source:   "git+https://github.com/acme/pi-tools.git#v2",
			wantArgs: []string{"remove", "git+https://github.com/acme/pi-tools.git#v2"},
		},
		{
			name:     "literal percent global git",
			scope:    target.ScopeGlobal,
			source:   "git+https://example.com/acme/100%25-tool.git#v1",
			wantArgs: []string{"remove", "git+https://example.com/acme/100%25-tool.git#v1"},
		},
		{
			name:     "project local",
			scope:    target.ScopeProject,
			source:   "./tools/pi-local",
			wantArgs: []string{"remove", "./tools/pi-local", "-l"},
		},
		{
			name:     "global local",
			scope:    target.ScopeGlobal,
			source:   "/opt/pi-local",
			wantArgs: []string{"remove", "/opt/pi-local"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action := newPiRemovalAction(t, test.scope, test.source)
			workDir := filepath.Join(t.TempDir(), "project with spaces")
			command, err := BuildRemovalCommand(RemovalBuildInput{
				Action:  action,
				WorkDir: workDir,
				Adapter: BuildDelegatedRemovalAttempt,
			})
			if err != nil {
				t.Fatalf("BuildRemovalCommand returned error: %v", err)
			}
			attempt := command.AttemptRequest()
			if attempt.Command != piCommand {
				t.Fatalf("command = %q, want %q", attempt.Command, piCommand)
			}
			if !slices.Equal(attempt.Args, test.wantArgs) {
				t.Fatalf("args = %#v, want %#v", attempt.Args, test.wantArgs)
			}
			if attempt.WorkDir != workDir {
				t.Fatalf("workdir = %q, want %q", attempt.WorkDir, workDir)
			}
			if slices.Contains(attempt.Args, "--approve") ||
				slices.Contains(attempt.Args, "--no-approve") {
				t.Fatalf("Pi remove args contain an approval flag: %#v", attempt.Args)
			}
		})
	}
}

func TestBuildDelegatedRemovalAttemptBuildsExactClaudeCommands(t *testing.T) {
	tests := []struct {
		name      string
		scope     target.Scope
		hostScope string
	}{
		{name: "project", scope: target.ScopeProject, hostScope: "project"},
		{name: "global maps to user", scope: target.ScopeGlobal, hostScope: "user"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action := newClaudeRemovalAction(t, test.scope, "context7@official")
			workDir := filepath.Join(t.TempDir(), "project with spaces")
			command, err := BuildRemovalCommand(RemovalBuildInput{
				Action:  action,
				WorkDir: workDir,
				Adapter: BuildDelegatedRemovalAttempt,
			})
			if err != nil {
				t.Fatalf("BuildRemovalCommand returned error: %v", err)
			}
			attempt := command.AttemptRequest()
			wantArgs := []string{
				"plugin",
				"uninstall",
				"context7@official",
				"--scope",
				test.hostScope,
				"--keep-data",
			}
			if attempt.Command != claudeCommand ||
				!slices.Equal(attempt.Args, wantArgs) ||
				attempt.WorkDir != workDir {
				t.Fatalf("Claude removal attempt = %#v, want args %#v", attempt, wantArgs)
			}
			if slices.Contains(attempt.Args, "--prune") {
				t.Fatalf("Claude removal args contain prune authority: %#v", attempt.Args)
			}
		})
	}
}

func TestBuildDelegatedRemovalAttemptBuildsExactCodexCommand(t *testing.T) {
	action := newCodexRemovalAction(t, "documents@openai-primary-runtime")
	workDir := filepath.Join(t.TempDir(), "project with spaces")
	command, err := BuildRemovalCommand(RemovalBuildInput{
		Action:  action,
		WorkDir: workDir,
		Adapter: BuildDelegatedRemovalAttempt,
	})
	if err != nil {
		t.Fatalf("BuildRemovalCommand returned error: %v", err)
	}
	attempt := command.AttemptRequest()
	wantArgs := []string{
		"plugin",
		"remove",
		"documents@openai-primary-runtime",
		"--json",
	}
	if attempt.Command != codexCommand ||
		!slices.Equal(attempt.Args, wantArgs) ||
		attempt.WorkDir != workDir {
		t.Fatalf("Codex removal attempt = %#v, want args %#v", attempt, wantArgs)
	}
	if slices.Contains(attempt.Args, "marketplace") {
		t.Fatalf("Codex removal widened to marketplace mutation: %#v", attempt.Args)
	}
}

func TestBuildDelegatedRemovalAttemptBuildsExactAntigravityCommand(t *testing.T) {
	action := newAntigravityRemovalAction(t, "modern-web-guidance@google")
	workDir := filepath.Join(t.TempDir(), "project with spaces")
	command, err := BuildRemovalCommand(RemovalBuildInput{
		Action:  action,
		WorkDir: workDir,
		Adapter: BuildDelegatedRemovalAttempt,
	})
	if err != nil {
		t.Fatalf("BuildRemovalCommand returned error: %v", err)
	}
	attempt := command.AttemptRequest()
	wantArgs := []string{"plugin", "uninstall", "modern-web-guidance"}
	if attempt.Command != agyCommand ||
		!slices.Equal(attempt.Args, wantArgs) ||
		attempt.WorkDir != workDir {
		t.Fatalf("Antigravity removal attempt = %#v, want args %#v", attempt, wantArgs)
	}
	if slices.Contains(attempt.Args, "--help") ||
		slices.Contains(attempt.Args, "modern-web-guidance@google") {
		t.Fatalf(
			"Antigravity removal passed help-like or source selector argv: %#v",
			attempt.Args,
		)
	}
}

func TestBuildAntigravityRemovalRejectsOpaqueSourceAndLeadingOptionIdentity(t *testing.T) {
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"antigravity-cli.plugin-carrier",
		"guidance",
	)
	if err != nil {
		t.Fatal(err)
	}
	localSource, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"./plugins/guidance",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = buildAntigravityCLIPluginCarrierRemoveCommand(commandAdapterInput{
		subject: subject,
		scope:   target.ScopeGlobal,
		source:  localSource,
		workDir: t.TempDir(),
	})
	var validation *ValidationError
	if !errors.As(err, &validation) ||
		validation.Code() != ReasonUnsupportedSource {
		t.Fatalf("opaque source error = %v", err)
	}

	if _, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"--help@google",
	); err == nil {
		t.Fatal("leading-option plugin identity was admitted")
	}
}

func TestBuildDelegatedRemovalAttemptRejectsUnadmittedRoute(t *testing.T) {
	fixture := newRemovalFixture(t)
	_, err := BuildRemovalCommand(RemovalBuildInput{
		Action:  fixture.removeAction(t),
		WorkDir: "/selected/project",
		Adapter: BuildDelegatedRemovalAttempt,
	})
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Code() != ReasonUnsupportedRoute {
		t.Fatalf("error = %v, want unsupported-route ValidationError", err)
	}
}

func TestBuildDelegatedRemovalAttemptRejectsRouteVersionMismatch(t *testing.T) {
	action := newPiRemovalAction(t, target.ScopeProject, "git:github.com/acme/pi-tools")
	var request RemovalRequest
	_, err := BuildRemovalCommand(RemovalBuildInput{
		Action:  action,
		WorkDir: "/selected/project",
		Adapter: func(captured RemovalRequest) (subprocess.CommandAttemptRequest, error) {
			request = captured
			return validRemovalAdapter(captured)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	forgedOperation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation:       lock.OperationRemove,
		Actuation:       lock.ActuationDelegatedHostRoute,
		Authority:       lock.AuthorityRemove,
		Route:           lock.RouteContractRef{RouteID: "pi.package-carrier.remove", AdapterContractVersion: "forged-v9"},
		EffectEnvelope:  lock.EffectEnvelopeComplete,
		Idempotency:     lock.ConditionallyIdempotent,
		Verification:    lock.VerificationHostRelation,
		TrustActivation: lock.TrustActivationNotRequired,
		Recovery:        lock.OperationRecoverySafeRetry,
	})
	if err != nil {
		t.Fatal(err)
	}
	request.operation = forgedOperation
	_, err = BuildDelegatedRemovalAttempt(request)
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Code() != ReasonRouteRequestMismatch {
		t.Fatalf("error = %v, want route-request-mismatch ValidationError", err)
	}
}
