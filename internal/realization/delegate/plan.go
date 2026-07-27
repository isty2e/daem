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
	runner     Runner
	command    CommandSpec
	env        EnvBindingSet
	packageRef PackageRef
	hasPackage bool
	pinPolicy  PinPolicy
}

// DelegatePlanSpec contains already-normalized delegate plan inputs.
type DelegatePlanSpec struct {
	Runner     Runner
	Command    CommandSpec
	Env        EnvBindingSet
	PackageRef *PackageRef
	PinPolicy  PinPolicy
}

// NewDelegatePlan validates and constructs a delegated executable plan.
func NewDelegatePlan(spec DelegatePlanSpec) (DelegatePlan, error) {
	if err := validateRunnerKind(spec.Runner.Kind()); err != nil {
		return DelegatePlan{}, err
	}
	if err := validateCommandName(spec.Command.Name()); err != nil {
		return DelegatePlan{}, err
	}
	if err := validatePinPolicy(spec.PinPolicy); err != nil {
		return DelegatePlan{}, err
	}
	if expected, ok := spec.Runner.fixedCommand(); ok && spec.Command.Name() != expected {
		return DelegatePlan{}, validationError(ReasonInvalidDelegatePlan, spec.Command.Name(), "runner requires command "+expected)
	}

	var packageRef PackageRef
	hasPackage := spec.PackageRef != nil
	if hasPackage {
		packageRef = *spec.PackageRef
		if err := validatePackageName(packageRef.Ecosystem(), packageRef.Name()); err != nil {
			return DelegatePlan{}, err
		}
		if err := validatePackageSelector(packageRef.Selector()); err != nil {
			return DelegatePlan{}, err
		}
	}
	if err := validatePlanPackage(spec.Runner, packageRef, hasPackage, spec.PinPolicy); err != nil {
		return DelegatePlan{}, err
	}

	return DelegatePlan{
		runner:     spec.Runner,
		command:    spec.Command,
		env:        spec.Env,
		packageRef: packageRef,
		hasPackage: hasPackage,
		pinPolicy:  spec.PinPolicy,
	}, nil
}

// Runner returns the delegated invocation family.
func (plan DelegatePlan) Runner() Runner { return plan.runner }

// Command returns the delegated argv identity.
func (plan DelegatePlan) Command() CommandSpec { return plan.command }

// Env returns child-process to host-source environment bindings.
func (plan DelegatePlan) Env() EnvBindingSet { return plan.env }

// PackageRef returns package-like identity when the runner needs one.
func (plan DelegatePlan) PackageRef() (PackageRef, bool) {
	return plan.packageRef, plan.hasPackage
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
		return validationError(ReasonInvalidDelegatePlan, plan.command.Name(), "delegate plan is not canonical")
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
	canonicalCommand, err := NewCommandSpec(command.Name(), command.Args())
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
		Command:    plan.command.Name(),
		Args:       plan.command.Args(),
		Env:        identityEnvBindings(plan.env.Bindings()),
		PinPolicy:  plan.pinPolicy,
	}
	if plan.hasPackage {
		payload.Package = &identityPackage{
			Ecosystem: plan.packageRef.Ecosystem(),
			Name:      plan.packageRef.Name(),
			Selector:  plan.packageRef.Selector(),
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return "delegate:v2:" + string(data)
}

func canonicalDelegatePlan(plan DelegatePlan) (DelegatePlan, error) {
	runner, err := NewRunner(plan.runner.Kind())
	if err != nil {
		return DelegatePlan{}, err
	}
	command, err := NewCommandSpec(plan.command.Name(), plan.command.Args())
	if err != nil {
		return DelegatePlan{}, err
	}
	env, err := NewEnvBindingSet(plan.env.Bindings())
	if err != nil {
		return DelegatePlan{}, err
	}
	var packageRef *PackageRef
	if plan.hasPackage {
		ref, err := NewPackageRef(
			plan.packageRef.Ecosystem(),
			plan.packageRef.Name(),
			plan.packageRef.Selector(),
		)
		if err != nil {
			return DelegatePlan{}, err
		}
		packageRef = &ref
	}
	return NewDelegatePlan(DelegatePlanSpec{
		Runner:     runner,
		Command:    command,
		Env:        env,
		PackageRef: packageRef,
		PinPolicy:  plan.pinPolicy,
	})
}

func sameDelegatePlanFacts(left DelegatePlan, right DelegatePlan) bool {
	if left.runner != right.runner ||
		left.pinPolicy != right.pinPolicy ||
		left.hasPackage != right.hasPackage ||
		left.packageRef != right.packageRef ||
		!sameCommandFacts(left.command, right.command) ||
		!sameEnvBindingFacts(left.env, right.env) {
		return false
	}
	return true
}

func sameCommandFacts(left CommandSpec, right CommandSpec) bool {
	return left.name == right.name && slices.Equal(left.args, right.args)
}

func sameEnvBindingFacts(left EnvBindingSet, right EnvBindingSet) bool {
	return slices.Equal(left.bindings, right.bindings)
}

type identityPayload struct {
	RunnerKind RunnerKind       `json:"runner_kind"`
	Command    string           `json:"command"`
	Args       []string         `json:"args"`
	Env        []identityEnv    `json:"env"`
	Package    *identityPackage `json:"package,omitempty"`
	PinPolicy  PinPolicy        `json:"pin_policy"`
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

func validatePinPolicy(policy PinPolicy) error {
	switch policy {
	case PinNotApplicable, PinPinned, PinFloating, PinHostSelected:
		return nil
	default:
		return validationError(ReasonInvalidPinPolicy, string(policy), "unsupported pin policy")
	}
}

func validatePlanPackage(runner Runner, packageRef PackageRef, hasPackage bool, policy PinPolicy) error {
	expectedEcosystem, expectsPackage := runner.packageEcosystem()
	if expectsPackage && !hasPackage {
		return validationError(ReasonInvalidDelegatePlan, string(runner.Kind()), "runner requires package identity")
	}
	if !expectsPackage && hasPackage {
		return validationError(ReasonInvalidDelegatePlan, string(runner.Kind()), "runner must not carry package identity")
	}
	if hasPackage && packageRef.Ecosystem() != expectedEcosystem {
		return validationError(ReasonInvalidDelegatePlan, string(packageRef.Ecosystem()), "package ecosystem does not match runner")
	}

	switch policy {
	case PinNotApplicable:
		if hasPackage || runner.Kind() == RunnerHostNative {
			return validationError(ReasonInvalidDelegatePlan, string(policy), "pin policy is not applicable only for plain ambient executables")
		}
	case PinPinned:
		if !hasPackage {
			return validationError(ReasonInvalidDelegatePlan, string(policy), "pinned policy requires package identity")
		}
		if packageRef.Selector() == "" {
			return validationError(ReasonInvalidDelegatePlan, packageRef.Name(), "pinned package identity requires a selector")
		}
	case PinFloating:
		if !hasPackage {
			return validationError(ReasonInvalidDelegatePlan, string(policy), "floating policy requires package identity")
		}
	case PinHostSelected:
		if runner.Kind() != RunnerHostNative || hasPackage {
			return validationError(ReasonInvalidDelegatePlan, string(policy), "host-selected policy is only for host-native delegates without package identity")
		}
	}
	return nil
}
