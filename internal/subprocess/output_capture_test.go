package subprocess

import (
	"io"
	"testing"
	"time"
)

func TestOutputCaptureTreatsNaturalEOFAsComplete(t *testing.T) {
	capture, err := NewOutputCapture(32)
	if err != nil {
		t.Fatalf("NewOutputCapture: %v", err)
	}
	t.Cleanup(capture.Close)

	writer := capture.Writer()
	capture.StartCopy()
	if _, err := io.WriteString(writer, "complete-stderr\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close child writer: %v", err)
	}

	snapshot := capture.Finish(time.Second)
	if snapshot.Incomplete || snapshot.Overflow || snapshot.Truncated() {
		t.Fatalf("snapshot = %#v, want complete", snapshot)
	}
	if snapshot.Text != "complete-stderr\n" {
		t.Fatalf("text = %q", snapshot.Text)
	}
}

func TestOutputCaptureDistinguishesOverflowFromForcedClosure(t *testing.T) {
	capture, err := NewOutputCapture(4)
	if err != nil {
		t.Fatalf("NewOutputCapture: %v", err)
	}
	t.Cleanup(capture.Close)

	writer := capture.Writer()
	capture.StartCopy()
	if _, err := io.WriteString(writer, "12345"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close child writer: %v", err)
	}

	snapshot := capture.Finish(time.Second)
	if !snapshot.Overflow || snapshot.Incomplete || snapshot.Text != "1234" {
		t.Fatalf("snapshot = %#v, want overflow with natural EOF", snapshot)
	}
}

func TestOutputCaptureMarksForcedCloseIncomplete(t *testing.T) {
	capture, err := NewOutputCapture(32)
	if err != nil {
		t.Fatalf("NewOutputCapture: %v", err)
	}
	t.Cleanup(func() {
		if capture.Writer() != nil {
			_ = capture.Writer().Close()
		}
		capture.Close()
	})

	writer := capture.Writer()
	capture.StartCopy()
	if _, err := io.WriteString(writer, "super-"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	started := time.Now()
	snapshot := capture.Finish(50 * time.Millisecond)
	elapsed := time.Since(started)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Finish took %s, want bounded forced close", elapsed)
	}
	if !snapshot.Incomplete || snapshot.Overflow || snapshot.Text != "super-" {
		t.Fatalf("snapshot = %#v, want forced incomplete prefix", snapshot)
	}
}
