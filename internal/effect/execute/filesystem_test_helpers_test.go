package execute

import (
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func testFilesystem() mutationfs.Store {
	return storagecommit.Adapter{}
}

type statefileRefusal struct{ error }

func (failure *statefileRefusal) Kind() mutationfs.FailureKind { return mutationfs.FailureUncommitted }
func (failure *statefileRefusal) Unwrap() error                { return failure.error }
