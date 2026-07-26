package journal

import (
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func journalTestFilesystem() mutationfs.Store {
	return storagecommit.Adapter{}
}
