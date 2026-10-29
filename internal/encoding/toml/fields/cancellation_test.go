package fields

import (
	"context"
	"errors"
	"testing"
)

type publicationContext struct {
	context.Context
	calls, cancelAfter int
}

func (ctx *publicationContext) Err() error {
	ctx.calls++
	if ctx.calls > ctx.cancelAfter {
		return context.Canceled
	}
	return nil
}

func TestFinalFieldValidationCancellationDoesNotPublishMetadata(t *testing.T) {
	input := []byte("value=true\n")
	var target struct {
		Value bool `toml:"value"`
	}
	baseline := &publicationContext{Context: t.Context(), cancelAfter: int(^uint(0) >> 1)}
	if _, err := DecodeAdmitted(baseline, input, &target); err != nil {
		t.Fatal(err)
	}
	ctx := &publicationContext{Context: t.Context(), cancelAfter: baseline.calls - 1}
	metadata, err := DecodeAdmitted(ctx, input, &target)
	if !errors.Is(err, context.Canceled) || len(metadata.Keys()) != 0 {
		t.Fatal("field-validation publication ignored cancellation", metadata, err)
	}
}
