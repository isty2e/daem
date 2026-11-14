package namespace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func TestRecordedPointKeepsFallbackWhenPreferredAppears(t *testing.T) {
	root := t.TempDir()
	preferred, fallback := filepath.Join(root, "preferred.json"), filepath.Join(root, "fallback.json")
	if err := os.WriteFile(fallback, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	fresh, err := CaptureEndpointRoot(t.Context(), [2]string{preferred, fallback}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	expected := pointWitnessProvenance(t, fresh, budget)
	if alternative, point := fresh.pointAlternative(); !point || alternative != configuration.PointFallback {
		t.Fatal("fresh selection did not retain its fallback role")
	}
	if err := os.WriteFile(preferred, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fresh.qualifyLocator(t.Context(), budget); err == nil {
		t.Fatal("fresh selection ignored a newly preferred alternative")
	}
	recorded, err := CaptureRecordedEndpointRoot(t.Context(), fallback, configuration.PointFallback, expected, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorded.Close() })
	reference, err := output.NewRootReference("recorded-point", "fixture-point-v1")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := (Roots{}).With(reference, recorded)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		qualified, err := roots.Qualify(t.Context(), budget)
		if err != nil {
			t.Fatal("frozen point reran the current chooser", err)
		}
		grant, present := qualified.grants[reference]
		if !present || grant.Provenance() != expected {
			t.Fatal("frozen point lost its recorded witness")
		}
		if alternative, point := grant.PointAlternative(); !point || alternative != configuration.PointFallback {
			t.Fatal("qualified point lost its alternative identity")
		}
		route, err := grant.observeEndpointRoute(t.Context(), budget)
		if err != nil {
			t.Fatal("route requalification reran the current chooser", err)
		}
		want, err := mutation.ObserveDirectoryEntryAuthorityBounded(fresh.endpoint.physical, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil || !route.Equal(want) {
			t.Fatal("frozen route changed files", err)
		}
	}
}

func TestRecordedPointRetainsAncestorWitnessAfterParentCreation(t *testing.T) {
	root := t.TempDir()
	preferred := filepath.Join(root, "preferred.json")
	fallback := filepath.Join(root, "configured", "fallback.json")
	budget := imageBudget(t)
	fresh, err := CaptureEndpointRoot(t.Context(), [2]string{preferred, fallback}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	if fresh.creation == nil {
		t.Fatal("fixture did not retain confined parent creation")
	}
	expected := pointWitnessProvenance(t, fresh, budget)
	if err := os.Mkdir(filepath.Dir(fallback), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorded, err := CaptureRecordedEndpointRoot(t.Context(), fallback, configuration.PointFallback, expected, budget)
	if err != nil {
		t.Fatal("recorded ancestor did not cover its now-created child", err)
	}
	t.Cleanup(func() { _ = recorded.Close() })
	if actual := pointWitnessProvenance(t, recorded, budget); actual != expected {
		t.Fatal("recovery replaced recorded ancestor authority with the new parent")
	}
	if err := recorded.qualifyLocator(t.Context(), budget); err != nil {
		t.Fatal(err)
	}
	if _, err := recorded.qualifyCreation(t.Context(), budget); err != nil {
		t.Fatal("retained creation contract did not qualify its completed parent", err)
	}
}

func TestRecordedPointCannotCreateBehindRetargetedFinalAlias(t *testing.T) {
	root := t.TempDir()
	selected := filepath.Join(root, "configured", "fallback.json")
	budget := imageBudget(t)
	fresh, err := CaptureEndpointRoot(t.Context(), [2]string{filepath.Join(root, "preferred.json"), selected}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	expected := pointWitnessProvenance(t, fresh, budget)
	if err := os.Mkdir(filepath.Dir(selected), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "other", "target.json")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, selected); err != nil {
		t.Skip("symlink creation is unavailable", err)
	}
	parent, err := CaptureRecordedRoot(t.Context(), filepath.Dir(target), expected, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	_, literalParent, err := endpointRoutePath(t.Context(), selected, budget)
	if err != nil || parent.creation == nil || literalParent == parent.SelectedPath {
		t.Fatal("fixture did not isolate a different retained parent below the recorded ancestor", err)
	}
	if rejected, err := CaptureRecordedEndpointRoot(t.Context(), selected, configuration.PointFallback, expected, budget); !errors.Is(err, ErrNoTrustedWriterRoot) || rejected.capturedRoot() != nil {
		t.Fatal("final alias conferred creation authority behind another parent", err)
	}
	if link, err := os.Readlink(selected); err != nil || link != target {
		t.Fatal("refused point capture changed the alias", err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "fixture" {
		t.Fatal("refused point capture changed its referent", err)
	}
}

func TestRecordedPointRefusesRetargetedSelectedFile(t *testing.T) {
	root := t.TempDir()
	first, second, selected := filepath.Join(root, "first.json"), filepath.Join(root, "second.json"), filepath.Join(root, "alias.json")
	for _, target := range []string{first, second} {
		if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(first, selected); err != nil {
		t.Skip("symlink creation is unavailable", err)
	}
	budget := imageBudget(t)
	fresh, err := CaptureEndpointRoot(t.Context(), [2]string{selected, selected}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	if alternative, point := fresh.pointAlternative(); !point || alternative != configuration.PointPreferred {
		t.Fatal("identical candidates did not retain the preferred role")
	}
	recorded, err := CaptureRecordedEndpointRoot(t.Context(), selected, configuration.PointPreferred, pointWitnessProvenance(t, fresh, budget), budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorded.Close() })
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, selected); err != nil {
		t.Fatal(err)
	}
	if err := recorded.qualifyLocator(t.Context(), budget); err == nil {
		t.Fatal("recorded alternative ignored its changed referent")
	}
}

func TestRecordedPointRejectsInvalidRoleAndWitnessWithoutCapability(t *testing.T) {
	root := t.TempDir()
	selected := filepath.Join(root, "settings.json")
	budget := imageBudget(t)
	fresh, err := CaptureEndpointRoot(t.Context(), [2]string{selected, selected}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	expected := pointWitnessProvenance(t, fresh, budget)
	for _, alternative := range []configuration.PointAlternative{"", "future"} {
		if rejected, err := CaptureRecordedEndpointRoot(t.Context(), selected, alternative, expected, budget); err == nil || rejected.capturedRoot() != nil {
			t.Fatal("unknown alternative returned a retained capability", err)
		}
	}
	if rejected, err := CaptureRecordedEndpointRoot(t.Context(), selected, configuration.PointPreferred, rootedpath.AuthorityProvenance{}, budget); err == nil || rejected.capturedRoot() != nil {
		t.Fatal("zero witness returned a retained capability", err)
	}
	unrelated, err := CaptureRoot(t.Context(), t.TempDir(), budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unrelated.Close() })
	if rejected, err := CaptureRecordedEndpointRoot(t.Context(), selected, configuration.PointPreferred, pointWitnessProvenance(t, unrelated, budget), budget); err == nil || rejected.capturedRoot() != nil {
		t.Fatal("unrelated witness became authority for the selected point", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if rejected, err := CaptureRecordedEndpointRoot(cancelled, selected, configuration.PointPreferred, expected, budget); !errors.Is(err, context.Canceled) || rejected.capturedRoot() != nil {
		t.Fatal("canceled capture returned a retained capability", err)
	}
}

func pointWitnessProvenance(t *testing.T, selection RootSelection, budget rootedpath.PhysicalTraversalBudget) rootedpath.AuthorityProvenance {
	t.Helper()
	authority, err := selection.capturedRoot().AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	return provenance
}
