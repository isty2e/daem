package aggregate_test

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestHostConfigDocumentSubjectBindsCanonicalPlacement(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			subject, err := aggregate.HostConfigDocumentSubject(id, selected, scope)
			if err != nil {
				t.Fatal(err)
			}
			placement, err := aggregate.HostConfigPlacementID(selected, scope)
			if err != nil {
				t.Fatal(err)
			}
			want, err := topologyprojection.Subject(id, placement)
			if err != nil || subject != want {
				t.Fatalf("whole-document subject lost canonical placement: %v", err)
			}
		}
	}

	other, err := entity.New(entity.KindHookAsset, "asset")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id       entity.ID
		selected target.Target
		scope    target.Scope
	}{
		{id: other, selected: target.TargetPi, scope: target.ScopeGlobal},
		{selected: target.TargetPi, scope: target.ScopeGlobal},
		{id: id, selected: "unsupported", scope: target.ScopeGlobal},
		{id: id, selected: target.TargetPi, scope: "unsupported"},
	} {
		if subject, err := aggregate.HostConfigDocumentSubject(test.id, test.selected, test.scope); err == nil || subject.Validate() == nil {
			t.Fatal("invalid entity/placement produced a usable document subject")
		}
	}
}
