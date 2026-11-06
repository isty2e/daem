package nativeoperation

import (
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

func testFilesystem() mutationfs.Store { return storagecommit.Adapter{} }

func testOwnershipRegistryBinder() ownershipmutation.RootedRegistryBinder {
	return ownershipstore.BindRooted
}
