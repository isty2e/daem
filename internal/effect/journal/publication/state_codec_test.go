package publication

import (
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

type invalidStateEncoder struct{}

func (invalidStateEncoder) Encode(durable.Snapshot) ([]byte, error) {
	return []byte("not-json"), nil
}

func (invalidStateEncoder) Decode(content []byte) (durable.Snapshot, error) {
	return testStateCodec().Decode(content)
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

func (*failAtCallStateEncoder) Decode(content []byte) (durable.Snapshot, error) {
	return testStateCodec().Decode(content)
}
