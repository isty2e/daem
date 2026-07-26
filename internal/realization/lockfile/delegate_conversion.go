package lockfile

import (
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/lock"
)

func delegatePlanFromDTO(dto *delegatePlanDTO) (*lock.DelegatePlanIdentity, error) {
	if dto == nil {
		return nil, nil
	}
	identity := lock.DelegatePlanIdentity{
		IdentityKey: dto.IdentityKey,
		RunnerKind:  delegate.RunnerKind(dto.RunnerKind),
		Command:     dto.Command,
		Args:        dto.Args,
		Env:         delegateEnvFromDTO(dto.Env),
		PinPolicy:   delegate.PinPolicy(dto.PinPolicy),
	}
	if dto.Package != nil {
		identity.Package = &lock.DelegatePackageIdentity{
			Ecosystem: delegate.PackageEcosystem(dto.Package.Ecosystem),
			Name:      dto.Package.Name,
			Selector:  dto.Package.Selector,
		}
	}
	canonical, err := lock.NewDelegatePlanIdentity(identity)
	if err != nil {
		return nil, err
	}
	return &canonical, nil
}

func delegatePlanToDTO(contract lock.LockedSubjectContract) *delegatePlanDTO {
	identity, ok := contract.DelegatePlanIdentity()
	if !ok {
		return nil
	}
	dto := &delegatePlanDTO{
		IdentityKey: identity.IdentityKey,
		RunnerKind:  string(identity.RunnerKind),
		Command:     identity.Command,
		Args:        identity.Args,
		Env:         delegateEnvToDTO(identity.Env),
		PinPolicy:   string(identity.PinPolicy),
	}
	if identity.Package != nil {
		dto.Package = &delegatePackageDTO{
			Ecosystem: string(identity.Package.Ecosystem),
			Name:      identity.Package.Name,
			Selector:  identity.Package.Selector,
		}
	}
	return dto
}

func delegateEnvFromDTO(values []delegateEnvDTO) []lock.DelegateEnvBinding {
	if len(values) == 0 {
		return nil
	}
	result := make([]lock.DelegateEnvBinding, 0, len(values))
	for _, value := range values {
		result = append(result, lock.DelegateEnvBinding{
			Name:       value.Name,
			SourceName: value.SourceName,
		})
	}
	return result
}

func delegateEnvToDTO(values []lock.DelegateEnvBinding) []delegateEnvDTO {
	if len(values) == 0 {
		return nil
	}
	result := make([]delegateEnvDTO, 0, len(values))
	for _, value := range values {
		result = append(result, delegateEnvDTO{
			Name:       value.Name,
			SourceName: value.SourceName,
		})
	}
	return result
}
