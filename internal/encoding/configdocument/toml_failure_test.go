package configdocument

import (
	"context"
	"errors"
	"testing"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

type parseFailureContext struct {
	context.Context
	calls, cancelAfter int
}

func (ctx *parseFailureContext) Err() error {
	ctx.calls++
	if ctx.calls > ctx.cancelAfter {
		return context.Canceled
	}
	return nil
}

func TestTOMLParseFailureIsNotReplacedByLaterCancellation(t *testing.T) {
	input := []byte("duplicate=1\nduplicate=2\n")
	limits := Limits{MaximumBytes: 1024, MaximumDepth: 4}
	structure, err := tomlsyntax.DocumentLimits(int64(len(input)), limits.MaximumDepth)
	if err != nil {
		t.Fatal(err)
	}
	baseline := &parseFailureContext{Context: t.Context(), cancelAfter: int(^uint(0) >> 1)}
	if err := tomlsyntax.Admit(baseline, input, structure); err != nil {
		t.Fatal(err)
	}
	if _, err := tomlsyntax.Native(baseline, input); err == nil {
		t.Fatal("duplicate keys must fail before the late cancellation point")
	}
	ctx := &parseFailureContext{Context: t.Context(), cancelAfter: baseline.calls}
	_, _, err = parseTOML(ctx, input, limits)
	if !errors.Is(err, ErrInvalidDocument) || errors.Is(err, context.Canceled) {
		t.Fatal("later cancellation replaced the parser failure", err)
	}
}
