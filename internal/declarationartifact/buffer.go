package declarationartifact

import (
	"bytes"
	"fmt"
)

// OutputBuffer accumulates one generated declaration artifact within the
// shared byte ceiling.
type OutputBuffer struct {
	output       bytes.Buffer
	maximumBytes int64
}

// NewOutputBuffer constructs an empty declaration artifact output buffer.
func NewOutputBuffer() OutputBuffer {
	return OutputBuffer{maximumBytes: MaximumBytes}
}

// Write implements io.Writer without admitting a partial over-limit write.
func (buffer *OutputBuffer) Write(content []byte) (int, error) {
	if int64(buffer.output.Len())+int64(len(content)) > buffer.maximumBytes {
		return 0, fmt.Errorf(
			"encode declaration artifact: %w (maximum %d bytes)",
			ErrTooLarge,
			buffer.maximumBytes,
		)
	}
	return buffer.output.Write(content)
}

// Bytes returns the generated declaration bytes.
func (buffer *OutputBuffer) Bytes() []byte {
	return buffer.output.Bytes()
}
