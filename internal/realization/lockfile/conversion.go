package lockfile

import (
	"fmt"
	"reflect"

	"github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

type snapshotAdmission struct {
	version  int
	section  func([]lock.LockedSubjectContract, []hostrelation.RelationOrderConstraint) (lock.LockedSection, error)
	validate func(lock.File) error
}

func defaultSnapshotAdmission() snapshotAdmission {
	return snapshotAdmission{lock.CurrentVersion, lock.NewLockedSection, lock.Validate}
}

func nativeSnapshotAdmission() snapshotAdmission {
	return snapshotAdmission{lock.NativeVersion, lock.NewNativeLockedSection, lock.ValidateNative}
}

func snapshotFromDTO(dto fileDTO) (lock.File, error) {
	return snapshotFromDTOWithAdmission(dto, defaultSnapshotAdmission())
}

func snapshotFromDTOWithAdmission(dto fileDTO, admission snapshotAdmission) (lock.File, error) {
	if dto.Version != int64(admission.version) {
		return lock.File{}, UnsupportedVersionError{
			Found:     dto.Version,
			Supported: int64(admission.version),
		}
	}

	subjects, err := subjectsFromDTO(dto.Locked.Subjects)
	if err != nil {
		return lock.File{}, err
	}
	orderConstraints, err := orderConstraintsFromDTO(dto.Locked.OrderConstraints)
	if err != nil {
		return lock.File{}, err
	}
	locked, err := admission.section(subjects, orderConstraints)
	if err != nil {
		return lock.File{}, err
	}
	return lock.File{
		Version: admission.version,
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
		Version: int64(file.Version),
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
