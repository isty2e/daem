package readiness

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func readinessOrderPaths(t testing.TB, root string) daempaths.Paths {
	t.Helper()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

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
		readinessOrderPaths(t, root),
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

func TestSelectedOrderDecisionRetainsPhysicalSubsetAndClasslessCardinality(t *testing.T) {
	root := t.TempDir()
	writeReadinessFile(t, filepath.Join(root, ".opencode", "opencode.json"), []byte(`{"plugin":["beta@2","foreign@1","alpha@1"]}`))
	writeReadinessFile(t, filepath.Join(root, ".opencode", "tui.json"), []byte(`{"plugin":["only-foreign@1"]}`))
	locked := readinessOpenCodeOrderLock(t, root)
	observation, err := relationhost.ObserveOrder(relationhost.OrderInput{
		Paths: readinessOrderPaths(t, root), Lockfile: locked, Constraint: locked.Locked.OrderConstraints()[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	edits, err := observation.SelectedEdits(nil)
	if err != nil || len(edits) != 2 {
		t.Fatal("fixture did not observe independent documents", err)
	}
	count := 0
	for _, edit := range edits {
		decision, admitted, err := DecideSelectedExtensionOrder(edit, nil)
		if err != nil {
			t.Fatal(err)
		}
		if admitted {
			count++
			if decision.Kind() != reconcile.OrderNormalize || len(decision.PrecedenceChanges()) != 2 || decision.Validate() != nil {
				t.Fatal("selected physical membership or risk changed", decision)
			}
		} else if filepath.Base(edit.SelectedPath()) != "tui.json" {
			t.Fatal("a populated desired sequence lost its order decision")
		}
	}
	if count != 1 {
		t.Fatal("an empty loaded subset fabricated an order decision", count)
	}
	inventory, err := opencodeplugin.ReadInventory(opencodeplugin.InventoryInput{ManifestRoot: root, Scope: target.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetOpenCode, target.ScopeProject, desiredextension.CarrierOpenCodePlugin)
	if !admitted {
		t.Fatal("fixture has no order capability")
	}
	for _, document := range inventory.Documents() {
		view, err := opencodeplugin.ReconstructRetirement(capability, document, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		edit, err := relationhost.NewOpenCodeDocumentEdit(view)
		if err != nil {
			t.Fatal(err)
		}
		if _, admitted, err := DecideSelectedExtensionOrder(edit, nil); err != nil || admitted {
			t.Fatal("retirement-only sequence fabricated an order result", err)
		}
	}
}

func TestClasslessRetirementAdmissionUsesLockedClassAndMatchingClaims(t *testing.T) {
	root := t.TempDir()
	paths := readinessOrderPaths(t, root)
	locked := readinessOpenCodeOrderLock(t, root)
	claim := readinessOrderCarrierClaim(t, locked, "beta@2")
	piClaim := readinessOrderCarrierClaim(t, readinessPiOrderLock(t, root), "npm:@acme/a")
	config := filepath.Join(root, ".opencode", "opencode.json")
	writeReadinessFile(t, config, []byte(`{`))
	for _, test := range []struct {
		name   string
		locked lock.File
		claims []durablecarrier.ManagedCarrierClaim
	}{
		{"locked_class_is_not_classless_fallback", locked, []durablecarrier.ManagedCarrierClaim{claim}},
		{"no_retirement_does_not_read", lock.File{}, nil},
		{"pi_retirement_does_not_read_opencode", lock.File{}, []durablecarrier.ManagedCarrierClaim{piClaim}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if edits, err := observeClasslessRetirementEdits(paths, test.locked, test.claims); err != nil || len(edits) != 0 {
				t.Fatal("classless admission read an inapplicable inventory or bypassed the locked class", edits, err)
			}
		})
	}
	writeReadinessFile(t, config, []byte(`{"plugin":["beta@2"]}`))
	edits, err := observeClasslessRetirementEdits(paths, lock.File{}, []durablecarrier.ManagedCarrierClaim{claim})
	if err != nil || len(edits) != 2 || !edits[0].Changed() {
		t.Fatal("classless retirement did not retain its existing and absent document actors", edits, err)
	}
	for _, edit := range edits {
		if _, ordered := edit.SelectedSequence().Constraint(); ordered {
			t.Fatal("classless retirement invented a desired order class")
		}
	}
	renamed := readinessOrderLock(t, root, desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, []string{"beta@2"})
	if len(renamed.Locked.OrderConstraints()) != 0 {
		t.Fatal("single desired plugin fixture unexpectedly has an order class")
	}
	if _, err := observeClasslessRetirementEdits(paths, renamed, []durablecarrier.ManagedCarrierClaim{claim}); err == nil {
		t.Fatal("a renamed still-desired source was admitted for classless retirement")
	}
	unrelated := readinessOrderLock(t, root, desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, []string{"unrelated@3"})
	if edits, err := observeClasslessRetirementEdits(paths, unrelated, []durablecarrier.ManagedCarrierClaim{claim}); err != nil || len(edits) != 2 || !edits[0].Changed() {
		t.Fatal("an unrelated desired plugin prevented exact retirement", edits, err)
	}
}

func readinessOrderCarrierClaim(t *testing.T, locked lock.File, source string) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	for _, contract := range locked.Locked.Subjects() {
		identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
		if err != nil {
			t.Fatal(err)
		}
		if admitted && string(identity.ExpectedRelation().SubjectKey()) == source {
			return testManagedCarrierClaim(t, contract)
		}
	}
	t.Fatal("fixture has no exact carrier relation", source)
	return durablecarrier.ManagedCarrierClaim{}
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
		readinessOrderPaths(t, root),
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
		readinessOrderPaths(t, root),
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
		readinessOrderPaths(t, root),
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
		readinessOrderPaths(t, root),
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
	extensions := make([]desiredextension.Extension, 0, len(sources))
	for index, source := range sources {
		spec := desiredextension.Spec{
			Name:    fmt.Sprintf("member-%d", index),
			Carrier: carrier,
			Target:  selectedTarget,
			Scope:   target.ScopeProject,
			Source:  source,
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
			readinessOrderPaths(t, root),
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
