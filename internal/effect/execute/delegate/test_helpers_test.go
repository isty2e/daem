package delegate

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type testActionInput struct {
	disposition     reconcile.DelegateDisposition
	mode            delegatepolicy.Mode
	command         string
	args            []string
	envRefs         []string
	env             map[string]string
	runnerReadiness delegatepolicy.RunnerReadiness
}

func testAction(t *testing.T, input testActionInput) reconcile.DelegateAction {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "claude-code.project.mcp-server", "context7")
	if err != nil {
		t.Fatalf("ProjectionSubjectID returned error: %v", err)
	}
	if input.command == "" {
		input.command = "delegate-test"
	}
	if input.runnerReadiness == "" {
		input.runnerReadiness = delegatepolicy.RunnerAvailable
	}
	plan := testDelegatePlan(t, input.command, input.args, input.envRefs, input.env)
	decision, err := delegatepolicy.Evaluate(delegatepolicy.Input{
		Plan:   plan,
		Mode:   input.mode,
		Runner: input.runnerReadiness,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	action, err := reconcile.NewDelegateAction(reconcile.DelegateActionInput{
		Subject:     subject,
		Target:      target.TargetClaudeCode,
		Scope:       target.ScopeProject,
		Plan:        plan,
		Disposition: input.disposition,
		Risks:       decision.Risks(),
	})
	if err != nil {
		t.Fatalf("NewDelegateAction returned error: %v", err)
	}
	return action
}

func testDelegatePlan(
	t *testing.T,
	commandName string,
	args []string,
	envRefs []string,
	env map[string]string,
) delegate.DelegatePlan {
	t.Helper()
	runner, err := delegate.NewRunner(delegate.RunnerPlain)
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	command, err := delegate.NewCommandSpec(commandName, args)
	if err != nil {
		t.Fatalf("NewCommandSpec returned error: %v", err)
	}
	bindings := make([]delegate.EnvBinding, 0, len(envRefs)+len(env))
	for _, name := range envRefs {
		binding, err := delegate.NewEnvBinding(name, name)
		if err != nil {
			t.Fatalf("NewEnvBinding returned error: %v", err)
		}
		bindings = append(bindings, binding)
	}
	for name, sourceName := range env {
		binding, err := delegate.NewEnvBinding(name, sourceName)
		if err != nil {
			t.Fatalf("NewEnvBinding returned error: %v", err)
		}
		bindings = append(bindings, binding)
	}
	envSet, err := delegate.NewEnvBindingSet(bindings)
	if err != nil {
		t.Fatalf("NewEnvBindingSet returned error: %v", err)
	}
	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner:    runner,
		Command:   command,
		Env:       envSet,
		PinPolicy: delegate.PinNotApplicable,
	})
	if err != nil {
		t.Fatalf("NewDelegatePlan returned error: %v", err)
	}
	return plan
}

func testWorkingDirectoryBinder(t *testing.T) subprocess.WorkingDirectoryBinder {
	t.Helper()
	root, err := rootedpath.CaptureRoot(t.TempDir())
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close captured test root: %v", err)
		}
	})
	return func() (subprocess.WorkingDirectoryBinding, error) {
		return root.AcquireWorkingDirectory()
	}
}

func testWorkingDirectoryBinderForAction(t *testing.T) BinderForAction {
	t.Helper()
	bind := testWorkingDirectoryBinder(t)
	return func(reconcile.DelegateAction) subprocess.WorkingDirectoryBinder {
		return bind
	}
}

func envContains(env []string, expected string) bool {
	return slices.Contains(env, expected)
}
