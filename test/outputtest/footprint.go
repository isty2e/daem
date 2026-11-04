package outputtest

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func KeyFootprint(t testing.TB, segments ...string) output.Footprint {
	t.Helper()
	path, err := output.NewKeyPath(segments...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(path)
	if err != nil {
		t.Fatal(err)
	}
	return footprint
}
