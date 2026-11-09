package namespace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func TestEndpointSelectionUsesReferentPresenceWithoutChangingFiles(t *testing.T) {
	for _, mode := range []string{"missing", "present", "directory", "linked", "dangling", "loop", "missing parent", "ancestor file"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			preferred := filepath.Join(base, ".config.json")
			fallback := filepath.Join(base, ".claude.json")
			want := fallback
			switch mode {
			case "present":
				if err := os.WriteFile(preferred, []byte("fixture"), 0o000); err != nil {
					t.Fatal(err)
				}
				want = preferred
			case "directory":
				if err := os.Mkdir(preferred, 0o700); err != nil {
					t.Fatal(err)
				}
				want = preferred
			case "linked", "dangling", "loop":
				target := filepath.Join(base, "target")
				if mode == "linked" {
					if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
						t.Fatal(err)
					}
					want = preferred
				}
				if mode == "loop" {
					target = preferred
				}
				if err := os.Symlink(target, preferred); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
			case "missing parent":
				preferred = filepath.Join(base, "missing", ".config.json")
			case "ancestor file":
				parent := filepath.Join(base, "not-directory")
				if err := os.WriteFile(parent, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
				preferred = filepath.Join(parent, ".config.json")
			}
			selected, err := SelectEndpointBounded(t.Context(), [2]string{preferred, fallback}, imageBudget(t))
			if mode == "loop" || mode == "ancestor file" {
				if err == nil || selected != "" {
					t.Fatal("an unknown preferred location became a fallback", selected, err)
				}
			} else if err != nil || selected != want {
				t.Fatal("endpoint choice did not follow referent presence", selected, err, want)
			}
			if _, err := os.Lstat(fallback); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("selection created or changed the fallback", err)
			}
		})
	}
}

func TestEndpointSelectionKeepsUnknownAndCancellationOutOfFallback(t *testing.T) {
	preferred := filepath.Join(t.TempDir(), "missing", ".config.json")
	candidates := [2]string{preferred, filepath.Join(t.TempDir(), ".claude.json")}
	quota := errors.New("endpoint selector allowance exhausted")
	if selected, err := SelectEndpointBounded(t.Context(), candidates, &refuseProtectionBudget{failure: quota}); !errors.Is(err, quota) || selected != "" {
		t.Fatal("a failed observation selected a fallback", selected, err)
	}
	if selected, err := SelectEndpointBounded(t.Context(), [2]string{preferred, preferred}, &refuseProtectionBudget{failure: quota}); err != nil || selected != preferred {
		t.Fatal("identical candidates required irrelevant existence evidence", selected, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if selected, err := SelectEndpointBounded(ctx, candidates, imageBudget(t)); !errors.Is(err, context.Canceled) || selected != "" {
		t.Fatal("cancellation became a fallback location", selected, err)
	}
}

func TestCapturedEndpointWitnessCannotBecomeAParentDirectoryGrant(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	preferred := filepath.Join(base, ".config.json")
	point := filepath.Join(outside, "account.json")
	if err := os.WriteFile(point, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(point, preferred); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	budget := imageBudget(t)
	selection, err := CaptureEndpointRoot(t.Context(), [2]string{preferred, filepath.Join(base, ".claude.json")}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selection.Close() })
	reference, err := output.NewRootReference("selected-endpoint", "endpoint-fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Roots{}).With(reference, RootSelection{SelectedPath: selection.SelectedPath, Root: selection.Root}); err == nil {
		t.Fatal("a point selection exposed a reusable parent-directory capability")
	}
	manifest, err := configuration.ProjectRootReference()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Roots{}).With(manifest, selection); err == nil {
		t.Fatal("a point selection became a manifest directory")
	}
	roots, err := (Roots{}).With(reference, selection)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := roots.Qualify(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	bound := map[output.RootReference]struct{}{reference: {}}
	pointObservation := qualified.grants[reference].endpoint
	grant, relative, err := nativeDocumentWriterGrant(qualified.grants, bound, selection.endpoint.physical, pointObservation, nil)
	if err != nil || relative.Path() != "account.json" {
		t.Fatal("the point's own referent lost its narrowly selected witness", relative, err)
	}
	sibling, err := rootedpath.NewRelativeDestination("sibling.json")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := NewWriterPin(grant.Provenance(), sibling)
	if err != nil {
		t.Fatal(err)
	}
	capability, routeErr := grant.acquireRoute(t.Context(), sibling, budget)
	if capability != nil {
		_ = capability.Close()
	}
	if routeErr == nil || capability != nil {
		t.Fatal("a point witness supplied a directory-relative route capability", routeErr)
	}
	alias := Alias{root: grant}
	route, err := alias.ObserveRoute(t.Context(), budget)
	if err != nil {
		t.Fatal("point metadata observation required a directory route", err)
	}
	wantRoute, err := mutation.ObserveDirectoryEntryAuthorityBounded(preferred, 64, budget)
	if err != nil || !route.Equal(wantRoute) {
		t.Fatal("point route observation did not preserve the selected literal alias", route, err)
	}
	siblingPath := filepath.Join(selection.SelectedPath, "sibling.json")
	siblingObservation, err := mutation.ObserveDirectoryEntryAuthorityBounded(siblingPath, 64, budget)
	if err != nil {
		t.Fatal(err)
	}
	for _, proposal := range []*WriterPin{nil, &pin} {
		if _, _, err := nativeDocumentWriterGrant(qualified.grants, bound, siblingPath, siblingObservation, proposal); !errors.Is(err, ErrNoTrustedWriterRoot) {
			t.Fatal("a bound point or its pin authorized a sibling", err)
		}
		if _, _, err := nativeDocumentWriterGrant(qualified.grants, nil, selection.endpoint.physical, pointObservation, proposal); !errors.Is(err, ErrNoTrustedWriterRoot) {
			t.Fatal("a point grant escaped its own alias group", err)
		}
	}
	t.Run("case-folded same endpoint", func(t *testing.T) {
		alias := filepath.Join(outside, "ACCOUNT.JSON")
		actual, err := os.Stat(point)
		if err != nil {
			t.Fatal(err)
		}
		folded, err := os.Stat(alias)
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("case-folded alias requires a case-insensitive volume")
		}
		if err != nil || !os.SameFile(actual, folded) {
			t.Fatal("case-folded fixture does not name one file", err)
		}
		observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(alias, 64, budget)
		if err != nil {
			t.Fatal(err)
		}
		_, selectedRelative, err := nativeDocumentWriterGrant(qualified.grants, bound, alias, observation, nil)
		if err != nil || selectedRelative.Path() != "account.json" {
			t.Fatal("one endpoint acquired spelling-dependent permission", selectedRelative, err)
		}
	})
}

func TestEndpointQualificationDetectsSameParentChoiceChanges(t *testing.T) {
	base := t.TempDir()
	preferred, fallback := filepath.Join(base, ".config.json"), filepath.Join(base, ".claude.json")
	if err := os.WriteFile(fallback, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection, err := CaptureEndpointRoot(t.Context(), [2]string{preferred, fallback}, imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selection.Close() })
	if err := selection.qualifyLocator(t.Context(), imageBudget(t)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fallback, preferred); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err := selection.qualifyLocator(t.Context(), imageBudget(t)); err == nil {
		t.Fatal("a same-referent preferred/fallback transition changed the captured selection")
	}
}

func TestEndpointCaptureInterpretsTheLogicalParentBeforeJoiningTheFile(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	for _, directory := range []string{filepath.Join(physical, "cwd"), filepath.Join(physical, "config"), filepath.Join(base, "config")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, parent := range []string{filepath.Join(physical, "config"), filepath.Join(base, "config")} {
		if err := os.WriteFile(filepath.Join(parent, ".config.json"), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logical := filepath.Join(base, "logical-cwd")
	if err := os.Symlink(filepath.Join(physical, "cwd"), logical); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	preferred := logical + string(filepath.Separator) + filepath.FromSlash("../config/.config.json")
	selection, err := CaptureEndpointRoot(t.Context(), [2]string{preferred, filepath.Join(base, ".claude.json")}, imageBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selection.Close() })
	want := filepath.Join(physical, "config", ".config.json")
	if runtime.GOOS == "windows" {
		want = filepath.Join(base, "config", ".config.json")
	}
	current, err := os.Stat(selection.endpoint.physical)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(want)
	if err != nil || !os.SameFile(current, expected) {
		t.Fatal("endpoint normalization selected the lexical parent instead of host path semantics", selection.endpoint.physical, err)
	}
	if err := selection.qualifyLocator(t.Context(), imageBudget(t)); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointAliasesCannotSelectAnUncreatedTargetParent(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	preferred := filepath.Join(base, ".config.json")
	fallback := filepath.Join(base, ".claude.json")
	for _, target := range []string{filepath.Join(outside, "missing", "target.json"), filepath.Join(outside, "target.json")} {
		if err := os.Symlink(target, fallback); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		selection, err := CaptureEndpointRoot(t.Context(), [2]string{preferred, fallback}, imageBudget(t))
		_ = selection.Close()
		if err == nil {
			t.Fatal("a final dangling endpoint alias escaped strict referent admission", err)
		}
		if err := os.Remove(fallback); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("endpoint selection created the target parent", err)
	}
	literal := filepath.Join(base, "configured", ".claude.json")
	selection, err := CaptureEndpointRoot(t.Context(), [2]string{literal, literal}, imageBudget(t))
	if err != nil {
		t.Fatal("literal configured-parent creation lost its existing confinement", err)
	}
	t.Cleanup(func() { _ = selection.Close() })
	if err := selection.qualifyLocator(t.Context(), imageBudget(t)); err != nil {
		t.Fatal(err)
	}
	reference, err := output.NewRootReference("configured-endpoint", "endpoint-fixture-v1")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := (Roots{}).With(reference, selection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roots.Qualify(t.Context(), imageBudget(t)); err != nil {
		t.Fatal("literal endpoint creation lost its private retained witness", err)
	}
	if _, err := os.Stat(filepath.Join(base, "configured")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("endpoint capture created its literal configured parent", err)
	}
}
