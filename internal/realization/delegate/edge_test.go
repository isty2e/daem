package delegate

import "testing"

func TestDelegatePlanEdgeHuntRoundOne(t *testing.T) {
	// Exploit E1: zero-value runner must not become a valid plan.
	assertReason(t, ReasonInvalidRunnerKind, func() error {
		command := mustCommand(t, "node", []string{"server.js"})
		_, err := NewDelegatePlan(DelegatePlanSpec{Command: command})
		return err
	})

	// Exploit E2: zero-value command must not become a valid plan.
	assertReason(t, ReasonInvalidCommand, func() error {
		runner := mustRunner(t, RunnerPlain)
		_, err := NewDelegatePlan(DelegatePlanSpec{Runner: runner})
		return err
	})

	// Exploit E3: returned package facts are defensive copies.
	plan := mustPlan(t, RunnerNPX, "npx", []string{"server@1.0.0"}, nil, &packageInput{
		ecosystem: EcosystemNPM,
		name:      "server",
		selector:  "1.0.0",
	}, PinPinned)
	refs := plan.PackageRefs()
	refs[0] = mustPackage(t, EcosystemNPM, "server", "2.0.0")
	if got := plan.PackageRefs()[0].Selector(); got != "1.0.0" {
		t.Fatalf("PackageRefs()[0].Selector() = %q, want immutable 1.0.0", got)
	}

	// Exploit E4: env refs are requirements by name, not KEY=value literals.
	assertReason(t, ReasonInvalidEnvRef, func() error {
		_, err := NewEnvBinding("API_TOKEN=secret", "API_TOKEN")
		return err
	})

	// Explore X1: non-ASCII confusables must not enter canonical env names.
	assertReason(t, ReasonInvalidEnvRef, func() error {
		_, err := NewEnvBinding("API＿TOKEN", "API_TOKEN")
		return err
	})

	// Explore X2: CRLF-like argv payloads are rejected as control characters.
	assertReason(t, ReasonInvalidArgument, func() error {
		_, err := NewCommandSpec("node", []string{"line\r\nbreak"})
		return err
	})
	assertReason(t, ReasonInvalidArgument, func() error {
		_, err := NewCommandSpec("node", []string{"line\u0085break"})
		return err
	})
}

func TestDelegatePlanEdgeHuntRoundTwo(t *testing.T) {
	// Exploit E1: host-native delegates derive host-selected policy and cannot
	// smuggle package identity outside their argv.
	hostPlan := mustPlan(t, RunnerHostNative, "claude", []string{"plugin", "install", "plugin"}, nil, nil, PinHostSelected)
	if len(hostPlan.PackageRefs()) != 0 {
		t.Fatalf("host-native package refs = %#v, want none", hostPlan.PackageRefs())
	}

	// Exploit E3: floating package identity is representable but not pinned.
	plan := mustPlan(t, RunnerNPX, "npx", []string{"server"}, nil, &packageInput{
		ecosystem: EcosystemNPM,
		name:      "server",
	}, PinFloating)
	if plan.PinPolicy() != PinFloating {
		t.Fatalf("floating delegate plan policy = %q", plan.PinPolicy())
	}

	// Exploit E4: registry ports are allowed only as registry identity, not image tags.
	ref := mustPackage(t, EcosystemContainer, "localhost:5000/acme/server", "sha256:abc123")
	if ref.PinPolicy() != PinFloating {
		t.Fatalf("malformed container digest policy = %q, want floating", ref.PinPolicy())
	}
	assertReason(t, ReasonInvalidPackageRef, func() error {
		_, err := NewPackageRef(EcosystemContainer, "acme/server:latest", "")
		return err
	})

	// Explore X1: malformed scoped npm package names stay invalid.
	for _, name := range []string{"@/server", "@scope/", "@scope/name/extra"} {
		assertReason(t, ReasonInvalidPackageRef, func() error {
			_, err := NewPackageRef(EcosystemNPM, name, "")
			return err
		})
	}

	// Explore X2: selectors are selectors, not paths or shell fragments.
	for _, selector := range []string{"../1.0.0", "1.0.0 ", "`whoami`"} {
		assertReason(t, ReasonInvalidPackageRef, func() error {
			_, err := NewPackageRef(EcosystemNPM, "server", selector)
			return err
		})
	}
}

func TestDelegatePlanEdgeHuntRoundThree(t *testing.T) {
	// Exploit E1: pin policy is derived from every package selector.
	pinned := mustPlan(t, RunnerNPX, "npx", []string{"server@1.0.0"}, nil, &packageInput{
		ecosystem: EcosystemNPM,
		name:      "server",
		selector:  "1.0.0",
	}, PinPinned)
	mixed := mustPlan(t, RunnerNPX, "npx", []string{"--package=server@1.0.0", "--package=helper@latest", "server"}, nil, nil, PinFloating)
	if len(mixed.PackageRefs()) != 2 {
		t.Fatalf("mixed PackageRefs() = %#v, want two complete package inputs", mixed.PackageRefs())
	}

	// Exploit E2: package selectors participate in identity.
	otherVersion := mustPlan(t, RunnerNPX, "npx", []string{"server@2.0.0"}, nil, &packageInput{
		ecosystem: EcosystemNPM,
		name:      "server",
		selector:  "2.0.0",
	}, PinPinned)
	if pinned.IdentityKey() == otherVersion.IdentityKey() {
		t.Fatalf("IdentityKey() collapsed package selectors")
	}

	// Exploit E3: runner fixed command matching is case-sensitive and exact.
	assertReason(t, ReasonInvalidDelegatePlan, func() error {
		runner := mustRunner(t, RunnerNPX)
		command := mustCommand(t, "NPX", []string{"server"})
		_, err := NewDelegatePlan(DelegatePlanSpec{
			Runner:  runner,
			Command: command,
		})
		return err
	})

	// Exploit E4: unknown package ecosystems are refused before plan construction.
	assertReason(t, ReasonInvalidPackageRef, func() error {
		_, err := NewPackageRef(PackageEcosystem("brew"), "server", "")
		return err
	})

	// Explore X1: shell metacharacter commands stay invalid.
	for _, command := range []string{"$(npx)", "`npx`", "npx|cat"} {
		assertReason(t, ReasonInvalidCommand, func() error {
			_, err := NewCommandSpec(command, nil)
			return err
		})
	}

	// Explore X2: spaces inside argv entries are preserved as argv identity, not shell parsing.
	planWithSpaceArg := mustPlan(t, RunnerPlain, "node", []string{"--label", "two words"}, nil, nil, PinNotApplicable)
	if got := planWithSpaceArg.Command().Args()[1]; got != "two words" {
		t.Fatalf("argv argument with space = %q, want preserved literal", got)
	}
}
