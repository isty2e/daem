package nativefixture

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

func ExtensionCompletionState(t *testing.T, paths daempaths.Paths) durable.Snapshot {
	t.Helper()
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	state, err := statefile.LoadOptionalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{
		Codec: codec, PhysicalPathBudget: physicalwork.NewCaptureBudget(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}
