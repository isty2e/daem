package authoring

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/declarationartifact"
)

func captureAuthoringTarget(t testing.TB, path string) declarationartifact.MutationTarget {
	t.Helper()
	target, err := declarationartifact.CaptureMutationTarget(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
