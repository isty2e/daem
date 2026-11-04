package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func nativeWholeFileJournalBaseline(t *testing.T, selectedTarget target.Target, scope target.Scope) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "native")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, selectedTarget, scope)
	if err != nil {
		t.Fatal(err)
	}
	root := "." + string(selectedTarget)
	if scope == target.ScopeGlobal {
		root = "@codex-home"
		if selectedTarget == target.TargetPi {
			root = "@pi-agent"
		}
	}
	baseline, err := durable.NewManagedPathState(subject, []target.Target{selectedTarget}, scope,
		outputtest.Parse(t, root+"/extension-settings/native.json"), testContentHash("native"),
		realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}
