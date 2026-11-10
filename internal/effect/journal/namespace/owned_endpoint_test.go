package namespace

import (
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
