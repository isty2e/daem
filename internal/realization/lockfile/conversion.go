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
	locked, err := lock.NewLockedSection(subjects)
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
	return fileDTO{
		Version: file.Version,
		Locked:  lockedSectionDTO{Subjects: subjects},
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
