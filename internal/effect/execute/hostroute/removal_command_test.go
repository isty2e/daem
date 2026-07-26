package hostroute

import (
	"errors"
	"testing"

	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildRemovalCommandPassesExactCanonicalRequestToAdapter(t *testing.T) {
	fixture := newRemovalFixture(t)
	var captured RemovalRequest
	command, err := BuildRemovalCommand(RemovalBuildInput{
		Action:  fixture.removeAction(t),
		WorkDir: "/selected/project",
		Adapter: func(request RemovalRequest) (subprocess.CommandAttemptRequest, error) {
			captured = request
			return subprocess.CommandAttemptRequest{
				Command: "fake-host",
				Args:    []string{"remove", request.RouteRequest().RouteID()},
				WorkDir: request.WorkDir(),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("BuildRemovalCommand returned error: %v", err)
	}
	if captured.Subject() != fixture.subject ||
		captured.Target() != target.TargetClaudeCode ||
		captured.Scope() != target.ScopeProject {
		t.Fatalf(
			"adapter identity = %s/%q/%q",
			captured.Subject().String(),
			captured.Target(),
			captured.Scope(),
		)
	}
	if captured.Operation().Operation() != lock.OperationRemove ||
		!captured.RouteRequest().Equal(fixture.removeRequest) {
		t.Fatalf(
			"adapter operation/request = %q/%#v",
			captured.Operation().Operation(),
			captured.RouteRequest(),
		)
	}
	if command.Subject() != fixture.subject ||
		!command.RouteRequest().Equal(fixture.removeRequest) {
		t.Fatal("command did not preserve exact removal identity")
	}
	attempt := command.AttemptRequest()
	attempt.Args[0] = "forged"
	if command.AttemptRequest().Args[0] != "remove" {
		t.Fatal("AttemptRequest exposed mutable command state")
	}
}

func TestBuildRemovalCommandRejectsNonRemovalActionsAndMissingAuthority(t *testing.T) {
	fixture := newRemovalFixture(t)
	tests := []struct {
		name  string
		input RemovalBuildInput
		code  ReasonCode
	}{
		{
			name: "zero action",
			input: RemovalBuildInput{
				WorkDir: "/selected/project",
				Adapter: validRemovalAdapter,
			},
			code: ReasonUnsupportedAction,
		},
		{
			name: "retain action",
			input: RemovalBuildInput{
				Action:  fixture.retainAction(t),
				WorkDir: "/selected/project",
				Adapter: validRemovalAdapter,
			},
			code: ReasonUnsupportedAction,
		},
		{
			name: "missing workdir",
			input: RemovalBuildInput{
				Action:  fixture.removeAction(t),
				WorkDir: " \t",
				Adapter: validRemovalAdapter,
			},
			code: ReasonMissingWorkDir,
		},
		{
			name: "missing adapter",
			input: RemovalBuildInput{
				Action:  fixture.removeAction(t),
				WorkDir: "/selected/project",
			},
			code: ReasonUnsupportedRoute,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildRemovalCommand(test.input)
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Code() != test.code {
				t.Fatalf("error = %v, want ValidationError code %q", err, test.code)
			}
		})
	}
}

func TestBuildRemovalCommandRejectsInvalidAdapterOutput(t *testing.T) {
	fixture := newRemovalFixture(t)
	tests := []struct {
		name    string
		adapter RemovalAdapter
		code    ReasonCode
	}{
		{
			name: "empty command",
			adapter: func(RemovalRequest) (subprocess.CommandAttemptRequest, error) {
				return subprocess.CommandAttemptRequest{
					Command: " ",
					WorkDir: "/selected/project",
				}, nil
			},
			code: ReasonUnsupportedRoute,
		},
		{
			name: "padded command",
			adapter: func(RemovalRequest) (subprocess.CommandAttemptRequest, error) {
				return subprocess.CommandAttemptRequest{
					Command: " fake-host ",
					WorkDir: "/selected/project",
				}, nil
			},
			code: ReasonUnsupportedRoute,
		},
		{
			name: "workdir substitution",
			adapter: func(RemovalRequest) (subprocess.CommandAttemptRequest, error) {
				return subprocess.CommandAttemptRequest{
					Command: "fake-host",
					WorkDir: "/other/project",
				}, nil
			},
			code: ReasonMissingWorkDir,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildRemovalCommand(RemovalBuildInput{
				Action:  fixture.removeAction(t),
				WorkDir: "/selected/project",
				Adapter: test.adapter,
			})
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Code() != test.code {
				t.Fatalf("error = %v, want ValidationError code %q", err, test.code)
			}
		})
	}
}

func TestBuildRemovalCommandPreservesAdapterError(t *testing.T) {
	fixture := newRemovalFixture(t)
	want := errors.New("adapter rejected host version")
	_, err := BuildRemovalCommand(RemovalBuildInput{
		Action:  fixture.removeAction(t),
		WorkDir: "/selected/project",
		Adapter: func(RemovalRequest) (subprocess.CommandAttemptRequest, error) {
			return subprocess.CommandAttemptRequest{}, want
		},
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want adapter error", err)
	}
}
