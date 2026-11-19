package lock

import (
	"context"
	"errors"
	"os"

	"github.com/isty2e/daem/internal/declarationartifact"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
)

// PreviousLockStatus describes the available evidence about the old lockfile.
type PreviousLockStatus string

const (
	PreviousMissing    PreviousLockStatus = "missing"
	PreviousComparable PreviousLockStatus = "comparable"
	PreviousLegacy     PreviousLockStatus = "legacy"
	PreviousDamaged    PreviousLockStatus = "damaged"
)

func (status PreviousLockStatus) comparisonAvailable() bool {
	return status == PreviousComparable || status == PreviousMissing
}

type previousLock struct {
	status   PreviousLockStatus
	snapshot lock.File
}

func readPreviousLock(ctx context.Context, path string, standalone bool) (previousLock, error) {
	if standalone {
		content, err := declarationartifact.Read(ctx, path)
		if err != nil {
			return previousReadFailure(err)
		}
		return decodePreviousLock(ctx, content)
	}
	file, err := lockfile.Load(ctx, path)
	if err != nil {
		return previousReadFailure(err)
	}
	return previousLock{status: PreviousComparable, snapshot: file}, nil
}

func previousReadFailure(err error) (previousLock, error) {
	// The declaration reader wraps an unresolved final symlink differently
	// from an absent entry. Do not unwrap its referent failure into absence.
	if os.IsNotExist(err) {
		return missingPreviousLock(), nil
	}
	return previousLock{}, err
}

func missingPreviousLock() previousLock {
	return previousLock{status: PreviousMissing, snapshot: lock.File{Version: lock.CurrentVersion}}
}

func decodePreviousLock(ctx context.Context, content []byte) (previousLock, error) {
	file, err := lockfile.DecodeForRelock(ctx, content)
	if err == nil {
		return previousLock{status: PreviousComparable, snapshot: file}, nil
	}
	var versionErr lockfile.UnsupportedVersionError
	switch {
	case errors.Is(err, lockfile.ErrDamagedCurrent):
		return previousLock{status: PreviousDamaged}, nil
	case errors.As(err, &versionErr) && versionErr.RelockSupported():
		return previousLock{status: PreviousLegacy}, nil
	default:
		return previousLock{}, err
	}
}
