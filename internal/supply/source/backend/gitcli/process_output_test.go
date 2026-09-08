package gitcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

func TestGitOutputDrainInterruptsActiveRead(t *testing.T) {
	for _, cause := range []error{nil, context.Canceled, context.DeadlineExceeded} {
		t.Run(fmt.Sprint(cause), func(t *testing.T) {
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
				var pathErr *os.PathError
				if !errors.Is(err, os.ErrClosed) || !errors.As(err, &pathErr) || pathErr.Op != "read" {
					t.Fatalf("drain error = %v, want native closed-pipe read cause", err)
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("transport error invented a caller context cause: %v", err)
				}
				if gitFrozenNonContextConsumer(err) {
					t.Fatalf("drain error was mistaken for a consumer rejection: %v", err)
				}
				wrapped := fmt.Errorf("consume output: %w", err)
				result := gitProcessResult{commandErr: cause}
				if got := gitObservedLifecycleError(wrapped, result); got != cause {
					t.Fatalf("drain lifecycle error = %v, want observed cause %v", got, cause)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("inherited writer retained an active Read")
			}
			if !reader.Incomplete() {
				t.Fatal("expired pipe output was marked complete")
			}
		})
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

func TestGitOutputDrainUsesObservedWaitWithoutHidingConsumerFailures(t *testing.T) {
	reader, _ := gitOutputTestPipe(t)
	reader.BeginDrain()
	reader.remaining = 0
	count, drainErr := reader.Read(make([]byte, 1))
	if count != 0 || !errors.Is(drainErr, os.ErrClosed) || !reader.Incomplete() {
		t.Fatalf("spent-budget read = %d, %v, incomplete=%t", count, drainErr, reader.Incomplete())
	}
	consumerClosed := &os.PathError{Op: "write", Path: "consumer-output", Err: os.ErrClosed}
	for _, testCase := range []struct {
		name            string
		err             error
		consumerFailure bool
	}{
		{name: "direct-drain", err: drainErr},
		{name: "wrapped-drain", err: fmt.Errorf("consume output: %w", drainErr)},
		{name: "joined-drains", err: errors.Join(drainErr, fmt.Errorf("consume output: %w", drainErr))},
		{name: "validation-with-drain", err: errors.Join(drainErr, errors.New("consumer rejection")), consumerFailure: true},
		{name: "closed-file-with-drain", err: fmt.Errorf("consume output: %w", errors.Join(drainErr, consumerClosed)), consumerFailure: true},
		{name: "consumer-closed-file", err: consumerClosed, consumerFailure: true},
		{name: "consumer-truncated-input", err: io.ErrUnexpectedEOF, consumerFailure: true},
	} {
		for _, cause := range []error{nil, context.Canceled, context.DeadlineExceeded} {
			t.Run(testCase.name+"/"+fmt.Sprint(cause), func(t *testing.T) {
				if got := gitFrozenNonContextConsumer(testCase.err); got != testCase.consumerFailure {
					t.Fatalf("frozen consumer = %t, want %t", got, testCase.consumerFailure)
				}
				want := cause
				if testCase.consumerFailure {
					want = nil
				}
				result := gitProcessResult{commandErr: cause}
				if got := gitObservedLifecycleError(testCase.err, result); got != want {
					t.Fatalf("lifecycle error = %v, want %v", got, want)
				}
			})
		}
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
