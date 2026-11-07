package namespace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestEmptyRootSelectionStillHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	qualified, err := (Roots{}).Qualify(ctx, imageBudget(t))
	if !errors.Is(err, context.Canceled) || len(qualified.grants) != 0 {
		t.Fatalf("cancelled empty selection returned grants or success: %v, %v", qualified.grants, err)
	}
}

func TestRootSelectionsKeepIndependentIdentitiesAndSnapshots(t *testing.T) {
	var roots Roots
	var references []output.RootReference
	expected := make(map[output.RootReference]RootSelection)
	for _, identity := range []string{"configuration", "account", "data", "state"} {
		reference, err := output.NewRootReference(identity, "fixture-"+identity+"-v1")
		if err != nil {
			t.Fatal(err)
		}
		selection := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), identity))
		previous := roots
		roots, err = roots.With(reference, selection)
		if err != nil {
			t.Fatal(err)
		}
		if _, supplied := previous.Selection(reference); supplied {
			t.Fatal("adding a selected root changed an earlier selection snapshot")
		}
		references = append(references, reference)
		expected[reference] = selection
	}
	qualified, err := roots.Qualify(t.Context(), imageBudget(t))
	if err != nil || len(qualified.grants) != 4 {
		t.Fatalf("independent root qualification = %d grants, %v", len(qualified.grants), err)
	}
	for reference, selection := range expected {
		actual, supplied := roots.Selection(reference)
		grant, granted := qualified.grants[reference]
		if !supplied || !granted || actual != selection || grant.selection != selection {
			t.Fatalf("root association changed for %v: selection=%v grant=%v", reference, actual, grant.selection)
		}
		authority, err := selection.Root.Authority()
		if err != nil {
			t.Fatal(err)
		}
		provenance, err := authority.Provenance()
		if err != nil || grant.Provenance() != provenance {
			t.Fatalf("qualified authority does not match %v: %v", reference, err)
		}
	}
	first, supplied := roots.Selection(references[0])
	if !supplied {
		t.Fatal("selected root was lost")
	}
	replacement := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "replacement"))
	updated, err := roots.With(references[0], replacement)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := roots.Selection(references[0])
	current, _ := updated.Selection(references[0])
	if old != first || current != replacement {
		t.Fatal("replacing a binding changed another immutable selection")
	}
	current.SelectedPath = "changed copy"
	again, _ := updated.Selection(references[0])
	if again != replacement {
		t.Fatal("a returned selection copy changed the root index")
	}
}

func TestWriterRootIdentityCannotChangeLocatorContractInsideASelection(t *testing.T) {
	first, err := output.NewRootReference("fixture-root", "fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := output.NewRootReference("fixture-root", "fixture-v2")
	if err != nil {
		t.Fatal(err)
	}
	selection := nativeNamespaceRoot(t, t.TempDir())
	roots, err := (Roots{}).With(first, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.With(second, selection); err == nil {
		t.Fatal("two locator contracts were accepted for one writer-root identity")
	}
	if _, supplied := roots.Selection(second); supplied {
		t.Fatal("a root identity bypassed the requested locator contract")
	}
	if _, err := roots.With(output.RootReference{}, selection); err == nil {
		t.Fatal("a zero root reference became a selection")
	}
	if _, err := roots.With(first, RootSelection{}); err == nil {
		t.Fatal("a supplied reference without a retained root became a selection")
	}
	missingHandle := selection
	missingHandle.Root = nil
	if _, err := roots.With(first, missingHandle); err == nil {
		t.Fatal("a valid selector without its retained handle became a selected binding")
	}
	for _, path := range []string{"relative", selection.SelectedPath + string(filepath.Separator) + ".", selection.SelectedPath + "\x00"} {
		invalid := selection
		invalid.SelectedPath = path
		if _, err := roots.With(first, invalid); err == nil {
			t.Fatalf("malformed selector %q became a selected binding", path)
		}
	}
	if err := selection.Root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := roots.Qualify(t.Context(), imageBudget(t)); err == nil {
		t.Fatal("a closed retained root qualified through the root index")
	}
}

func TestReservationSelectorsUseIndependentlyKnownRequirements(t *testing.T) {
	var empty ReservationSelectors
	if selected, err := empty.Select(configuration.ProtectionRequirements{}); err == nil || selected != nil {
		t.Fatal("unknown requirements became an empty protection policy")
	}
	if selected, err := empty.Select(configuration.NoProtectionRequirements()); err != nil || len(selected) != 0 {
		t.Fatalf("known-no-protection needs no selectors: %v, %v", selected, err)
	}

	configurationRoot, err := output.NewRootReference("opencode-configuration", "fixture-configuration-v1")
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := output.NewRootReference("opencode-state", "fixture-state-v1")
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := configuration.RequireProtectionRoots([]output.RootReference{configurationRoot, stateRoot})
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	selector, err := NewReservationSelector(configurationRoot, filepath.Join(base, "configuration"))
	if err != nil {
		t.Fatal(err)
	}
	selectors, err := NewReservationSelectors([]ReservationSelector{selector})
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := selectors.Select(requirements); err == nil || selected != nil {
		t.Fatal("a later missing root returned partial protection")
	}
	stateSelector, err := NewReservationSelector(stateRoot, filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	input := []ReservationSelector{stateSelector, selector}
	selectors, err = NewReservationSelectors(input)
	if err != nil {
		t.Fatal(err)
	}
	clear(input)
	selected, err := selectors.Select(requirements)
	if err != nil || len(selected) != 2 {
		t.Fatalf("independent root selection = %v, %v", selected, err)
	}
	if selected, err := selectors.Select(configuration.NoProtectionRequirements()); err != nil || len(selected) != 0 {
		t.Fatalf("unused selectors invented a protection requirement: %v, %v", selected, err)
	}
	expected := map[output.RootReference]string{
		configurationRoot: filepath.Join(base, "configuration"),
		stateRoot:         filepath.Join(base, "state"),
	}
	for _, selectedRoot := range selected {
		path, present := expected[selectedRoot.Reference()]
		if !present || selectedRoot.Path() != path {
			t.Fatalf("selector lost its reference/path association: %v at %q", selectedRoot.Reference(), selectedRoot.Path())
		}
		delete(expected, selectedRoot.Reference())
	}
	if len(expected) != 0 {
		t.Fatal("not every required root was selected")
	}
	clear(selected)
	if selected, err := selectors.Select(requirements); err != nil || len(selected) != 2 {
		t.Fatalf("mutating returned selectors changed selection: %v, %v", selected, err)
	}
	if _, err := os.Lstat(filepath.Join(base, "state")); !os.IsNotExist(err) {
		t.Fatalf("a read-only selector touched the future root: %v", err)
	}
}

func TestReservationSelectorsRejectConflictingOrUnmatchedLocatorContracts(t *testing.T) {
	first, err := output.NewRootReference("host-data", "fixture-data-v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := output.NewRootReference("host-data", "fixture-data-v2")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	selector, err := NewReservationSelector(first, filepath.Join(base, "first"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewReservationSelector(first, filepath.Join(base, "other"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewReservationSelectors([]ReservationSelector{{}}); err == nil {
		t.Fatal("zero selector was admitted")
	}
	if _, err := NewReservationSelectors([]ReservationSelector{selector, other}); err == nil {
		t.Fatal("one root contract acquired conflicting selectors")
	}
	selectors, err := NewReservationSelectors([]ReservationSelector{selector, selector})
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := configuration.RequireProtectionRoots([]output.RootReference{second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selectors.Select(requirements); err == nil {
		t.Fatal("root identity alone bypassed the required locator contract")
	}
	secondSelector, err := NewReservationSelector(second, filepath.Join(base, "second"))
	if err != nil {
		t.Fatal(err)
	}
	selectors, err = NewReservationSelectors([]ReservationSelector{selector, secondSelector})
	if err != nil {
		t.Fatal(err)
	}
	requirements, err = configuration.RequireProtectionRoots([]output.RootReference{first, second, first})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := selectors.Select(requirements)
	if err != nil || len(selected) != 2 {
		t.Fatalf("distinct read-only locator contracts were lost: %v, %v", selected, err)
	}
	expected := map[output.RootReference]string{first: selector.Path(), second: secondSelector.Path()}
	for _, current := range selected {
		path, present := expected[current.Reference()]
		if !present || current.Path() != path {
			t.Fatalf("locator contract lost its selected path: %v at %q", current.Reference(), current.Path())
		}
		delete(expected, current.Reference())
	}
	if len(expected) != 0 {
		t.Fatal("not every locator contract was selected")
	}
	for _, path := range []string{"", "relative", " /absolute", string(os.PathSeparator) + "bad\x00path"} {
		if _, err := NewReservationSelector(first, path); err == nil {
			t.Fatalf("invalid selector %q was admitted", path)
		}
	}
	spacedPath := filepath.Join(base, "directory ")
	spaced, err := NewReservationSelector(first, spacedPath)
	if err != nil || spaced.Path() != spacedPath {
		t.Fatalf("generic selector changed a valid path component: %q, %v", spaced.Path(), err)
	}
}

func TestReservationSelectorPreservesSymlinkParentMeaning(t *testing.T) {
	base := t.TempDir()
	actual := filepath.Join(base, "actual", "codex")
	for _, path := range []string{filepath.Join(base, "actual", "inner"), actual, filepath.Join(base, "codex")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "actual", "inner"), link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	reference, err := output.NewRootReference("fixture-root", "fixture-literal-v1")
	if err != nil {
		t.Fatal(err)
	}
	literal := link + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "codex"
	selector, err := NewReservationSelector(reference, literal)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := filepath.EvalSymlinks(selector.Path())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.EvalSymlinks(actual)
	if err != nil {
		t.Fatal(err)
	}
	if observed != expected || selector.Path() != literal {
		t.Fatalf("selector changed literal path meaning: got %q (%q), expected %q", selector.Path(), observed, expected)
	}
}

func TestNativeMissingRootSelectionDoesNotGrantSiblingWriters(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(base, "agent")
	budget := physicalwork.NewCaptureBudget(nil)
	root, err := CaptureRoot(t.Context(), selected, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Root.Close()
	outside := filepath.Join(base, "outside.json")
	if err := os.WriteFile(outside, []byte("{\"outside\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(selected, "settings.json")); err != nil {
		t.Fatal(err)
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json")
	namespace, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: root}), journalTestFilesystem(), budget)
	if err == nil || namespace.writerRoot != nil {
		t.Fatal("a missing-root anchor granted an unselected sibling writer")
	}
	actual, err := os.ReadFile(outside)
	if err != nil || string(actual) != "{\"outside\":true}\n" {
		t.Fatal("refusal changed the sibling", err)
	}
}

func TestNativeRecordedExistingRootCannotBecomeAnAncestorFallback(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(base, "agent")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	budget := physicalwork.NewCaptureBudget(nil)
	root, err := CaptureRoot(t.Context(), selected, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Root.Close()
	authority, err := root.Root.AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(selected, selected+"-previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	current, err := CaptureRecordedRoot(t.Context(), selected, provenance, budget)
	if err == nil {
		_ = current.Root.Close()
		t.Fatal("an existing root incarnation change was disguised as first-root creation")
	}
}

func fixtureNativeRoots(t *testing.T, selections map[output.RootRole]RootSelection) Roots {
	t.Helper()
	var roots Roots
	for role, selection := range selections {
		if selection == (RootSelection{}) {
			continue
		}
		var reference output.RootReference
		var err error
		switch role {
		case output.RootProject:
			reference, err = configuration.ProjectRootReference()
		case output.RootPiAgent:
			reference, err = configuration.RootReferenceForScope(target.TargetPi, target.ScopeGlobal)
		case output.RootCodexHome:
			reference, err = configuration.RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
		default:
			t.Fatalf("unknown legacy fixture root role %q", role)
		}
		if err != nil {
			t.Fatal(err)
		}
		roots, err = roots.With(reference, selection)
		if err != nil {
			t.Fatal(err)
		}
	}
	return roots
}
