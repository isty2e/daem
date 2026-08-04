package delegate

import (
	"encoding/json"
	"slices"
)

// PinPolicy describes whether a delegated package outcome is constrained.
type PinPolicy string

const (
	PinNotApplicable PinPolicy = "not_applicable"
	PinPinned        PinPolicy = "pinned"
	PinFloating      PinPolicy = "floating"
	PinHostSelected  PinPolicy = "host_selected"
)

// DelegatePlan is the canonical, lockable delegated executable invocation identity.
type DelegatePlan struct {
	runner      Runner
	command     CommandSpec
	env         EnvBindingSet
	packageRefs []PackageRef
	pinPolicy   PinPolicy
}

// DelegatePlanSpec contains already-normalized delegate plan inputs.
type DelegatePlanSpec struct {
	Runner  Runner
	Command CommandSpec
	Env     EnvBindingSet
}

// NewDelegatePlan validates and constructs a delegated executable plan.
func NewDelegatePlan(spec DelegatePlanSpec) (DelegatePlan, error) {
	if err := validateRunnerKind(spec.Runner.Kind()); err != nil {
		return DelegatePlan{}, err
	}
	if err := validateCommandExecutable(spec.Command.Executable()); err != nil {
		return DelegatePlan{}, err
	}
	if expected, ok := spec.Runner.fixedCommand(); ok && spec.Command.Executable() != expected {
		return DelegatePlan{}, validationError(ReasonInvalidDelegatePlan, spec.Command.Executable(), "runner requires command "+expected)
	}
	resolution, err := derivePackageResolution(spec.Runner, spec.Command)
	if err != nil {
		return DelegatePlan{}, err
	}
	pinPolicy := packageResolutionPinPolicy(spec.Runner, resolution)

	return DelegatePlan{
		runner:      spec.Runner,
		command:     spec.Command,
		env:         spec.Env,
		packageRefs: resolution.refs,
		pinPolicy:   pinPolicy,
	}, nil
}

// Runner returns the delegated invocation family.
func (plan DelegatePlan) Runner() Runner { return plan.runner }

// Command returns the delegated argv identity.
func (plan DelegatePlan) Command() CommandSpec { return plan.command }

// Env returns child-process to host-source environment bindings.
func (plan DelegatePlan) Env() EnvBindingSet { return plan.env }

// PackageRefs returns canonical package inputs recognized from the exact argv.
// A floating policy can accompany a partial set when a runner input is opaque.
func (plan DelegatePlan) PackageRefs() []PackageRef {
	return append([]PackageRef(nil), plan.packageRefs...)
}

// PinPolicy returns package outcome constraint semantics.
func (plan DelegatePlan) PinPolicy() PinPolicy { return plan.pinPolicy }

// Validate rejects a zero, forged, or non-canonical delegate plan.
func (plan DelegatePlan) Validate() error {
	canonical, err := canonicalDelegatePlan(plan)
	if err != nil {
		return err
	}
	if !sameDelegatePlanFacts(plan, canonical) {
		return validationError(ReasonInvalidDelegatePlan, plan.command.Executable(), "delegate plan is not canonical")
	}
	return nil
}

// Equal reports whether two valid plans contain the same executable identity.
func (plan DelegatePlan) Equal(other DelegatePlan) bool {
	return plan.Validate() == nil &&
		other.Validate() == nil &&
		sameDelegatePlanFacts(plan, other)
}

// CorrelatesInvocation reports whether one canonical command and environment
// binding set identify this plan's exact process invocation.
func (plan DelegatePlan) CorrelatesInvocation(command CommandSpec, env EnvBindingSet) bool {
	if plan.Validate() != nil {
		return false
	}
	canonicalCommand, err := NewCommandSpec(command.Executable(), command.Args())
	if err != nil || !sameCommandFacts(command, canonicalCommand) {
		return false
	}
	canonicalEnv, err := NewEnvBindingSet(env.Bindings())
	if err != nil || !sameEnvBindingFacts(env, canonicalEnv) {
		return false
	}
	return sameCommandFacts(plan.command, canonicalCommand) &&
		sameEnvBindingFacts(plan.env, canonicalEnv)
}

// IdentityKey returns a stable display-independent identity string.
func (plan DelegatePlan) IdentityKey() string {
	payload := identityPayload{
		RunnerKind: plan.runner.Kind(),
		Command:    plan.command.Executable(),
		Args:       plan.command.Args(),
		Env:        identityEnvBindings(plan.env.Bindings()),
		PinPolicy:  plan.pinPolicy,
	}
	for _, ref := range plan.packageRefs {
		payload.Packages = append(payload.Packages, identityPackage{
			Ecosystem: ref.Ecosystem(),
			Name:      ref.Name(),
			Selector:  ref.Selector(),
		})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return "delegate:v3:" + string(data)
}

func canonicalDelegatePlan(plan DelegatePlan) (DelegatePlan, error) {
	runner, err := NewRunner(plan.runner.Kind())
	if err != nil {
		return DelegatePlan{}, err
	}
	command, err := NewCommandSpec(plan.command.Executable(), plan.command.Args())
	if err != nil {
		return DelegatePlan{}, err
	}
	env, err := NewEnvBindingSet(plan.env.Bindings())
	if err != nil {
		return DelegatePlan{}, err
	}
	return NewDelegatePlan(DelegatePlanSpec{
		Runner:  runner,
		Command: command,
		Env:     env,
	})
}

func sameDelegatePlanFacts(left DelegatePlan, right DelegatePlan) bool {
	if left.runner != right.runner ||
		left.pinPolicy != right.pinPolicy ||
		!slices.Equal(left.packageRefs, right.packageRefs) ||
		!sameCommandFacts(left.command, right.command) ||
		!sameEnvBindingFacts(left.env, right.env) {
		return false
	}
	return true
}

func sameCommandFacts(left CommandSpec, right CommandSpec) bool {
	return left.executable == right.executable && slices.Equal(left.args, right.args)
}

func sameEnvBindingFacts(left EnvBindingSet, right EnvBindingSet) bool {
	return slices.Equal(left.bindings, right.bindings)
}

type identityPayload struct {
	RunnerKind RunnerKind        `json:"runner_kind"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        []identityEnv     `json:"env"`
	Packages   []identityPackage `json:"packages,omitempty"`
	PinPolicy  PinPolicy         `json:"pin_policy"`
}

type identityEnv struct {
	Name       string `json:"name"`
	SourceName string `json:"source_name"`
}

type identityPackage struct {
	Ecosystem PackageEcosystem `json:"ecosystem"`
	Name      string           `json:"name"`
	Selector  string           `json:"selector,omitempty"`
}

func identityEnvBindings(bindings []EnvBinding) []identityEnv {
	result := make([]identityEnv, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, identityEnv{
			Name:       binding.Name(),
			SourceName: binding.SourceName(),
		})
	}
	return result
}
