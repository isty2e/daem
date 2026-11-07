package namespace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func TestReservationSelectorsKeepIndependentReferencesWithoutBorrowingInput(t *testing.T) {
	configurationRoot, err := output.NewRootReference("opencode-configuration", "fixture-configuration-v1")
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := output.NewRootReference("opencode-state", "fixture-state-v1")
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
	if _, supplied := selectors.selectors[stateRoot]; supplied {
		t.Fatal("root collection invented an unsupplied locator")
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
	expected := map[output.RootReference]string{
		configurationRoot: filepath.Join(base, "configuration"),
		stateRoot:         filepath.Join(base, "state"),
	}
	for _, selectedRoot := range selectors.selectors {
		path, present := expected[selectedRoot.Reference()]
		if !present || selectedRoot.Path() != path {
			t.Fatalf("selector lost its reference/path association: %v at %q", selectedRoot.Reference(), selectedRoot.Path())
		}
		delete(expected, selectedRoot.Reference())
	}
	if len(expected) != 0 {
		t.Fatal("not every required root was selected")
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
	if _, supplied := selectors.selectors[second]; supplied {
		t.Fatal("root identity alone supplied a different locator contract")
	}
	secondSelector, err := NewReservationSelector(second, filepath.Join(base, "second"))
	if err != nil {
		t.Fatal(err)
	}
	selectors, err = NewReservationSelectors([]ReservationSelector{selector, secondSelector})
	if err != nil {
		t.Fatal(err)
	}
	if len(selectors.selectors) != 2 {
		t.Fatal("distinct read-only locator contracts were lost")
	}
	expected := map[output.RootReference]string{first: selector.Path(), second: secondSelector.Path()}
	for _, current := range selectors.selectors {
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
	namespace, err := Observe(t.Context(), []aggregate.DocumentAddress{alias}, fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootPiAgent: root}), fixtureProtection(t), journalTestFilesystem(), budget)
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

func TestNativeRootSelectorKeepsPhysicalParentsAndFreshLocatorBinding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Win32 normalizes parent components before following reparse points")
	}
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprintf("present=%t", present), func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, relative := range []string{"actual/inner", "other/inner", "host"} {
				if err := os.MkdirAll(filepath.Join(base, relative), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			physical := filepath.Join(base, "actual", "host")
			if present {
				if err := os.Mkdir(physical, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(base, "route")
			if err := os.Symlink(filepath.Join(base, "actual", "inner"), link); err != nil {
				t.Fatal(err)
			}
			literal := link + "/../host"
			budget := imageBudget(t)
			selection, err := CaptureRootSelector(t.Context(), literal, budget)
			if err != nil {
				t.Fatal(err)
			}
			defer selection.Root.Close()
			if selection.SelectedPath != physical || selection.literal != literal {
				t.Fatalf("locator lost its physical meaning: %+v", selection)
			}
			if !present {
				if _, err := os.Stat(physical); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("selecting a future root created it", err)
				}
			}
			reference, _ := configuration.RootReferenceForScope(target.TargetCodex, target.ScopeGlobal)
			roots, err := (Roots{}).With(reference, selection)
			if err != nil {
				t.Fatal(err)
			}
			qualified, err := roots.Qualify(t.Context(), budget)
			if err != nil || qualified.grants[reference].SelectedPath() != physical {
				t.Fatal("fresh literal root did not qualify", err)
			}
			provenance := qualified.grants[reference].Provenance()
			recorded, err := CaptureRecordedRoot(t.Context(), literal, provenance, budget)
			if err != nil {
				t.Fatal("recorded authority lost literal root semantics", err)
			}
			if err := recorded.Root.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(base, "other", "inner"), link); err != nil {
				t.Fatal(err)
			}
			if _, err := roots.Qualify(t.Context(), budget); err == nil {
				t.Fatal("a changed locator qualified the old captured root")
			}
		})
	}
}

func TestNativeRootSelectorRejectsOldLexicalJournalBinding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Win32 parent components have lexical meaning")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"actual/inner", "actual/host", "host"} {
		if err := os.MkdirAll(filepath.Join(base, relative), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "route")
	if err := os.Symlink(filepath.Join(base, "actual", "inner"), link); err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	old, err := CaptureRoot(t.Context(), filepath.Join(base, "host"), budget)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Root.Close()
	qualified, err := old.Root.AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := qualified.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	selection, err := CaptureRecordedRoot(t.Context(), link+"/../host", provenance, budget)
	if err == nil {
		_ = selection.Root.Close()
		t.Fatal("an old lexical journal was redirected into the host's physical root")
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
