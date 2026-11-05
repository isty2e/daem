package record

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func testContentHash(seed string) artifact.ContentHash {
	contentHash := artifact.ContentHash(seed)
	if contentHash.Validate() == nil {
		return contentHash
	}
	return artifact.HashFileContent([]byte(seed))
}

func testContentHashString(seed string) string {
	return string(testContentHash(seed))
}

func journalTestCodecs() aggregate.CodecCatalog {
	return aggregatecodec.Catalog()
}

func journalTestFilesystem() mutationfs.Store {
	return storagecommit.Adapter{}
}

func testStateCodec() durable.SnapshotCodec {
	return statefile.Codec{}
}
