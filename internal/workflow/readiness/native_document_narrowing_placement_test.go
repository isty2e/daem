package readiness

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativeDocumentNarrowingRefusesSameOwnerWrongPlacement(t *testing.T) {
	for _, test := range []struct{ name, desired string }{
		{name: "retained update", desired: `{"model":"fast"}`},
		{name: "already desired", desired: `{"model":"balanced"}`},
		{name: "empty release", desired: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := narrowingInput(t, configdocument.JSON, `{"model":"balanced","local":9}`, `{"model":"balanced","local":9}`, test.desired)
			original := input.Baseline
			owner, ok := topologyprojection.EntityID(original.Subject())
			if !ok {
				t.Fatal("fixture baseline is not entity-backed")
			}
			placement, err := aggregate.HostConfigPlacementID(target.TargetCodex, target.ScopeGlobal)
			if err != nil {
				t.Fatal(err)
			}
			subject, err := topologyprojection.Subject(owner, placement)
			if err != nil {
				t.Fatal(err)
			}
			input.Baseline, err = durable.NewManagedPathState(subject, original.ConsumerTargets(), original.Scope(), original.Destination(),
				original.ContentHash(), original.ContentKind(), original.PermissionPolicy(), original.FileMode())
			if err != nil {
				t.Fatal(err)
			}
			result, err := BuildNativeDocumentNarrowing(t.Context(), input)
			if err == nil || result.Validate() == nil {
				t.Fatal("same owner and hash bypassed full placement identity")
			}
		})
	}
}
