package record

import "github.com/isty2e/daem/internal/assurance/durable"

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

func (*reusingBufferStateEncoder) Decode(content []byte) (durable.Snapshot, error) {
	return testStateCodec().Decode(content)
}
