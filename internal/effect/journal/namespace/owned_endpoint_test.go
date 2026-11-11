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
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestRecordedOwnedEndpointRetainsItsParentIncarnation(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "source")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "owned.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := observation.Exact()
	if !known {
		t.Fatal("source identity is not exact")
	}
	fresh, err := CaptureOwnedEndpointRoot(t.Context(), path, exact, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	provenance := pointWitnessProvenance(t, fresh, budget)
	recorded, err := CaptureRecordedOwnedEndpointRoot(t.Context(), path, exact, provenance, budget)
	if err != nil {
		t.Fatal("unchanged owned source did not recapture", err)
	}
	t.Cleanup(func() { _ = recorded.Close() })
	if recorded.Root != nil || recorded.endpoint.basis != endpointFromOwned || recorded.endpoint.alternative != "" {
		t.Fatal("recorded source gained a directory grant or a current chooser")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if value, err := CaptureRecordedOwnedEndpointRoot(cancelled, path, exact, provenance, budget); !errors.Is(err, context.Canceled) || value.capturedRoot() != nil {
		t.Fatal("cancelled source capture returned retained authority", err)
	}
	if err := os.Rename(parent, parent+"-retired"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil || !current.MatchesRetainedExact(exact) {
		t.Fatal("parent-replacement control changed the exact file-slot identity", err)
	}
	if value, err := CaptureRecordedOwnedEndpointRoot(t.Context(), path, exact, provenance, budget); err == nil || value.capturedRoot() != nil {
		t.Fatal("recorded source accepted a different parent incarnation", err)
	}
	freshParent, err := CaptureOwnedEndpointRoot(t.Context(), path, exact, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = freshParent.Close() })
	newParent, err := CaptureRecordedOwnedEndpointRoot(t.Context(), path, exact, pointWitnessProvenance(t, freshParent, budget), budget)
	if err != nil {
		t.Fatal("fresh parent control failed for the same file-slot identity", err)
	}
	t.Cleanup(func() { _ = newParent.Close() })
}

func TestMissingOwnedEndpointIsOnlyAnIdentityWitness(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "source")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "owned.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := observed.Exact()
	if !known {
		t.Fatal("source identity is not exact")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, missingParent := range []bool{false, true} {
		if missingParent {
			if err := os.Remove(parent); err != nil {
				t.Fatal(err)
			}
		}
		selected, err := CaptureOwnedEndpointRoot(t.Context(), path, exact, budget)
		if err != nil {
			t.Fatal("missing source lost its metadata-retirement witness", err)
		}
		if selected.Root != nil || selected.endpoint == nil {
			t.Fatal("missing source exposed a containing directory grant")
		}
		provenance := pointWitnessProvenance(t, selected, budget)
		recorded, err := CaptureRecordedOwnedEndpointRoot(t.Context(), path, exact, provenance, budget)
		if err != nil {
			_ = selected.Close()
			t.Fatal("missing source witness did not recapture", err)
		}
		if err := errors.Join(recorded.Close(), selected.Close()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("identity capture created a source file", err)
		}
		if missingParent {
			if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("identity capture created a source parent", err)
			}
		}
	}
}

func TestRecordedMissingOwnedParentRetainsItsExistingAncestor(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "later")
	path := filepath.Join(parent, "owned.json")
	budget := imageBudget(t)
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := observed.Exact()
	if !known {
		t.Fatal("ASCII missing source did not retain exact identity")
	}
	missing, err := CaptureOwnedEndpointRoot(t.Context(), path, exact, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Close()
	provenance := pointWitnessProvenance(t, missing, budget)
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"later":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	recorded, err := CaptureRecordedOwnedEndpointRoot(t.Context(), path, exact, provenance, budget)
	if err != nil {
		t.Fatal("a later child invalidated its unchanged recorded ancestor", err)
	}
	defer recorded.Close()
	if recorded.Root != nil || pointWitnessProvenance(t, recorded, budget) != provenance {
		t.Fatal("later parent replaced the retained ancestor or gained a directory grant")
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != `{"later":true}` {
		t.Fatal("identity recapture changed the later file", err)
	}
}

func TestOwnedEndpointQualifiesOnlyItsRetainedPhysicalFile(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "owned.json")
	if err := os.WriteFile(path, []byte("{\"theme\":\"preserved\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := imageBudget(t)
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := observed.Exact()
	if !known {
		t.Fatal("existing source has no exact authority")
	}
	selected, err := CaptureOwnedEndpointRoot(t.Context(), path, exact, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = selected.Close() })
	destination, err := output.Parse(aggregate.ClaudeGlobalMCPConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := configuration.OwnedMCPSourceBinding(configuration.AliasInput{
		Target: target.TargetClaudeCode, Scope: target.ScopeGlobal, Destination: destination,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Root != nil {
		t.Fatal("owned file exposed its parent as a directory grant")
	}
	roots, err := (Roots{}).With(binding.RootReference(), selected)
	if err != nil {
		t.Fatal(err)
	}
	currentBinding, err := configuration.RootBindingForAlias(configuration.AliasInput{
		Target: target.TargetClaudeCode, Scope: target.ScopeGlobal, Destination: destination,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Roots{}).With(currentBinding.RootReference(), selected); err == nil {
		t.Fatal("a fixed owned source impersonated a current point chooser")
	}
	qualified, err := roots.Qualify(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	grant := qualified.grants[binding.RootReference()]
	if _, hasAlternative := grant.PointAlternative(); hasAlternative {
		t.Fatal("owned source acquired a preferred/fallback meaning")
	}
	sibling, err := rootedpath.NewRelativeDestination("sibling.json")
	if err != nil {
		t.Fatal(err)
	}
	if capability, err := grant.acquireRoute(t.Context(), sibling, budget); err == nil || capability != nil {
		t.Fatal("fixed source granted authority over a sibling", err)
	}
	other := filepath.Join(base, "other.json")
	if err := os.WriteFile(other, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rejected, err := CaptureOwnedEndpointRoot(t.Context(), other, exact, budget); err == nil || rejected.capturedRoot() != nil {
		t.Fatal("retained source identity authorized a different path", err)
	}
	chooser, err := CaptureEndpointRoot(t.Context(), [2]string{path, other}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = chooser.Close() })
	if _, err := (Roots{}).With(binding.RootReference(), chooser); err == nil {
		t.Fatal("a current chooser impersonated an owned source")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Skip("symlink creation is unavailable", err)
	}
	if err := selected.qualifyLocator(t.Context(), budget); err == nil {
		t.Fatal("source requalification followed a new leaf alias away from its retained identity")
	}
	if content, err := os.ReadFile(other); err != nil || string(content) != "{}" {
		t.Fatal("source qualification changed unrelated content", err)
	}
}
