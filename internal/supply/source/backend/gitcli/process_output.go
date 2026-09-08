package gitcli

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

// gitOutputReader spends the inherited-output budget only inside Read.
// Consumer parsing and filesystem work must not expire buffered pipe data.
// The budget is cumulative: progress cannot renew an escaped writer's grace.
type gitOutputReader struct {
	file *os.File

	mu          sync.Mutex
	draining    bool
	remaining   time.Duration
	readStarted time.Time
	incomplete  bool
}

func newGitOutputReader(file *os.File) (*gitOutputReader, error) {
	if err := file.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return &gitOutputReader{file: file, remaining: subprocess.InheritedOutputCloseWait}, nil
}

func (reader *gitOutputReader) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	reader.readStarted = time.Now()
	if reader.draining {
		reader.armDeadline()
	}
	reader.mu.Unlock()

	count, err := reader.file.Read(buffer)

	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.draining {
		reader.remaining = max(0, reader.remaining-time.Since(reader.readStarted))
	}
	reader.readStarted = time.Time{}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		reader.closeIncomplete()
		// Pipe grace exhaustion is not a caller context deadline.
		err = &os.PathError{Op: "read", Path: reader.file.Name(), Err: os.ErrClosed}
	}
	if reader.incomplete && errors.Is(err, os.ErrClosed) {
		err = &gitOutputDrainError{cause: err}
	}
	return count, err
}

func (reader *gitOutputReader) BeginDrain() {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.draining {
		return
	}
	reader.draining = true
	if !reader.readStarted.IsZero() {
		reader.readStarted = time.Now()
		reader.armDeadline()
	}
}

// armDeadline and closeIncomplete require mu; neither waits for consumer work.
func (reader *gitOutputReader) armDeadline() {
	if reader.remaining <= 0 {
		reader.closeIncomplete()
		return
	}
	if err := reader.file.SetReadDeadline(reader.readStarted.Add(reader.remaining)); err != nil {
		reader.closeIncomplete()
	}
}

func (reader *gitOutputReader) closeIncomplete() {
	reader.incomplete = true
	_ = reader.file.Close()
}

func (reader *gitOutputReader) Incomplete() bool {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.incomplete
}

func (reader *gitOutputReader) Close() error {
	return reader.file.Close()
}

// A drain cutoff is a transport failure, not an independent consumer rejection.
type gitOutputDrainError struct {
	cause error
}

func (err *gitOutputDrainError) Error() string {
	return err.cause.Error()
}

func (err *gitOutputDrainError) Unwrap() error {
	return err.cause
}

func gitOutputDrainOnly(err error) bool {
	switch wrapped := err.(type) {
	case *gitOutputDrainError:
		return true
	case interface{ Unwrap() error }:
		return gitOutputDrainOnly(wrapped.Unwrap())
	case interface{ Unwrap() []error }:
		causes := wrapped.Unwrap()
		if len(causes) == 0 {
			return false
		}
		// Joined cleanup or validation failures still require consumer precedence.
		for _, cause := range causes {
			if !gitOutputDrainOnly(cause) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
