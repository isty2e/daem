package lock

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/delegate"
)

func TestLockedSubjectContractRetainsCanonicalDelegatePlanValue(t *testing.T) {
	args := []string{"-y", "@scope/server@1.2.3"}
	packageRef := mustPackageRef(t, delegate.EcosystemNPM, "@scope/server", "1.2.3")
	plan := mustDelegatePlan(
		t,
		delegate.RunnerNPX,
		"npx",
		args,
		&packageRef,
		delegate.PinPinned,
	)
	contract := mustLockedContractWithDelegatePlan(t, plan)

	args[0] = "--mutated"
	first, ok := contract.DelegatePlan()
	if !ok {
		t.Fatal("contract is missing delegate plan")
	}
	firstArgs := first.Command().Args()
	firstArgs[0] = "--also-mutated"

	second, ok := contract.DelegatePlan()
	if !ok {
		t.Fatal("contract is missing delegate plan on second read")
	}
	if !second.Equal(plan) || second.Command().Args()[0] != "-y" {
		t.Fatalf("delegate plan was mutated through caller-owned data: %#v", second.Command().Args())
	}
}

func mustDelegatePlan(
	t *testing.T,
	runnerKind delegate.RunnerKind,
	commandName string,
	args []string,
	packageRef *delegate.PackageRef,
	pinPolicy delegate.PinPolicy,
) delegate.DelegatePlan {
	t.Helper()
	runner, err := delegate.NewRunner(runnerKind)
	if err != nil {
		t.Fatalf("NewRunner returned error: %v", err)
	}
	command, err := delegate.NewCommandSpec(commandName, args)
	if err != nil {
		t.Fatalf("NewCommandSpec returned error: %v", err)
	}
	env := testDelegateEnvBindings(t, nil)
	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner:  runner,
		Command: command,
		Env:     env,
	})
	if err != nil {
		t.Fatalf("NewDelegatePlan returned error: %v", err)
	}
	if plan.PinPolicy() != pinPolicy {
		t.Fatalf("derived pin policy = %q, want %q", plan.PinPolicy(), pinPolicy)
	}
	if packageRef != nil {
		refs := plan.PackageRefs()
		if len(refs) != 1 || refs[0] != *packageRef {
			t.Fatalf("derived package refs = %#v, want %#v", refs, *packageRef)
		}
	}
	return plan
}

func testDelegateEnvBindings(t *testing.T, values map[string]string) delegate.EnvBindingSet {
	t.Helper()
	bindings := make([]delegate.EnvBinding, 0, len(values))
	for name, sourceName := range values {
		binding, err := delegate.NewEnvBinding(name, sourceName)
		if err != nil {
			t.Fatalf("NewEnvBinding returned error: %v", err)
		}
		bindings = append(bindings, binding)
	}
	set, err := delegate.NewEnvBindingSet(bindings)
	if err != nil {
		t.Fatalf("NewEnvBindingSet returned error: %v", err)
	}
	return set
}

func mustPackageRef(
	t *testing.T,
	ecosystem delegate.PackageEcosystem,
	name string,
	selector string,
) delegate.PackageRef {
	t.Helper()
	packageRef, err := delegate.NewPackageRef(ecosystem, name, selector)
	if err != nil {
		t.Fatalf("NewPackageRef returned error: %v", err)
	}
	return packageRef
}

func mustLockedContractWithDelegatePlan(
	t *testing.T,
	plan delegate.DelegatePlan,
) LockedSubjectContract {
	t.Helper()
	placement := mustTestMCPPlacement(t, aggregate.MCPPlacementClaudeProject)
	credentialReferences := plan.Env().SourceNames()
	input := testMCPProjectionInput(t, placement, credentialReferences)
	command := plan.Command()
	input.LauncherCommand = command.Executable()
	input.LauncherArgs = command.Args()
	input.Graph = testMCPProjectionGraph(t, placement, command.Executable(), credentialReferences)
	input.DelegatePlan = &plan
	contract, err := NewMCPProjectionSubjectContract(input)
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return contract
}
