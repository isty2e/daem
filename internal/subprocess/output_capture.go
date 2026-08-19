package subprocess

import (
	"io"
	"os"
	"sync"
	"time"
)

// OutputSnapshot is one diagnostic stream after its copy goroutine has stopped.
// Overflow and incomplete closure are distinct causes of the truncated
// projection; callers must not treat process-exit errors as completeness.
type OutputSnapshot struct {
	Text       string
	Overflow   bool
	Incomplete bool
}

// Truncated reports overflow or forced/uncertain closure.
func (snapshot OutputSnapshot) Truncated() bool {
	return snapshot.Overflow || snapshot.Incomplete
}

// OutputCapture is a caller-owned child output pipe copied into a bounded
// buffer. Completeness is observed from that copy, not from command.Wait.
type OutputCapture struct {
	reader *os.File
	writer *os.File
	buffer *BoundedBuffer
	done   chan error

	startOnce  sync.Once
	finishOnce sync.Once
	snapshot   OutputSnapshot
}

// NewOutputCapture constructs a pipe and bounded buffer for one child stream.
func NewOutputCapture(limit int) (*OutputCapture, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &OutputCapture{
		reader: reader,
		writer: writer,
		buffer: NewBoundedBuffer(limit),
		done:   make(chan error, 1),
	}, nil
}

// Writer is the child end. Assign it to cmd.Stdout or cmd.Stderr before Start.
func (capture *OutputCapture) Writer() *os.File {
	if capture == nil {
		return nil
	}
	return capture.writer
}

// CloseWriter drops the parent's duplicate write end after Start so only the
// child (and inherited descendants) keep the stream open.
func (capture *OutputCapture) CloseWriter() {
	if capture == nil || capture.writer == nil {
		return
	}
	_ = capture.writer.Close()
	capture.writer = nil
}

// Close closes both pipe ends. Call it on setup failure before StartCopy.
func (capture *OutputCapture) Close() {
	if capture == nil {
		return
	}
	capture.CloseWriter()
	if capture.reader != nil {
		_ = capture.reader.Close()
	}
}

// StartCopy begins the parent-side bounded copy. CloseWriter must already have
// run so the parent cannot keep the write end open.
func (capture *OutputCapture) StartCopy() {
	if capture == nil {
		return
	}
	capture.startOnce.Do(func() {
		go func() {
			_, err := io.Copy(capture.buffer, capture.reader)
			capture.done <- err
		}()
	})
}

// Finish waits for natural copy EOF or, after bound, closes the reader to
// force the copy to stop. The snapshot is taken only after the copier returns.
func (capture *OutputCapture) Finish(bound time.Duration) OutputSnapshot {
	if capture == nil {
		return OutputSnapshot{Incomplete: true}
	}
	capture.finishOnce.Do(func() {
		if bound <= 0 {
			bound = InheritedOutputCloseWait
		}
		capture.StartCopy()
		timer := time.NewTimer(bound)
		defer timer.Stop()
		var copyErr error
		forced := false
		select {
		case copyErr = <-capture.done:
			_ = capture.reader.Close()
		case <-timer.C:
			forced = true
			_ = capture.reader.Close()
			copyErr = <-capture.done
		}
		text, overflow := capture.buffer.snapshot()
		capture.snapshot = OutputSnapshot{
			Text:       text,
			Overflow:   overflow,
			Incomplete: forced || copyErr != nil,
		}
	})
	return capture.snapshot
}
