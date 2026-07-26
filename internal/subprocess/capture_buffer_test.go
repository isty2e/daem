package subprocess

import "testing"

func TestBoundedBufferConsumesInputAndRetainsOnlyBoundedPrefix(t *testing.T) {
	buffer := NewBoundedBuffer(4)
	for _, payload := range [][]byte{[]byte("ab"), []byte("cde"), []byte("f")} {
		written, err := buffer.Write(payload)
		if err != nil {
			t.Fatalf("Write(%q): %v", payload, err)
		}
		if written != len(payload) {
			t.Fatalf("Write(%q) = %d, want %d", payload, written, len(payload))
		}
	}
	if got := buffer.String(); got != "abcd" {
		t.Fatalf("String = %q, want bounded prefix", got)
	}
	if !buffer.Truncated() {
		t.Fatal("Truncated = false after omitted input")
	}
}

func TestBoundedBufferZeroLimitMarksOnlyNonEmptyInputTruncated(t *testing.T) {
	buffer := NewBoundedBuffer(0)
	if _, err := buffer.Write(nil); err != nil {
		t.Fatalf("Write(nil): %v", err)
	}
	if buffer.Truncated() {
		t.Fatal("empty input marked truncated")
	}
	if _, err := buffer.Write([]byte("x")); err != nil {
		t.Fatalf("Write(non-empty): %v", err)
	}
	if !buffer.Truncated() || buffer.String() != "" {
		t.Fatalf("zero-limit buffer = %q truncated=%t", buffer.String(), buffer.Truncated())
	}
}
