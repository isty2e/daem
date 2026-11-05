package qualification

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type NativeRecoveryOptions struct {
	Filesystem         mutationfs.RootedStore
	OperationAuthority *rootedpath.EntryAuthority
	State              durable.SnapshotReader
	Registry           ownershipmutation.RegistryReader
	Codecs             aggregate.CodecCatalog
}
