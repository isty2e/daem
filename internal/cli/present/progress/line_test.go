package progress

import (
	"bytes"
	"errors"
	"testing"
)

func TestEphemeralLineSuppressesEquivalentValuesAndClosesOnce(t *testing.T) {
	var output bytes.Buffer
	line := newEphemeralLine(&output)

	line.write("working")
	line.write("working")
	line.close()
	line.close()

	const want = "\r\x1b[2Kworking\r\x1b[2K"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestEphemeralLineWithNilOutputIsNoop(t *testing.T) {
	line := newEphemeralLine(nil)

	line.write("working")
	line.close()
	line.close()

	if line.active || line.disabled || line.lastLine != "" {
		t.Fatalf("nil-output line changed state: %#v", line)
	}
}

func TestEphemeralLineDisablesPermanentlyAfterWriteFailure(t *testing.T) {
	writer := &failingLineWriter{failAt: 2}
	line := newEphemeralLine(writer)

	line.write("one")
	line.write("two")
	line.write("three")
	line.close()

	if writer.attempts != 2 {
		t.Fatalf("write attempts = %d, want 2", writer.attempts)
	}
	if !line.disabled {
		t.Fatal("line remained enabled after write failure")
	}
}

func TestEphemeralLineDisablesPermanentlyAfterClearFailure(t *testing.T) {
	writer := &failingLineWriter{failAt: 2}
	line := newEphemeralLine(writer)

	line.write("one")
	line.close()
	line.close()
	line.write("two")

	if writer.attempts != 2 {
		t.Fatalf("write attempts = %d, want 2", writer.attempts)
	}
	if !line.disabled || line.active {
		t.Fatalf("line state = %#v, want disabled and inactive", line)
	}
}

type failingLineWriter struct {
	buffer   bytes.Buffer
	attempts int
	failAt   int
}

func (writer *failingLineWriter) Write(content []byte) (int, error) {
	writer.attempts++
	if writer.attempts == writer.failAt {
		return 0, errors.New("stderr closed")
	}
	return writer.buffer.Write(content)
}
