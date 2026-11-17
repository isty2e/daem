package recoveryoperation

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
)

func testStateCodec() durable.SnapshotCodec {
	return statefile.Codec{}
}

type countingStateEncoder struct {
	calls int
}

func (encoder *countingStateEncoder) Encode(snapshot durable.Snapshot) ([]byte, error) {
	encoder.calls++
	return testStateCodec().Encode(snapshot)
}

func (*countingStateEncoder) Decode(content []byte) (durable.Snapshot, error) {
	return testStateCodec().Decode(content)
}

func testStateReader(path string) durable.SnapshotReader {
	return func(ctx context.Context) (durable.Snapshot, error) {
		return statefile.LoadOptional(ctx, path)
	}
}
