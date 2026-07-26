//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestTerminalConfirmationReadStopsOnContextDeadline(t *testing.T) {
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := readTerminalConfirmationLine(ctx, terminal, 4096)
		result <- err
	}()

	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want context deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked terminal read did not stop at context deadline")
	}
}

func TestTerminalConfirmationReadRejectsClosedDescriptor(t *testing.T) {
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	if err := terminal.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = readTerminalConfirmationLine(context.Background(), terminal, 4096)
	if err == nil || !strings.Contains(err.Error(), "file descriptor") {
		t.Fatalf("error = %v", err)
	}
}

func TestTerminalConfirmationReadTreatsBufferedCanonicalEOFAsEOF(t *testing.T) {
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()

	type result struct {
		answer string
		err    error
	}
	resultChannel := make(chan result, 1)
	go func() {
		answer, readErr := readTerminalConfirmationLine(context.Background(), terminal, 4096)
		resultChannel <- result{answer: answer, err: readErr}
	}()

	if _, err := master.Write([]byte{'y', 'e', 's', 4}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-resultChannel:
		if got.answer != "yes" || !errors.Is(got.err, io.EOF) {
			t.Fatalf("read = (%q, %v), want (yes, EOF)", got.answer, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("buffered canonical EOF did not complete the terminal read")
	}
}
