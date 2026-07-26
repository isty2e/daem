package claudeplugin_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestReadInstalledInventoryTreatsMissingFileAsFreshEmptyEvidence(t *testing.T) {
	inventory, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
		ConfigRoot:  t.TempDir(),
		ProjectRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("ReadInstalledInventory returned error: %v", err)
	}
	if inventory.Availability() != observerelation.InventorySupported ||
		inventory.Freshness() != observerelation.EvidenceFresh {
		t.Fatalf("inventory = availability %q freshness %q", inventory.Availability(), inventory.Freshness())
	}
	subject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7@market", "context7", "context7@market", target.ScopeProject)
	assertState(t, observeclaudeplugin.Correlate(subject, inventory), observerelation.StateMissing, observerelation.ReasonMissing)
}

func TestReadInstalledInventoryResolvesRelativeClaudeConfigFromHostWorkDir(t *testing.T) {
	workDir := t.TempDir()
	projectRoot := t.TempDir()
	subject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7@market", "context7", "context7@market", target.ScopeGlobal)
	t.Setenv("CLAUDE_CONFIG_DIR", "relative-claude-config")
	writeInstalledPlugins(t, filepath.Join(workDir, "relative-claude-config"), `{"version":2,"plugins":{"context7@market":[{"scope":"user"}]}}`)

	inventory, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
		WorkDir:     workDir,
		ProjectRoot: projectRoot,
	})
	if err != nil {
		t.Fatalf("ReadInstalledInventory returned error: %v", err)
	}
	assertState(t, observeclaudeplugin.Correlate(subject, inventory), observerelation.StateExactCorrelation, observerelation.ReasonNone)
}

func TestReadInstalledInventoryFiltersProjectRowsAndAttachesExactCorrelation(t *testing.T) {
	configRoot := t.TempDir()
	projectRoot := t.TempDir()
	otherProject := t.TempDir()
	projectSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7@market", "project-context7", "context7@market", target.ScopeProject)
	globalSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7@market", "global-context7", "context7@market", target.ScopeGlobal)
	writeInstalledPlugins(t, configRoot, `{
  "version": 2,
  "plugins": {
    "context7@market": [
      {"scope":"user", "installPath":"/cache/user", "future":true},
      {"scope":"project", "projectPath":`+quotedPath(projectRoot)+`},
      {"scope":"project", "projectPath":`+quotedPath(otherProject)+`},
      {"scope":"local", "projectPath":`+quotedPath(projectRoot)+`},
      {"scope":"managed"}
    ]
  },
  "futureTopLevel": {"accepted": true}
}`)

	inventory, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
		ConfigRoot:  configRoot,
		ProjectRoot: projectRoot,
		Relations: []observeclaudeplugin.ScopedRelation{
			mustObservationKey(t, "project-context7", projectSubject),
			mustObservationKey(t, "global-context7", globalSubject),
		},
	})
	if err != nil {
		t.Fatalf("ReadInstalledInventory returned error: %v", err)
	}
	assertState(t, observeclaudeplugin.Correlate(projectSubject, inventory), observerelation.StateExactCorrelation, observerelation.ReasonNone)
	assertState(t, observeclaudeplugin.Correlate(globalSubject, inventory), observerelation.StateExactCorrelation, observerelation.ReasonNone)
}

func TestReadInstalledInventoryCanonicalizesProjectSymlinks(t *testing.T) {
	configRoot := t.TempDir()
	root := t.TempDir()
	subject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7@market", "context7", "context7@market", target.ScopeProject)
	realProject := filepath.Join(root, "real-project")
	if err := os.Mkdir(realProject, 0o700); err != nil {
		t.Fatalf("create real project: %v", err)
	}
	symlinkProject := filepath.Join(root, "linked-project")
	if err := os.Symlink(realProject, symlinkProject); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeInstalledPlugins(t, configRoot, `{"version":2,"plugins":{"context7@market":[{"scope":"project","projectPath":`+quotedPath(realProject)+`}]}}`)

	inventory, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
		ConfigRoot:  configRoot,
		ProjectRoot: symlinkProject,
	})
	if err != nil {
		t.Fatalf("ReadInstalledInventory returned error: %v", err)
	}
	assertState(t, observeclaudeplugin.Correlate(subject, inventory), observerelation.StateExactCorrelation, observerelation.ReasonNone)
}

func TestReadInstalledInventoryRejectsUntrustedSchemaStates(t *testing.T) {
	projectRoot := t.TempDir()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "invalid json", content: `{`, want: "EOF"},
		{name: "trailing value", content: `{"version":2,"plugins":{}} {}`, want: "multiple JSON values"},
		{name: "unsupported version", content: `{"version":3,"plugins":{}}`, want: "unsupported version 3"},
		{name: "missing plugins", content: `{"version":2}`, want: "plugins object is required"},
		{name: "null plugins", content: `{"version":2,"plugins":null}`, want: "plugins object is required"},
		{name: "plugins is array", content: `{"version":2,"plugins":[]}`, want: "JSON object is required"},
		{name: "plugin rows null", content: `{"version":2,"plugins":{"x":null}}`, want: "rows must be an array"},
		{name: "unknown scope", content: `{"version":2,"plugins":{"x":[{"scope":"workspace"}]}}`, want: "unsupported or missing scope"},
		{name: "missing scope", content: `{"version":2,"plugins":{"x":[{}]}}`, want: "unsupported or missing scope"},
		{name: "project path missing", content: `{"version":2,"plugins":{"x":[{"scope":"project"}]}}`, want: "projectPath"},
		{name: "project path relative", content: `{"version":2,"plugins":{"x":[{"scope":"project","projectPath":"relative"}]}}`, want: "absolute path required"},
		{name: "duplicate top-level key", content: `{"version":2,"version":2,"plugins":{}}`, want: "duplicate JSON object key"},
		{name: "duplicate plugin key", content: `{"version":2,"plugins":{"x":[],"x":[]}}`, want: "duplicate JSON object key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configRoot := t.TempDir()
			writeInstalledPlugins(t, configRoot, test.content)
			_, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
				ConfigRoot:  configRoot,
				ProjectRoot: projectRoot,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestReadInstalledInventoryIgnoresUnselectedFutureHostRows(t *testing.T) {
	configRoot := t.TempDir()
	projectRoot := t.TempDir()
	selected := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "selected@market", "selected", "selected@market", target.ScopeGlobal)
	writeInstalledPlugins(t, configRoot, `{"version":2,"plugins":{"selected@market":[{"scope":"user"}],"unrelated-scope@market":[{"scope":"future-scope","future":1,"future":2}],"unrelated-shape@market":{"future":"schema"}}}`)

	inventory, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
		ConfigRoot:  configRoot,
		ProjectRoot: projectRoot,
		Relations:   []observeclaudeplugin.ScopedRelation{mustObservationKey(t, "selected", selected)},
	})
	if err != nil {
		t.Fatalf("ReadInstalledInventory returned unrelated row error: %v", err)
	}
	assertState(t, observeclaudeplugin.Correlate(selected, inventory), observerelation.StateExactCorrelation, observerelation.ReasonNone)
}

func TestReadInstalledInventoryRejectsDuplicateFieldsInSelectedRows(t *testing.T) {
	configRoot := t.TempDir()
	projectRoot := t.TempDir()
	selected := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "selected@market", "selected", "selected@market", target.ScopeGlobal)
	writeInstalledPlugins(t, configRoot, `{"version":2,"plugins":{"selected@market":[{"scope":"user","scope":"user"}]}}`)

	_, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
		ConfigRoot:  configRoot,
		ProjectRoot: projectRoot,
		Relations:   []observeclaudeplugin.ScopedRelation{mustObservationKey(t, "selected", selected)},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("error = %v, want selected-row duplicate-key rejection", err)
	}
}

func TestReadInstalledInventoryIgnoresKnownUnselectedScopeForSelectedKey(t *testing.T) {
	configRoot := t.TempDir()
	projectRoot := t.TempDir()
	selected := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "selected@market", "selected", "selected@market", target.ScopeGlobal)
	writeInstalledPlugins(t, configRoot, `{"version":2,"plugins":{"selected@market":[{"scope":"user"},{"scope":"project"}]}}`)

	inventory, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
		ConfigRoot:  configRoot,
		ProjectRoot: projectRoot,
		Relations:   []observeclaudeplugin.ScopedRelation{mustObservationKey(t, "selected", selected)},
	})
	if err != nil {
		t.Fatalf("ReadInstalledInventory returned unselected project-row error: %v", err)
	}
	assertState(t, observeclaudeplugin.Correlate(selected, inventory), observerelation.StateExactCorrelation, observerelation.ReasonNone)
}

func TestReadInstalledInventoryCorrelatesSharedCarrierForEachExpectedRelation(t *testing.T) {
	configRoot := t.TempDir()
	projectRoot := t.TempDir()
	first := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7@market", "first", "context7@market", target.ScopeGlobal)
	second := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7@market", "second", "context7@market", target.ScopeGlobal)
	writeInstalledPlugins(t, configRoot, `{"version":2,"plugins":{"context7@market":[{"scope":"user"}]}}`)

	inventory, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{
		ConfigRoot:  configRoot,
		ProjectRoot: projectRoot,
		Relations: []observeclaudeplugin.ScopedRelation{
			mustObservationKey(t, "first", first),
			mustObservationKey(t, "second", second),
		},
	})
	if err != nil {
		t.Fatalf("ReadInstalledInventory returned error: %v", err)
	}
	assertState(t, observeclaudeplugin.Correlate(first, inventory), observerelation.StateExactCorrelation, observerelation.ReasonNone)
	assertState(t, observeclaudeplugin.Correlate(second, inventory), observerelation.StateExactCorrelation, observerelation.ReasonNone)
}

func writeInstalledPlugins(t *testing.T, configRoot string, content string) {
	t.Helper()
	path := filepath.Join(configRoot, "plugins", "installed_plugins.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create plugins directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write installed plugins: %v", err)
	}
}

func quotedPath(path string) string {
	return `"` + strings.ReplaceAll(path, `\`, `\\`) + `"`
}

func mustObservationKey(
	t *testing.T,
	name string,
	relation realization.DelegatedRelation,
) observeclaudeplugin.ScopedRelation {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "test", name)
	if err != nil {
		t.Fatalf("topology.NewSubjectID returned error: %v", err)
	}
	key, err := observerelation.NewCorrelationKey(
		subject,
		relation.ExpectedRelation(),
	)
	if err != nil {
		t.Fatalf("observerelation.NewCorrelationKey returned error: %v", err)
	}
	scoped, err := observeclaudeplugin.NewScopedRelation(key, relation.Scope())
	if err != nil {
		t.Fatalf("observeclaudeplugin.NewScopedRelation returned error: %v", err)
	}
	return scoped
}
