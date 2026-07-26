package journal

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
)

type failingStateCodec struct {
	encodeErr error
	decodeErr error
}

func (codec failingStateCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	if codec.encodeErr != nil {
		return nil, codec.encodeErr
	}
	return testStateCodec().Encode(snapshot)
}

func (codec failingStateCodec) Decode(content []byte) (durable.Snapshot, error) {
	if codec.decodeErr != nil {
		return durable.Snapshot{}, codec.decodeErr
	}
	return testStateCodec().Decode(content)
}

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

type invalidStateEncoder struct{}

func (invalidStateEncoder) Encode(durable.Snapshot) ([]byte, error) {
	return []byte("not-json"), nil
}

type failAtCallStateEncoder struct {
	calls     int
	failureAt int
	err       error
}

func (encoder *failAtCallStateEncoder) Encode(snapshot durable.Snapshot) ([]byte, error) {
	encoder.calls++
	if encoder.calls == encoder.failureAt {
		return nil, encoder.err
	}
	return testStateCodec().Encode(snapshot)
}

type reusingBufferStateEncoder struct {
	buffer []byte
}

func (encoder *reusingBufferStateEncoder) Encode(snapshot durable.Snapshot) ([]byte, error) {
	content, err := testStateCodec().Encode(snapshot)
	if err != nil {
		return nil, err
	}
	encoder.buffer = append(encoder.buffer[:0], content...)
	return encoder.buffer, nil
}

func testStateReader(path string) durable.SnapshotReader {
	return func(ctx context.Context) (durable.Snapshot, error) {
		return statefile.LoadOptional(ctx, path)
	}
}
