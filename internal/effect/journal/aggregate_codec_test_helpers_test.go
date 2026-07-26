package journal

import (
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
)

func journalTestCodecs() aggregate.CodecCatalog {
	return aggregatecodec.Catalog()
}
