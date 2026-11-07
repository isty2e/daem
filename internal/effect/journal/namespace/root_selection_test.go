package namespace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

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
	namespace, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, Roots{PiAgent: root}, journalTestFilesystem(), budget)
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
