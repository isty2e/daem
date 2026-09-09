package tomlstrict

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMalformedErrorRetainsCauseAndDetectionPosition(t *testing.T) {
	for _, test := range []struct {
		name, content string
		line, column  int
	}{
		{"EOF", "version = 1\ntargets = [\n", 3, 1},
		{"Unicode", "\"é\" = [", 1, 8},
		{"CRLF", "x=1\r\ny=[", 2, 4},
		{"BOM", "\xef\xbb\xbfx=[", 1, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := Admit(context.Background(), []byte(test.content), StandardLimits())
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("error=%v", err)
			}
			if err.Error() != "malformed TOML structure: unclosed array" {
				t.Fatalf("raw error changed: %v", err)
			}
			line, column, ok := ErrorPosition(fmt.Errorf("outer: %w", err))
			if !ok || line != test.line || column != test.column {
				t.Fatalf("position=(%d,%d,%v), want (%d,%d,true)", line, column, ok, test.line, test.column)
			}
		})
	}
}

func TestPositionDoesNotRelabelBudgetCancellationOrDecodeErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	limits := StandardLimits()
	limits.MaximumDepth = 1
	for _, test := range []struct {
		ctx     context.Context
		content string
		limits  Limits
		cause   error
	}{
		{ctx, "x=[", StandardLimits(), context.Canceled},
		{context.Background(), "x=" + strings.Repeat("[", 4), limits, ErrMaximumDepthExceeded},
	} {
		err := Admit(test.ctx, []byte(test.content), test.limits)
		if !errors.Is(err, test.cause) {
			t.Fatalf("err=%v, want %v", err, test.cause)
		}
		if _, _, ok := ErrorPosition(err); ok {
			t.Fatalf("non-syntax failure gained position: %v", err)
		}
	}
	content := []byte("x = nope\n")
	if err := Admit(context.Background(), content, StandardLimits()); err != nil {
		t.Fatalf("decoder fixture failed structure admission: %v", err)
	}
	var values map[string]string
	_, decodeErr := DecodeAdmitted(context.Background(), content, &values)
	if decodeErr == nil {
		t.Fatal("invalid value was decoded")
	}
	if _, _, ok := ErrorPosition(decodeErr); ok {
		t.Fatal("decoder error mislabeled as scanner position")
	}
	if _, _, ok := ErrorPosition(errors.New("malformed TOML structure")); ok {
		t.Fatal("classified unowned text")
	}
	if _, _, ok := ErrorPosition(nil); ok {
		t.Fatal("nil has a position")
	}
}
