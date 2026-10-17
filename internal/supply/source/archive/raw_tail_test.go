package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"
)

func TestExtractTarConsumesTrailingInput(t *testing.T) {
	content := tarContent(t, tar.Header{Name: "payload.txt", Mode: 0o600}, "payload")
	for _, tail := range [][]byte{nil, make([]byte, 128), []byte("nonzero trailing data")} {
		input := bytes.NewReader(append(bytes.Clone(content), tail...))
		limits := testBudget()
		limits.inputBytes = int64(input.Len())
		root := t.TempDir()
		if err := extractTarWithBudget(t.Context(), input, root, limits); err != nil {
			t.Fatalf("extract with %d trailing bytes: %v", len(tail), err)
		}
		if input.Len() != 0 {
			t.Fatalf("unread input = %d bytes, want EOF", input.Len())
		}
		payload, err := os.ReadFile(filepath.Join(root, "payload.txt"))
		if err != nil || string(payload) != "payload" {
			t.Fatalf("payload = %q, error = %v", payload, err)
		}
	}
}

func TestExtractTarTailUsesCumulativeInputBudget(t *testing.T) {
	content := tarContent(t, tar.Header{Name: "payload.txt", Mode: 0o600}, "payload")
	tail := bytes.Repeat([]byte("x"), 128)
	limits := testBudget()
	limits.inputBytes = int64(len(content) + len(tail) - 1)

	err := extractTarWithBudget(t.Context(), io.MultiReader(bytes.NewReader(content), bytes.NewReader(tail)), t.TempDir(), limits)
	limitErr := requireLimitKind(t, err, LimitInputBytes)
	if !errors.Is(err, ErrLimitExceeded) || limitErr.Limit() != limits.inputBytes {
		t.Fatalf("tail overflow = %v, want shared input limit %d", err, limits.inputBytes)
	}
}

func TestExtractTarTailPreservesReadErrors(t *testing.T) {
	content := tarContent(t, tar.Header{Name: "payload.txt", Mode: 0o600}, "payload")
	readErr := errors.New("tail read failed")
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "io", err: readErr},
		{name: "typed_io", err: &os.PathError{Op: "read", Path: "tail", Err: readErr}},
		{name: "canceled", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input := io.MultiReader(bytes.NewReader(content), iotest.ErrReader(testCase.err))
			err := ExtractTar(t.Context(), input, t.TempDir())
			if !errors.Is(err, testCase.err) {
				t.Fatalf("tail error = %v, want identity %v", err, testCase.err)
			}
			if testCase.name == "typed_io" {
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) || pathErr.Err != readErr {
					t.Fatalf("tail error lost typed cause: %v", err)
				}
			}
		})
	}
}

func TestExtractTarTailObservesCancellation(t *testing.T) {
	content := tarContent(t, tar.Header{Name: "payload.txt", Mode: 0o600}, "payload")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := io.MultiReader(bytes.NewReader(content), cancelingTarTail{cancel: cancel})

	if err := ExtractTar(ctx, input, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("tail cancellation = %v, want context.Canceled", err)
	}
}

type cancelingTarTail struct {
	cancel context.CancelFunc
}

func (tail cancelingTarTail) Read(buffer []byte) (int, error) {
	tail.cancel()
	if len(buffer) == 0 {
		return 0, nil
	}
	buffer[0] = 'x'
	return 1, nil
}
