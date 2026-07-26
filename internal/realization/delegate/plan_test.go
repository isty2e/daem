package delegate

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestDelegatePlanConstructsSupportedForms(t *testing.T) {
	tests := []struct {
		name     string
		runner   RunnerKind
		command  string
		args     []string
		env      []string
		pkg      *packageInput
		pin      PinPolicy
		needsEnv string
	}{
		{
			name:     "plain ambient binary",
			runner:   RunnerPlain,
			command:  "node",
			args:     []string{"server.js"},
			env:      []string{"API_TOKEN"},
			pin:      PinNotApplicable,
			needsEnv: "API_TOKEN",
		},
		{
			name:    "npx package",
			runner:  RunnerNPX,
			command: "npx",
			args:    []string{"-y", "@modelcontextprotocol/server-filesystem@1.2.3"},
			pkg:     &packageInput{ecosystem: EcosystemNPM, name: "@modelcontextprotocol/server-filesystem", selector: "1.2.3"},
			pin:     PinPinned,
		},
		{
			name:    "uvx package",
			runner:  RunnerUVX,
			command: "uvx",
			args:    []string{"mcp-server"},
			pkg:     &packageInput{ecosystem: EcosystemPython, name: "mcp-server", selector: "0.4.0"},
			pin:     PinPinned,
		},
		{
			name:    "docker image",
			runner:  RunnerDocker,
			command: "docker",
			args:    []string{"run", "ghcr.io/acme/mcp-server"},
			pkg:     &packageInput{ecosystem: EcosystemContainer, name: "ghcr.io/acme/mcp-server", selector: "sha256:abc123"},
			pin:     PinPinned,
		},
		{
			name:    "host native delegate",
			runner:  RunnerHostNative,
			command: "claude",
			args:    []string{"mcp", "add", "filesystem"},
			pin:     PinHostSelected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := mustPlan(t, test.runner, test.command, test.args, test.env, test.pkg, test.pin)
			if plan.Runner().Kind() != test.runner {
				t.Fatalf("Runner().Kind() = %q, want %q", plan.Runner().Kind(), test.runner)
			}
			if plan.Command().Name() != test.command {
				t.Fatalf("Command().Name() = %q, want %q", plan.Command().Name(), test.command)
			}
			if plan.PinPolicy() != test.pin {
				t.Fatalf("PinPolicy() = %q, want %q", plan.PinPolicy(), test.pin)
			}
			if test.needsEnv != "" && !slices.Contains(plan.Env().SourceNames(), test.needsEnv) {
				t.Fatalf("Env().SourceNames() = %#v, want %q", plan.Env().SourceNames(), test.needsEnv)
			}
			if !strings.HasPrefix(plan.IdentityKey(), "delegate:v2:") {
				t.Fatalf("IdentityKey() = %q, want delegate:v2 prefix", plan.IdentityKey())
			}
		})
	}
}

func TestCommandSpecPreservesEmptyArgument(t *testing.T) {
	command, err := NewCommandSpec("node", []string{"--label", ""})
	if err != nil {
		t.Fatalf("NewCommandSpec returned error: %v", err)
	}
	args := command.Args()
	if len(args) != 2 || args[0] != "--label" || args[1] != "" {
		t.Fatalf("Args = %#v, want empty argument preserved", args)
	}
}

func TestDelegatePlanRejectsInvalidStatesWithReasons(t *testing.T) {
	assertReason(t, ReasonInvalidRunnerKind, func() error {
		_, err := NewRunner(RunnerKind("shell"))
		return err
	})
	assertReason(t, ReasonInvalidCommand, func() error {
		_, err := NewCommandSpec("/usr/local/bin/npx", nil)
		return err
	})
	assertReason(t, ReasonInvalidCommand, func() error {
		_, err := NewCommandSpec("npx && rm", nil)
		return err
	})
	assertReason(t, ReasonInvalidArgument, func() error {
		_, err := NewCommandSpec("node", []string{"ok", "bad\narg"})
		return err
	})
	assertReason(t, ReasonInvalidEnvRef, func() error {
		_, err := NewEnvBinding("1TOKEN", "TOKEN")
		return err
	})
	assertReason(t, ReasonInvalidPackageRef, func() error {
		_, err := NewPackageRef(EcosystemNPM, "scope/name", "")
		return err
	})
	assertReason(t, ReasonInvalidPackageRef, func() error {
		_, err := NewPackageRef(EcosystemPython, "../local", "")
		return err
	})
	assertReason(t, ReasonInvalidPackageRef, func() error {
		_, err := NewPackageRef(EcosystemNPM, ".bin", "")
		return err
	})
	assertReason(t, ReasonInvalidPackageRef, func() error {
		_, err := NewPackageRef(EcosystemContainer, "server:latest", "")
		return err
	})
	assertReason(t, ReasonInvalidPinPolicy, func() error {
		runner := mustRunner(t, RunnerPlain)
		command := mustCommand(t, "node", nil)
		_, err := NewDelegatePlan(DelegatePlanSpec{
			Runner:    runner,
			Command:   command,
			PinPolicy: PinPolicy("unknown"),
		})
		return err
	})
	assertReason(t, ReasonInvalidDelegatePlan, func() error {
		pkg := mustPackage(t, EcosystemNPM, "server", "")
		runner := mustRunner(t, RunnerNPX)
		command := mustCommand(t, "node", nil)
		_, err := NewDelegatePlan(DelegatePlanSpec{
			Runner:     runner,
			Command:    command,
			PackageRef: &pkg,
			PinPolicy:  PinFloating,
		})
		return err
	})
	assertReason(t, ReasonInvalidDelegatePlan, func() error {
		runner := mustRunner(t, RunnerNPX)
		command := mustCommand(t, "npx", []string{"server"})
		_, err := NewDelegatePlan(DelegatePlanSpec{
			Runner:    runner,
			Command:   command,
			PinPolicy: PinFloating,
		})
		return err
	})
	assertReason(t, ReasonInvalidDelegatePlan, func() error {
		pkg := mustPackage(t, EcosystemPython, "server", "")
		runner := mustRunner(t, RunnerNPX)
		command := mustCommand(t, "npx", []string{"server"})
		_, err := NewDelegatePlan(DelegatePlanSpec{
			Runner:     runner,
			Command:    command,
			PackageRef: &pkg,
			PinPolicy:  PinFloating,
		})
		return err
	})
	assertReason(t, ReasonInvalidDelegatePlan, func() error {
		pkg := mustPackage(t, EcosystemNPM, "server", "")
		runner := mustRunner(t, RunnerPlain)
		command := mustCommand(t, "node", nil)
		_, err := NewDelegatePlan(DelegatePlanSpec{
			Runner:     runner,
			Command:    command,
			PackageRef: &pkg,
			PinPolicy:  PinFloating,
		})
		return err
	})
	assertReason(t, ReasonInvalidDelegatePlan, func() error {
		runner := mustRunner(t, RunnerHostNative)
		command := mustCommand(t, "claude", []string{"plugin", "install", "foo"})
		_, err := NewDelegatePlan(DelegatePlanSpec{
			Runner:    runner,
			Command:   command,
			PinPolicy: PinNotApplicable,
		})
		return err
	})
}

func TestDelegatePlanIdentityIsStableAndImmutable(t *testing.T) {
	args := []string{"-y", "server@1.0.0"}
	env := []string{"Z_TOKEN", "A_TOKEN", "Z_TOKEN"}
	pkg := &packageInput{ecosystem: EcosystemNPM, name: "server", selector: "1.0.0"}

	plan := mustPlan(t, RunnerNPX, "npx", args, env, pkg, PinPinned)
	reordered := mustPlan(t, RunnerNPX, "npx", []string{"-y", "server@1.0.0"}, []string{"A_TOKEN", "Z_TOKEN"}, pkg, PinPinned)
	if plan.IdentityKey() != reordered.IdentityKey() {
		t.Fatalf("IdentityKey() changed for reordered env refs:\n%s\n%s", plan.IdentityKey(), reordered.IdentityKey())
	}

	args[0] = "--mutated"
	env[0] = "MUTATED"
	if got := plan.Command().Args()[0]; got != "-y" {
		t.Fatalf("command args mutated through input slice: %q", got)
	}
	if !slices.Equal(plan.Env().SourceNames(), []string{"A_TOKEN", "Z_TOKEN"}) {
		t.Fatalf("env bindings mutated through input slice: %#v", plan.Env().SourceNames())
	}

	returnedArgs := plan.Command().Args()
	returnedArgs[0] = "--mutated"
	if got := plan.Command().Args()[0]; got != "-y" {
		t.Fatalf("command args mutated through accessor slice: %q", got)
	}
	returnedEnv := plan.Env().Bindings()
	returnedEnv[0] = EnvBinding{}
	if !slices.Equal(plan.Env().SourceNames(), []string{"A_TOKEN", "Z_TOKEN"}) {
		t.Fatalf("env bindings mutated through accessor slice: %#v", plan.Env().SourceNames())
	}

	changedArg := mustPlan(t, RunnerNPX, "npx", []string{"server@1.0.0"}, []string{"A_TOKEN", "Z_TOKEN"}, pkg, PinPinned)
	if plan.IdentityKey() == changedArg.IdentityKey() {
		t.Fatalf("IdentityKey() did not change after argv identity changed")
	}
}

func TestEnvBindingSetPreservesChildSourceIdentityAndRejectsConflicts(t *testing.T) {
	first, err := NewEnvBinding("API_TOKEN", "CONTEXT7_API_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewEnvBinding("SECOND_TOKEN", "CONTEXT7_API_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewEnvBindingSet([]EnvBinding{second, first, first})
	if err != nil {
		t.Fatalf("NewEnvBindingSet returned error: %v", err)
	}
	bindings := set.Bindings()
	if len(bindings) != 2 ||
		bindings[0].Name() != "API_TOKEN" ||
		bindings[0].SourceName() != "CONTEXT7_API_TOKEN" ||
		bindings[1].Name() != "SECOND_TOKEN" ||
		bindings[1].SourceName() != "CONTEXT7_API_TOKEN" {
		t.Fatalf("bindings = %#v, want two child names sharing one source", bindings)
	}
	if !slices.Equal(set.SourceNames(), []string{"CONTEXT7_API_TOKEN"}) {
		t.Fatalf("source names = %#v, want one deduplicated source", set.SourceNames())
	}

	conflict, err := NewEnvBinding("API_TOKEN", "OTHER_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	assertReason(t, ReasonInvalidEnvRef, func() error {
		_, err := NewEnvBindingSet([]EnvBinding{first, conflict})
		return err
	})
}

func TestDelegatePlanIdentityIncludesChildAndSourceEnvironmentNames(t *testing.T) {
	mapping := func(child string, source string) EnvBindingSet {
		binding, err := NewEnvBinding(child, source)
		if err != nil {
			t.Fatal(err)
		}
		set, err := NewEnvBindingSet([]EnvBinding{binding})
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	plan := func(env EnvBindingSet) DelegatePlan {
		value, err := NewDelegatePlan(DelegatePlanSpec{
			Runner:    mustRunner(t, RunnerPlain),
			Command:   mustCommand(t, "node", []string{"server.js"}),
			Env:       env,
			PinPolicy: PinNotApplicable,
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	base := plan(mapping("API_TOKEN", "CONTEXT7_API_TOKEN"))
	changedChild := plan(mapping("RENAMED_TOKEN", "CONTEXT7_API_TOKEN"))
	changedSource := plan(mapping("API_TOKEN", "OTHER_TOKEN"))
	if base.IdentityKey() == changedChild.IdentityKey() {
		t.Fatal("identity ignored child environment name")
	}
	if base.IdentityKey() == changedSource.IdentityKey() {
		t.Fatal("identity ignored host environment source name")
	}
}

func TestDelegatePlanRefusesMachineLocalAndSecretBearingResponsibilities(t *testing.T) {
	assertReason(t, ReasonInvalidCommand, func() error {
		_, err := NewCommandSpec("../bin/server", nil)
		return err
	})
	assertReason(t, ReasonInvalidCommand, func() error {
		_, err := NewCommandSpec("C:\\Users\\me\\server.exe", nil)
		return err
	})
	assertReason(t, ReasonInvalidPackageRef, func() error {
		_, err := NewPackageRef(EcosystemContainer, "/var/lib/docker/server", "")
		return err
	})
	assertReason(t, ReasonInvalidPackageRef, func() error {
		_, err := NewPackageRef(EcosystemNPM, "server", "1.2.3;rm")
		return err
	})

	envRefs := mustEnv(t, []string{"API_TOKEN", "HOME"})
	if !slices.Equal(envRefs.SourceNames(), []string{"API_TOKEN", "HOME"}) {
		t.Fatalf("EnvBindingSet source names = %#v, want reference names only", envRefs.SourceNames())
	}
}

type packageInput struct {
	ecosystem PackageEcosystem
	name      string
	selector  string
}

func mustPlan(t *testing.T, kind RunnerKind, commandName string, args []string, env []string, pkgInput *packageInput, pin PinPolicy) DelegatePlan {
	t.Helper()
	runner := mustRunner(t, kind)
	command := mustCommand(t, commandName, args)
	envRefs := mustEnv(t, env)
	var packageRef *PackageRef
	if pkgInput != nil {
		ref := mustPackage(t, pkgInput.ecosystem, pkgInput.name, pkgInput.selector)
		packageRef = &ref
	}
	plan, err := NewDelegatePlan(DelegatePlanSpec{
		Runner:     runner,
		Command:    command,
		Env:        envRefs,
		PackageRef: packageRef,
		PinPolicy:  pin,
	})
	if err != nil {
		t.Fatalf("NewDelegatePlan() error = %v", err)
	}
	return plan
}

func mustRunner(t *testing.T, kind RunnerKind) Runner {
	t.Helper()
	runner, err := NewRunner(kind)
	if err != nil {
		t.Fatalf("NewRunner(%q) error = %v", kind, err)
	}
	return runner
}

func mustCommand(t *testing.T, name string, args []string) CommandSpec {
	t.Helper()
	command, err := NewCommandSpec(name, args)
	if err != nil {
		t.Fatalf("NewCommandSpec(%q, %#v) error = %v", name, args, err)
	}
	return command
}

func mustEnv(t *testing.T, names []string) EnvBindingSet {
	t.Helper()
	bindings := make([]EnvBinding, 0, len(names))
	for _, name := range names {
		binding, err := NewEnvBinding(name, name)
		if err != nil {
			t.Fatalf("NewEnvBinding(%q, %q) error = %v", name, name, err)
		}
		bindings = append(bindings, binding)
	}
	envRefs, err := NewEnvBindingSet(bindings)
	if err != nil {
		t.Fatalf("NewEnvBindingSet(%#v) error = %v", names, err)
	}
	return envRefs
}

func mustPackage(t *testing.T, ecosystem PackageEcosystem, name string, selector string) PackageRef {
	t.Helper()
	ref, err := NewPackageRef(ecosystem, name, selector)
	if err != nil {
		t.Fatalf("NewPackageRef(%q, %q, %q) error = %v", ecosystem, name, selector, err)
	}
	return ref
}

func assertReason(t *testing.T, want ReasonCode, run func() error) {
	t.Helper()
	err := run()
	if err == nil {
		t.Fatalf("got nil error, want reason %q", want)
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error %T %v has no reason code", err, err)
	}
	got := validation.Code()
	if got != want {
		t.Fatalf("reason = %q, want %q; err = %v", got, want, err)
	}
}
