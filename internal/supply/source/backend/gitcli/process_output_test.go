package gitcli

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

func TestGitOutputDrainInterruptsActiveRead(t *testing.T) {
	reader, _ := gitOutputTestPipe(t)
	result := make(chan error, 1)
	go func() {
		_, err := reader.Read(make([]byte, 1))
		result <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		reader.mu.Lock()
		active := !reader.readStarted.IsZero()
		reader.mu.Unlock()
		if active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reader did not enter Read")
		}
		runtime.Gosched()
	}
	reader.BeginDrain()
	select {
	case err := <-result:
		if !errors.Is(err, os.ErrClosed) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("drain error = %v, want closed pipe rather than caller deadline", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("inherited writer retained an active Read")
	}
	if !reader.Incomplete() {
		t.Fatal("expired pipe output was marked complete")
	}
}

func TestGitOutputDrainPreservesBufferedDataBetweenReads(t *testing.T) {
	reader, writer := gitOutputTestPipe(t)
	if _, err := writer.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader.BeginDrain()
	buffer := make([]byte, 1)
	if _, err := io.ReadFull(reader, buffer); err != nil || string(buffer) != "a" {
		t.Fatalf("first read = %q, %v; want a", buffer, err)
	}
	remaining := reader.remaining
	if remaining <= 0 || remaining >= subprocess.InheritedOutputCloseWait {
		t.Fatalf("remaining budget = %v, want positive spent budget", remaining)
	}

	time.Sleep(2 * subprocess.InheritedOutputCloseWait)
	reader.BeginDrain()
	if reader.remaining != remaining {
		t.Fatal("consumer work or repeated drain changed the remaining budget")
	}
	if _, err := io.ReadFull(reader, buffer); err != nil || string(buffer) != "b" {
		t.Fatalf("second read = %q, %v; want b", buffer, err)
	}
	if reader.remaining >= remaining {
		t.Fatal("successful progress did not spend cumulative read budget")
	}
	if _, err := reader.Read(buffer); err != io.EOF || reader.Incomplete() {
		t.Fatalf("final read = %v, incomplete=%t; want complete EOF", err, reader.Incomplete())
	}
}

func TestGitOutputDrainDoesNotRenewExhaustedBudget(t *testing.T) {
	reader, writer := gitOutputTestPipe(t)
	if _, err := writer.Write([]byte("buffered")); err != nil {
		t.Fatal(err)
	}
	reader.BeginDrain()
	// Exhaustion can follow successful reads without an earlier timeout error.
	reader.remaining = 0
	reader.BeginDrain()
	count, err := reader.Read(make([]byte, 8))
	if count != 0 || !errors.Is(err, os.ErrClosed) || !reader.Incomplete() {
		t.Fatalf("exhausted read = %d, %v, incomplete=%t", count, err, reader.Incomplete())
	}
}

func gitOutputTestPipe(t *testing.T) (*gitOutputReader, *os.File) {
	t.Helper()
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	t.Cleanup(func() { _ = output.Close() })
	reader, err := newGitOutputReader(input)
	if err != nil {
		t.Fatal(err)
	}
	return reader, output
}
