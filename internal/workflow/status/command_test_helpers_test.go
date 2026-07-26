package status

import (
	"os"
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func statusLockfileFromRecords(
	t *testing.T,
	records ...lock.LockedSubjectContract,
) lock.File {
	t.Helper()
	return snapshottest.File(t, records...)
}

func statusClaudePluginExtensionLockfile(
	t *testing.T,
	declarationID string,
	pluginKey string,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    declarationID,
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, pluginKey),
	})
	return snapshottest.ExtensionCarrierFile(t, value)
}

func statusCodexPluginExtensionLockfile(
	t *testing.T,
	declarationID string,
	pluginKey string,
) (lock.File, realization.DelegatedRelation) {
	t.Helper()
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name:    declarationID,
		Carrier: desiredextension.CarrierCodexPlugin,
		Target:  target.TargetCodex,
		Scope:   target.ScopeGlobal,
		Source:  desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, pluginKey),
	})
	return snapshottest.ExtensionCarrierFile(t, value)
}

func statusClaudeSelection(t *testing.T, requested ...string) targetselection.Selection {
	t.Helper()
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{target.TargetCodex, target.TargetClaudeCode},
		requested,
	)
	if err != nil {
		t.Fatalf("targetselection.ForAvailableTargets: %v", err)
	}
	return selection
}

func resolveTestPaths(t *testing.T, manifestPath string) daempaths.Paths {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	return paths
}

func writeTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
}
