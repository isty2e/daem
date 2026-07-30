package readiness

import (
	"os"
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestObserveExtensionOrdersPlansSelectedPiSequence(t *testing.T) {
	root := t.TempDir()
	writeReadinessFile(
		t,
		filepath.Join(root, ".pi", "settings.json"),
		[]byte(`{"packages":["npm:@acme/b","npm:foreign","npm:@acme/a"]}`),
	)
	locked := readinessPiOrderLock(t, root)
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetPi})
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := observeExtensionOrders(
		daempaths.Paths{ManifestRoot: root},
		locked,
		selected,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("observeExtensionOrders: %v", err)
	}
	if len(decisions) != 1 ||
		decisions[0].Kind() != reconcile.OrderNormalize ||
		decisions[0].SequenceID() != "pi:project:settings.packages" ||
		decisions[0].ForeignRowCount() != 1 ||
		len(decisions[0].PrecedenceChanges()) != 2 {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestObserveExtensionOrdersTurnsUnreadableSequenceIntoTypedBlock(t *testing.T) {
	root := t.TempDir()
	writeReadinessFile(t, filepath.Join(root, ".pi", "settings.json"), []byte(`{`))
	locked := readinessPiOrderLock(t, root)
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetPi})
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := observeExtensionOrders(
		daempaths.Paths{ManifestRoot: root},
		locked,
		selected,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("observeExtensionOrders: %v", err)
	}
	if len(decisions) != 1 ||
		decisions[0].Kind() != reconcile.OrderBlocked ||
		decisions[0].Reason() != reconcile.OrderReasonObservationUnavailable ||
		decisions[0].HasCurrentSequence() ||
		decisions[0].Detail() == "" {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestObserveExtensionOrdersPlansOpenCodeDocumentsIndependently(t *testing.T) {
	root := t.TempDir()
	writeReadinessFile(
		t,
		filepath.Join(root, ".opencode", "opencode.json"),
		[]byte(`{"plugin":["beta@2","foreign-server@1","alpha@1"]}`),
	)
	writeReadinessFile(
		t,
		filepath.Join(root, ".opencode", "tui.json"),
		[]byte(`{"plugin":["alpha@1","foreign-tui@1","beta@2"]}`),
	)
	locked := readinessOpenCodeOrderLock(t, root)
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetOpenCode})
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := observeExtensionOrders(
		daempaths.Paths{ManifestRoot: root},
		locked,
		selected,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("observeExtensionOrders: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("decisions = %#v, want two physical sequences", decisions)
	}
	kinds := make(map[string]reconcile.RelationOrderDecisionKind, len(decisions))
	for _, decision := range decisions {
		kinds[string(decision.SequenceID())] = decision.Kind()
	}
	if kinds["opencode:project:server.json.plugins"] != reconcile.OrderNormalize ||
		kinds["opencode:project:tui.json.plugins"] != reconcile.OrderExact {
		t.Fatalf("sequence kinds = %#v", kinds)
	}
}

func TestObserveExtensionOrdersProjectsOpenCodeOrderOntoTUIOnlyMembership(t *testing.T) {
	root := t.TempDir()
	writeReadinessFile(
		t,
		filepath.Join(root, ".opencode", "tui.json"),
		[]byte(`{"plugin":["beta@2","foreign@1","alpha@1"]}`),
	)
	locked := readinessOpenCodeOrderLock(t, root)
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetOpenCode})
	if err != nil {
		t.Fatal(err)
	}

	decisions, err := observeExtensionOrders(
		daempaths.Paths{ManifestRoot: root},
		locked,
		selected,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("observeExtensionOrders: %v", err)
	}
	if len(decisions) != 1 ||
		decisions[0].SequenceID() != "opencode:project:tui.json.plugins" ||
		decisions[0].Kind() != reconcile.OrderNormalize ||
		decisions[0].Reason() != reconcile.OrderReasonNone ||
		len(decisions[0].DesiredMembers()) != 2 {
		t.Fatalf("decisions = %#v, want one TUI normalization", decisions)
	}
}

func TestObserveExtensionOrdersSkipsUnselectedOrderClass(t *testing.T) {
	root := t.TempDir()
	locked := readinessPiOrderLock(t, root)
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetOpenCode})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := observeExtensionOrders(
		daempaths.Paths{ManifestRoot: root},
		locked,
		selected,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("observeExtensionOrders: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("unselected order produced decisions")
	}
}

func readinessOpenCodeOrderLock(t testing.TB, root string) lock.File {
	t.Helper()
	return readinessOrderLock(
		t,
		root,
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		[]string{"alpha@1", "beta@2"},
	)
}

func readinessPiOrderLock(t testing.TB, root string) lock.File {
	t.Helper()
	return readinessOrderLock(
		t,
		root,
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		[]string{"npm:@acme/a", "npm:@acme/b"},
	)
}

func readinessOrderLock(
	t testing.TB,
	root string,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	sourceValues []string,
) lock.File {
	t.Helper()
	sources := make([]desiredextension.SourceRef, 0, len(sourceValues))
	for _, value := range sourceValues {
		source, err := desiredextension.NewSourceRef(
			desiredextension.SourceKindHostSource,
			value,
		)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source)
	}
	extensions := make([]desiredextension.Extension, 0, 2)
	for index, name := range []string{"a", "b"} {
		spec := desiredextension.Spec{
			Name:    name,
			Carrier: carrier,
			Target:  selectedTarget,
			Scope:   target.ScopeProject,
			Source:  sources[index],
		}
		extension, err := desiredextension.New(spec)
		if err != nil {
			t.Fatal(err)
		}
		extensions = append(extensions, extension)
	}
	subjects, err := lockrefine.Extensions(extensions)
	if err != nil {
		t.Fatal(err)
	}
	constraints, err := lockrefine.ExtensionOrderConstraints(
		extensions,
		aggregatecodec.ExtensionOrderIdentityResolver(
			daempaths.Paths{ManifestRoot: root},
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	section, err := lock.NewLockedSection(subjects, constraints)
	if err != nil {
		t.Fatal(err)
	}
	return lock.File{Version: lock.CurrentVersion, Locked: section}
}

func writeReadinessFile(t testing.TB, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
