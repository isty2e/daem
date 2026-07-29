package lockfile

import (
	"fmt"
	"reflect"

	"github.com/isty2e/daem/internal/realization/lock"
)

func snapshotFromDTO(dto fileDTO) (lock.File, error) {
	subjects, err := subjectsFromDTO(dto.Locked.Subjects)
	if err != nil {
		return lock.File{}, err
	}
	orderConstraints, err := orderConstraintsFromDTO(dto.Locked.OrderConstraints)
	if err != nil {
		return lock.File{}, err
	}
	locked, err := lock.NewLockedSection(subjects, orderConstraints)
	if err != nil {
		return lock.File{}, err
	}
	return lock.File{
		Version: dto.Version,
		Locked:  locked,
	}, nil
}

func dtoFromSnapshot(file lock.File) (fileDTO, error) {
	subjects, err := subjectsToDTO(file.Locked.Subjects())
	if err != nil {
		return fileDTO{}, err
	}
	orderConstraints, err := orderConstraintsToDTO(file.Locked.OrderConstraints())
	if err != nil {
		return fileDTO{}, err
	}
	return fileDTO{
		Version: file.Version,
		Locked: lockedSectionDTO{
			Subjects:         subjects,
			OrderConstraints: orderConstraints,
		},
	}, nil
}

func validateCanonicalDTO(decoded fileDTO, canonical lock.File) error {
	reencoded, err := dtoFromSnapshot(canonical)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(decoded, reencoded) {
		return fmt.Errorf("lockfile contains non-canonical values")
	}
	return nil
}
