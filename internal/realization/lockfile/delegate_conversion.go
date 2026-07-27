package lockfile

import (
	"fmt"

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
	var packageRef *delegate.PackageRef
	if dto.Package != nil {
		value, err := delegate.NewPackageRef(
			delegate.PackageEcosystem(dto.Package.Ecosystem),
			dto.Package.Name,
			dto.Package.Selector,
		)
		if err != nil {
			return nil, err
		}
		packageRef = &value
	}
	plan, err := delegate.NewDelegatePlan(delegate.DelegatePlanSpec{
		Runner:     runner,
		Command:    command,
		Env:        env,
		PackageRef: packageRef,
		PinPolicy:  delegate.PinPolicy(dto.PinPolicy),
	})
	if err != nil {
		return nil, err
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
	if packageRef, present := plan.PackageRef(); present {
		dto.Package = &delegatePackageDTO{
			Ecosystem: string(packageRef.Ecosystem()),
			Name:      packageRef.Name(),
			Selector:  packageRef.Selector(),
		}
	}
	return dto
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
