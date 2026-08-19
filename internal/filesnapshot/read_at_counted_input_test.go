package filesnapshot_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/filesnapshot"
)

func TestReadRegularFileAtCountedRejectsInvalidInputWithoutUnsupported(t *testing.T) {
	t.Parallel()

	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	cases := []struct {
		name string
		ctx  context.Context
		dir  *os.File
		ent  string
		max  int64
	}{
		{name: "nil context", dir: dir, ent: "plugin.json", max: 64},
		{name: "canceled context", ctx: canceled, dir: dir, ent: "plugin.json", max: 64},
		{name: "nil directory", ctx: t.Context(), ent: "plugin.json", max: 64},
		{name: "empty name", ctx: t.Context(), dir: dir, max: 64},
		{name: "nested name", ctx: t.Context(), dir: dir, ent: "nested/name", max: 64},
		{name: "non-positive bound", ctx: t.Context(), dir: dir, ent: "plugin.json"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			counted, err := filesnapshot.ReadRegularFileAtCounted(testCase.ctx, testCase.dir, testCase.ent, testCase.max)
			if err == nil || errors.Is(err, filesnapshot.ErrUnsupported) {
				t.Fatalf("ReadRegularFileAtCounted(%s) = %+v, %v, want invalid input", testCase.name, counted, err)
			}
			if counted.Exists || counted.Attempted != 0 || len(counted.Content) != 0 {
				t.Fatalf("invalid input observation = %+v, want zero CountedContent", counted)
			}
		})
	}
}
