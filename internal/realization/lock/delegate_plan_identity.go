package lock

import (
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/realization/delegate"
)

// DelegatePackageIdentity is the lock-portable package identity for a delegate plan.
type DelegatePackageIdentity struct {
	Ecosystem delegate.PackageEcosystem
	Name      string
	Selector  string
}

// DelegateEnvBinding is the lock-portable child-name to host-source mapping.
type DelegateEnvBinding struct {
	Name       string
	SourceName string
}

// DelegatePlanIdentity is the lock-owned executable invocation identity facet.
type DelegatePlanIdentity struct {
	IdentityKey string
	RunnerKind  delegate.RunnerKind
	Command     string
	Args        []string
	Env         []DelegateEnvBinding
	Package     *DelegatePackageIdentity
	PinPolicy   delegate.PinPolicy
}

// NewDelegatePlanIdentity validates and canonicalizes a stored delegate plan identity.
func NewDelegatePlanIdentity(input DelegatePlanIdentity) (DelegatePlanIdentity, error) {
	plan, err := delegatePlanFromIdentity(input)
	if err != nil {
		return DelegatePlanIdentity{}, err
	}
	canonical := DelegatePlanIdentityFromPlan(plan)
	if strings.TrimSpace(input.IdentityKey) == "" {
		return DelegatePlanIdentity{}, fmt.Errorf("delegate plan identity key is required")
	}
	if input.IdentityKey != canonical.IdentityKey {
		return DelegatePlanIdentity{}, fmt.Errorf("delegate plan identity key does not match canonical plan identity")
	}
	return canonical, nil
}

// DelegatePlanIdentityFromPlan copies the canonical delegate plan facts into lock-owned form.
func DelegatePlanIdentityFromPlan(plan delegate.DelegatePlan) DelegatePlanIdentity {
	identity := DelegatePlanIdentity{
		IdentityKey: plan.IdentityKey(),
		RunnerKind:  plan.Runner().Kind(),
		Command:     plan.Command().Name(),
		Args:        plan.Command().Args(),
		Env:         delegateEnvBindingsFromPlan(plan.Env()),
		PinPolicy:   plan.PinPolicy(),
	}
	if packageRef, ok := plan.PackageRef(); ok {
		identity.Package = &DelegatePackageIdentity{
			Ecosystem: packageRef.Ecosystem(),
			Name:      packageRef.Name(),
			Selector:  packageRef.Selector(),
		}
	}
	return identity
}

// CorrelatesInvocation reports whether command, argv, and credential
// references identify this exact locked delegate invocation.
func (identity DelegatePlanIdentity) CorrelatesInvocation(
	command string,
	args []string,
	env []DelegateEnvBinding,
) bool {
	canonical, err := NewDelegatePlanIdentity(identity)
	if err != nil {
		return false
	}
	canonicalEnv, err := newDelegateEnvBindingSet(env)
	if err != nil {
		return false
	}
	return canonical.Command == command &&
		slices.Equal(canonical.Args, args) &&
		slices.Equal(canonical.Env, canonicalEnv)
}

func delegatePlanFromIdentity(identity DelegatePlanIdentity) (delegate.DelegatePlan, error) {
	runner, err := delegate.NewRunner(identity.RunnerKind)
	if err != nil {
		return delegate.DelegatePlan{}, err
	}
	command, err := delegate.NewCommandSpec(identity.Command, identity.Args)
	if err != nil {
		return delegate.DelegatePlan{}, err
	}
	env, err := delegateEnvBindingSetFromIdentity(identity.Env)
	if err != nil {
		return delegate.DelegatePlan{}, err
	}

	var packageRef *delegate.PackageRef
	if identity.Package != nil {
		ref, err := delegate.NewPackageRef(identity.Package.Ecosystem, identity.Package.Name, identity.Package.Selector)
		if err != nil {
			return delegate.DelegatePlan{}, err
		}
		packageRef = &ref
	}
	return delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner:     runner,
		Command:    command,
		Env:        env,
		PackageRef: packageRef,
		PinPolicy:  identity.PinPolicy,
	})
}

func cloneDelegatePlanIdentity(identity DelegatePlanIdentity) DelegatePlanIdentity {
	cloned := DelegatePlanIdentity{
		IdentityKey: strings.TrimSpace(identity.IdentityKey),
		RunnerKind:  identity.RunnerKind,
		Command:     identity.Command,
		Args:        append([]string(nil), identity.Args...),
		Env:         cloneDelegateEnvBindings(identity.Env),
		PinPolicy:   identity.PinPolicy,
	}
	if identity.Package != nil {
		cloned.Package = &DelegatePackageIdentity{
			Ecosystem: identity.Package.Ecosystem,
			Name:      strings.TrimSpace(identity.Package.Name),
			Selector:  strings.TrimSpace(identity.Package.Selector),
		}
	}
	return cloned
}

// EnvSourceNames returns deterministic unique host environment source names.
func (identity DelegatePlanIdentity) EnvSourceNames() []string {
	canonical, err := NewDelegatePlanIdentity(identity)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(canonical.Env))
	names := make([]string, 0, len(canonical.Env))
	for _, binding := range canonical.Env {
		if _, ok := seen[binding.SourceName]; ok {
			continue
		}
		seen[binding.SourceName] = struct{}{}
		names = append(names, binding.SourceName)
	}
	return normalizeStrings(names)
}

func delegateEnvBindingsFromPlan(set delegate.EnvBindingSet) []DelegateEnvBinding {
	bindings := set.Bindings()
	result := make([]DelegateEnvBinding, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, DelegateEnvBinding{
			Name:       binding.Name(),
			SourceName: binding.SourceName(),
		})
	}
	return result
}

func delegateEnvBindingSetFromIdentity(values []DelegateEnvBinding) (delegate.EnvBindingSet, error) {
	bindings := make([]delegate.EnvBinding, 0, len(values))
	for _, value := range values {
		binding, err := delegate.NewEnvBinding(value.Name, value.SourceName)
		if err != nil {
			return delegate.EnvBindingSet{}, err
		}
		bindings = append(bindings, binding)
	}
	return delegate.NewEnvBindingSet(bindings)
}

func newDelegateEnvBindingSet(values []DelegateEnvBinding) ([]DelegateEnvBinding, error) {
	set, err := delegateEnvBindingSetFromIdentity(values)
	if err != nil {
		return nil, err
	}
	return delegateEnvBindingsFromPlan(set), nil
}

func cloneDelegateEnvBindings(values []DelegateEnvBinding) []DelegateEnvBinding {
	return append([]DelegateEnvBinding(nil), values...)
}
