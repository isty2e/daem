package declarationartifact

import (
	"errors"
	"testing"
)

func TestOutputBufferRejectsOutputBeforeExceedingLimit(t *testing.T) {
	t.Parallel()

	output := OutputBuffer{maximumBytes: 4}
	if count, err := output.Write([]byte("1234")); err != nil || count != 4 {
		t.Fatalf("exact write = (%d, %v)", count, err)
	}
	if count, err := output.Write([]byte("5")); !errors.Is(err, ErrTooLarge) || count != 0 {
		t.Fatalf("oversized write = (%d, %v), want zero and ErrTooLarge", count, err)
	}
	if got := string(output.Bytes()); got != "1234" {
		t.Fatalf("buffer = %q after rejected write", got)
	}
}
