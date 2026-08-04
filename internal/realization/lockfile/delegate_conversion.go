package lockfile

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/lock"
)

func delegatePlanFromDTO(dto *delegatePlanDTO) (*delegate.DelegatePlan, error) {
	if dto == nil {
		return nil, nil
	}
	runner, err := delegate.NewRunner(delegate.RunnerKind(dto.RunnerKind))
	if err != nil {
		return nil, err
	}
	command, err := delegate.NewCommandSpec(dto.Command, dto.Args)
	if err != nil {
		return nil, err
	}
	env, err := delegateEnvFromDTO(dto.Env)
	if err != nil {
		return nil, err
	}
	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner:  runner,
		Command: command,
		Env:     env,
	})
	if err != nil {
		return nil, err
	}
	packageRefs, err := delegatePackagesFromDTO(dto.Packages)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(packageRefs, plan.PackageRefs()) {
		return nil, fmt.Errorf("delegate plan packages do not match canonical command inputs")
	}
	if dto.PinPolicy != string(plan.PinPolicy()) {
		return nil, fmt.Errorf("delegate plan pin policy does not match canonical package assurance")
	}
	if dto.IdentityKey != plan.IdentityKey() {
		return nil, fmt.Errorf("delegate plan identity key does not match canonical plan")
	}
	return &plan, nil
}

func delegatePlanToDTO(contract lock.LockedSubjectContract) *delegatePlanDTO {
	plan, ok := contract.DelegatePlan()
	if !ok {
		return nil
	}
	command := plan.Command()
	dto := &delegatePlanDTO{
		IdentityKey: plan.IdentityKey(),
		RunnerKind:  string(plan.Runner().Kind()),
		Command:     command.Executable(),
		Args:        command.Args(),
		Env:         delegateEnvToDTO(plan.Env()),
		PinPolicy:   string(plan.PinPolicy()),
	}
	dto.Packages = delegatePackagesToDTO(plan.PackageRefs())
	return dto
}

func delegatePackagesFromDTO(values []delegatePackageDTO) ([]delegate.PackageRef, error) {
	result := make([]delegate.PackageRef, 0, len(values))
	for _, value := range values {
		ref, err := delegate.NewPackageRef(
			delegate.PackageEcosystem(value.Ecosystem),
			value.Name,
			value.Selector,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, ref)
	}
	return result, nil
}

func delegatePackagesToDTO(values []delegate.PackageRef) []delegatePackageDTO {
	if len(values) == 0 {
		return nil
	}
	result := make([]delegatePackageDTO, 0, len(values))
	for _, value := range values {
		result = append(result, delegatePackageDTO{
			Ecosystem: string(value.Ecosystem()),
			Name:      value.Name(),
			Selector:  value.Selector(),
		})
	}
	return result
}

func delegateEnvFromDTO(values []delegateEnvDTO) (delegate.EnvBindingSet, error) {
	result := make([]delegate.EnvBinding, 0, len(values))
	for _, value := range values {
		binding, err := delegate.NewEnvBinding(value.Name, value.SourceName)
		if err != nil {
			return delegate.EnvBindingSet{}, err
		}
		result = append(result, binding)
	}
	return delegate.NewEnvBindingSet(result)
}

func delegateEnvToDTO(env delegate.EnvBindingSet) []delegateEnvDTO {
	values := env.Bindings()
	if len(values) == 0 {
		return nil
	}
	result := make([]delegateEnvDTO, 0, len(values))
	for _, value := range values {
		result = append(result, delegateEnvDTO{
			Name:       value.Name(),
			SourceName: value.SourceName(),
		})
	}
	return result
}
