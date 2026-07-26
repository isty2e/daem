package lock

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/delegate"
)

func TestDelegatePlanIdentityRejectsTamperedKeyAndMachineLocalCommand(t *testing.T) {
	identity := DelegatePlanIdentityFromPlan(mustDelegatePlan(t, delegate.RunnerPlain, "node", []string{"server.js"}, nil, delegate.PinNotApplicable))
	identity.IdentityKey = "delegate:v2:{}"
	if _, err := NewDelegatePlanIdentity(identity); err == nil || !strings.Contains(err.Error(), "identity key does not match") {
		t.Fatalf("NewDelegatePlanIdentity error = %v, want identity mismatch", err)
	}

	identity = DelegatePlanIdentity{
		IdentityKey: "delegate:v2:{}",
		RunnerKind:  delegate.RunnerPlain,
		Command:     "/usr/bin/node",
		Args:        []string{"server.js"},
		PinPolicy:   delegate.PinNotApplicable,
	}
	if _, err := NewDelegatePlanIdentity(identity); err == nil || !strings.Contains(err.Error(), "portable token") {
		t.Fatalf("NewDelegatePlanIdentity error = %v, want machine-local command rejection", err)
	}
}

func TestDelegatePlanIdentityClonesMutableFields(t *testing.T) {
	packageRef := mustPackageRef(t, delegate.EcosystemNPM, "@scope/server", "1.2.3")
	input := DelegatePlanIdentityFromPlan(mustDelegatePlan(
		t,
		delegate.RunnerNPX,
		"npx",
		[]string{"-y", "@scope/server@1.2.3"},
		&packageRef,
		delegate.PinPinned,
	))
	contract := mustLockedContractWithDelegateIdentity(t, input)

	input.Args[0] = "--mutated"
	input.Env = append(input.Env, DelegateEnvBinding{Name: "MUTATED", SourceName: "MUTATED"})
	input.Package.Name = "mutated"

	first, ok := contract.DelegatePlanIdentity()
	if !ok {
		t.Fatal("record is missing delegate identity")
	}
	first.Args[0] = "--also-mutated"
	first.Env = append(first.Env, DelegateEnvBinding{Name: "ALSO_MUTATED", SourceName: "ALSO_MUTATED"})
	first.Package.Name = "also-mutated"

	second, ok := contract.DelegatePlanIdentity()
	if !ok {
		t.Fatal("record is missing delegate identity on second read")
	}
	if second.Args[0] != "-y" || len(second.Env) != 0 || second.Package.Name != "@scope/server" {
		t.Fatalf("delegate identity was mutated through caller-owned data: %#v", second)
	}
}

func TestDelegatePlanIdentityCorrelatesExactInvocation(t *testing.T) {
	runner, err := delegate.NewRunner(delegate.RunnerPlain)
	if err != nil {
		t.Fatal(err)
	}
	command, err := delegate.NewCommandSpec("node", []string{"server.js", "--stdio"})
	if err != nil {
		t.Fatal(err)
	}
	env := testDelegateEnvBindings(t, map[string]string{
		"TOKEN_B": "TOKEN_B",
		"TOKEN_A": "TOKEN_A",
	})
	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner:    runner,
		Command:   command,
		Env:       env,
		PinPolicy: delegate.PinNotApplicable,
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := DelegatePlanIdentityFromPlan(plan)

	if !identity.CorrelatesInvocation(
		"node",
		[]string{"server.js", "--stdio"},
		[]DelegateEnvBinding{
			{Name: "TOKEN_A", SourceName: "TOKEN_A"},
			{Name: "TOKEN_B", SourceName: "TOKEN_B"},
		},
	) {
		t.Fatal("exact invocation did not correlate")
	}
	for _, test := range []struct {
		name    string
		command string
		args    []string
		env     []DelegateEnvBinding
	}{
		{name: "command", command: "evil", args: []string{"server.js", "--stdio"}, env: []DelegateEnvBinding{{Name: "TOKEN_A", SourceName: "TOKEN_A"}, {Name: "TOKEN_B", SourceName: "TOKEN_B"}}},
		{name: "argument order", command: "node", args: []string{"--stdio", "server.js"}, env: []DelegateEnvBinding{{Name: "TOKEN_A", SourceName: "TOKEN_A"}, {Name: "TOKEN_B", SourceName: "TOKEN_B"}}},
		{name: "credential", command: "node", args: []string{"server.js", "--stdio"}, env: []DelegateEnvBinding{{Name: "TOKEN_A", SourceName: "TOKEN_A"}}},
		{name: "child name", command: "node", args: []string{"server.js", "--stdio"}, env: []DelegateEnvBinding{{Name: "RENAMED", SourceName: "TOKEN_A"}, {Name: "TOKEN_B", SourceName: "TOKEN_B"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if identity.CorrelatesInvocation(test.command, test.args, test.env) {
				t.Fatal("mismatched invocation correlated")
			}
		})
	}

	tampered := identity
	tampered.IdentityKey = "delegate:v2:{}"
	if tampered.CorrelatesInvocation(identity.Command, identity.Args, identity.Env) {
		t.Fatal("invalid identity correlated")
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
		Runner:     runner,
		Command:    command,
		Env:        env,
		PackageRef: packageRef,
		PinPolicy:  pinPolicy,
	})
	if err != nil {
		t.Fatalf("NewDelegatePlan returned error: %v", err)
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

func mustPackageRef(t *testing.T, ecosystem delegate.PackageEcosystem, name string, selector string) delegate.PackageRef {
	t.Helper()
	packageRef, err := delegate.NewPackageRef(ecosystem, name, selector)
	if err != nil {
		t.Fatalf("NewPackageRef returned error: %v", err)
	}
	return packageRef
}

func mustLockedContractWithDelegateIdentity(t *testing.T, identity DelegatePlanIdentity) LockedSubjectContract {
	t.Helper()
	placement := mustTestMCPPlacement(t, aggregate.MCPPlacementClaudeProject)
	input := testMCPProjectionInput(t, placement, nil)
	input.LauncherCommand = identity.Command
	input.LauncherArgs = append([]string(nil), identity.Args...)
	input.Graph = testMCPProjectionGraph(t, placement, identity.Command, nil)
	input.DelegatePlanIdentity = &identity
	contract, err := NewMCPProjectionSubjectContract(input)
	if err != nil {
		t.Fatalf("NewMCPProjectionSubjectContract returned error: %v", err)
	}
	return contract
}
