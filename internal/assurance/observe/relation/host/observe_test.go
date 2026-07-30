package relationhost

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestObserveRejectsMissingOrCanceledContext(t *testing.T) {
	if _, err := Observe(nil, Input{}); err == nil {
		t.Fatal("Observe returned nil error for nil context")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Observe(ctx, Input{}); err != context.Canceled {
		t.Fatalf("Observe error = %v, want context.Canceled", err)
	}
}

func TestObserveRejectsOnlyCorrelationOutsideSelectedCarriers(t *testing.T) {
	key := relationobserve.CorrelationKey{}
	if _, err := Observe(context.Background(), Input{OnlyCorrelation: &key}); err == nil {
		t.Fatal("Observe returned nil error for unselected observation-only expectation")
	}
}

func TestNewObserverCatalogRejectsDuplicateCarrierAndSortsRows(t *testing.T) {
	observe := func(Input, []carrierRecord) (relationobserve.BatchSpec, error) {
		return relationobserve.BatchSpec{}, nil
	}
	first := passiveObserver{carrier: desiredextension.CarrierPiPackage, observe: observe}
	second := passiveObserver{carrier: desiredextension.CarrierClaudeCodePlugin, observe: observe}

	catalog, err := newObserverCatalog([]passiveObserver{first, second})
	if err != nil {
		t.Fatalf("newObserverCatalog returned error: %v", err)
	}
	if len(catalog) != 2 ||
		catalog[0].carrier != second.carrier ||
		catalog[1].carrier != first.carrier {
		t.Fatalf("catalog order = %#v, want lexical carrier order", catalog)
	}

	if _, err := newObserverCatalog([]passiveObserver{first, first}); err == nil {
		t.Fatal("newObserverCatalog accepted duplicate carrier")
	}
	if _, err := newObserverCatalog([]passiveObserver{{observe: observe}}); err == nil {
		t.Fatal("newObserverCatalog accepted empty carrier")
	}
	if _, err := newObserverCatalog([]passiveObserver{{carrier: first.carrier}}); err == nil {
		t.Fatal("newObserverCatalog accepted nil observer")
	}
}

func TestDefaultObserverCatalogIncludesAdmittedPassiveObservers(t *testing.T) {
	catalog, err := defaultObserverCatalog()
	if err != nil {
		t.Fatalf("defaultObserverCatalog returned error: %v", err)
	}
	seen := make(map[desiredextension.Carrier]bool, len(catalog))
	for _, observer := range catalog {
		seen[observer.carrier] = true
	}
	for _, carrier := range []desiredextension.Carrier{
		desiredextension.CarrierAntigravityCLIPlugin,
		desiredextension.CarrierClaudeCodePlugin,
		desiredextension.CarrierCodexPlugin,
		desiredextension.CarrierOpenCodePlugin,
		desiredextension.CarrierPiPackage,
	} {
		if !seen[carrier] {
			t.Fatalf("default observer catalog omitted %q", carrier)
		}
	}
}

func TestObserveOpenCodePluginsUsesSelectedScopeAndConfigKinds(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	projectConfig := filepath.Join(projectRoot, ".opencode")
	globalConfig := filepath.Join(root, "xdg", "opencode")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	if err := os.MkdirAll(projectConfig, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(globalConfig, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(projectConfig, "opencode.jsonc"),
		[]byte(`{"plugin":["@acme/shared@1"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(projectConfig, "tui.json"),
		[]byte(`{"plugin":[["@acme/shared@1",{"theme":true}]]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(globalConfig, "opencode.json"),
		[]byte(`{"plugin":["global-noise"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	key := mustOpenCodeCorrelationKey(t, "@acme/shared@1")
	spec, err := observeOpenCodePlugins(
		Input{Paths: daempaths.Paths{ManifestRoot: projectRoot}},
		[]carrierRecord{{
			key:     key,
			carrier: desiredextension.CarrierOpenCodePlugin,
			scope:   target.ScopeProject,
		}},
	)
	if err != nil {
		t.Fatalf("observeOpenCodePlugins: %v", err)
	}
	if len(spec.Correlations) != 1 ||
		spec.Correlations[0].Result.State() != relationobserve.StateExactCorrelation {
		t.Fatalf("OpenCode correlations = %#v, want one source-exact logical row", spec.Correlations)
	}
	if len(spec.AuthorityPaths) != 4 {
		t.Fatalf("OpenCode authority paths = %#v, want every config candidate", spec.AuthorityPaths)
	}
	gotPaths := map[string]target.Scope{}
	for _, path := range spec.AuthorityPaths {
		gotPaths[path.Path()] = path.Scope()
	}
	for _, path := range []string{
		filepath.Join(projectConfig, "opencode.json"),
		filepath.Join(projectConfig, "opencode.jsonc"),
		filepath.Join(projectConfig, "tui.json"),
		filepath.Join(projectConfig, "tui.jsonc"),
	} {
		if gotPaths[path] != target.ScopeProject {
			t.Fatalf("OpenCode authority path %q scope = %q, want project", path, gotPaths[path])
		}
	}
}

func TestObserveOpenCodePluginsCombinesJSONAndJSONC(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, ".opencode")
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configRoot, "opencode.json"),
		[]byte(`{"plugin":["selected"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configRoot, "opencode.jsonc"),
		[]byte(`{"plugin":["shadow"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	key := mustOpenCodeCorrelationKey(t, "shadow")
	spec, err := observeOpenCodePlugins(
		Input{Paths: daempaths.Paths{ManifestRoot: root}},
		[]carrierRecord{{
			key:     key,
			carrier: desiredextension.CarrierOpenCodePlugin,
			scope:   target.ScopeProject,
		}},
	)
	if err != nil {
		t.Fatalf("observeOpenCodePlugins: %v", err)
	}
	if got := spec.Correlations[0].Result.State(); got != relationobserve.StateExactCorrelation {
		t.Fatalf("OpenCode JSONC correlation = %q, want exact", got)
	}
	if len(spec.AuthorityPaths) != 4 ||
		spec.AuthorityPaths[0].Path() != filepath.Join(configRoot, "opencode.json") ||
		spec.AuthorityPaths[1].Path() != filepath.Join(configRoot, "opencode.jsonc") {
		t.Fatalf("server candidate authority = %#v", spec.AuthorityPaths)
	}
}

func TestObserveOpenCodePluginsKeepsProjectAndGlobalScopesDistinct(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	projectConfig := filepath.Join(projectRoot, ".opencode")
	globalBase := filepath.Join(root, "xdg")
	globalConfig := filepath.Join(globalBase, "opencode")
	t.Setenv("XDG_CONFIG_HOME", globalBase)
	for _, directory := range []string{projectConfig, globalConfig} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(projectConfig, "opencode.json"),
		filepath.Join(globalConfig, "opencode.json"),
	} {
		if err := os.WriteFile(path, []byte(`{"plugin":["shared"]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	projectKey := mustOpenCodeCorrelationKey(t, "shared")
	globalKey := mustOpenCodeCorrelationKeyWithSubject(t, "global-test", "shared")
	spec, err := observeOpenCodePlugins(
		Input{Paths: daempaths.Paths{ManifestRoot: projectRoot}},
		[]carrierRecord{
			{
				key:     projectKey,
				carrier: desiredextension.CarrierOpenCodePlugin,
				scope:   target.ScopeProject,
			},
			{
				key:     globalKey,
				carrier: desiredextension.CarrierOpenCodePlugin,
				scope:   target.ScopeGlobal,
			},
		},
	)
	if err != nil {
		t.Fatalf("observeOpenCodePlugins: %v", err)
	}
	if len(spec.Correlations) != 2 {
		t.Fatalf("OpenCode correlations = %#v, want two scope-specific rows", spec.Correlations)
	}
	if got := spec.Correlations[0].Result.State(); got != relationobserve.StateExactCorrelation {
		t.Fatalf("project correlation = %q, want exact correlation", got)
	}
	if got := spec.Correlations[1].Result.State(); got != relationobserve.StateExactCorrelation {
		t.Fatalf("global correlation = %q, want exact correlation", got)
	}
	gotPaths := map[string]target.Scope{}
	for _, path := range spec.AuthorityPaths {
		gotPaths[path.Path()] = path.Scope()
	}
	if gotPaths[filepath.Join(projectConfig, "opencode.json")] != target.ScopeProject {
		t.Fatal("project OpenCode authority path is absent or has wrong scope")
	}
	if gotPaths[filepath.Join(globalConfig, "opencode.json")] != target.ScopeGlobal {
		t.Fatal("global OpenCode authority path is absent or has wrong scope")
	}
}

func TestObserveOpenCodePluginsRejectsMalformedOrDuplicateSelectedRows(t *testing.T) {
	tests := map[string]string{
		"malformed": `{"plugin":[`,
		"duplicate": `{"plugin":["alpha","alpha"]}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			configRoot := filepath.Join(root, ".opencode")
			if err := os.MkdirAll(configRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				filepath.Join(configRoot, "opencode.json"),
				[]byte(content),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			key := mustOpenCodeCorrelationKey(t, "alpha")
			_, err := observeOpenCodePlugins(
				Input{Paths: daempaths.Paths{ManifestRoot: root}},
				[]carrierRecord{{
					key:     key,
					carrier: desiredextension.CarrierOpenCodePlugin,
					scope:   target.ScopeProject,
				}},
			)
			if err == nil {
				t.Fatal("observeOpenCodePlugins succeeded, want refusal")
			}
		})
	}
}

func TestObservePiPackagesUsesScopeSpecificExactSettings(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	agentRoot := filepath.Join(root, "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	if err := os.MkdirAll(filepath.Join(projectRoot, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(projectRoot, ".pi", "settings.json"),
		[]byte(`{"packages":["npm:@acme/pi@1.0.0"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(agentRoot, "settings.json"),
		[]byte(`{"packages":["npm:global-noise"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	key := mustPiCorrelationKey(t, "npm:@acme/pi@1.0.0")
	spec, err := observePiPackages(
		Input{Paths: daempaths.Paths{ManifestRoot: projectRoot}},
		[]carrierRecord{{
			key:     key,
			carrier: desiredextension.CarrierPiPackage,
			scope:   target.ScopeProject,
		}},
	)
	if err != nil {
		t.Fatalf("observePiPackages returned error: %v", err)
	}
	if len(spec.Correlations) != 1 ||
		spec.Correlations[0].Result.State() != relationobserve.StateExactCorrelation {
		t.Fatalf("Pi correlations = %#v, want one exact project row", spec.Correlations)
	}
	if len(spec.AuthorityPaths) != 1 ||
		spec.AuthorityPaths[0].Path() != filepath.Join(projectRoot, ".pi", "settings.json") ||
		spec.AuthorityPaths[0].Scope() != target.ScopeProject {
		t.Fatalf("Pi authority paths = %#v", spec.AuthorityPaths)
	}
}

func TestObserveCodexPluginsUsesExactGlobalConfigRelation(t *testing.T) {
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(
		filepath.Join(codexHome, "config.toml"),
		[]byte("[plugins.\"documents@official\"]\n[plugins.\"documents@private\"]\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	key := mustCodexCorrelationKey(t, "documents@official")
	spec, err := observeCodexPlugins(
		Input{},
		[]carrierRecord{{
			key:     key,
			carrier: desiredextension.CarrierCodexPlugin,
			scope:   target.ScopeGlobal,
		}},
	)
	if err != nil {
		t.Fatalf("observeCodexPlugins: %v", err)
	}
	if len(spec.Correlations) != 1 ||
		spec.Correlations[0].Result.State() != relationobserve.StateExactCorrelation {
		t.Fatalf("Codex correlations = %#v, want exact correlation", spec.Correlations)
	}
	canonicalHome, err := filepath.EvalSymlinks(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.AuthorityPaths) != 1 ||
		spec.AuthorityPaths[0].Path() != filepath.Join(canonicalHome, "config.toml") ||
		spec.AuthorityPaths[0].Scope() != target.ScopeGlobal {
		t.Fatalf("Codex authority paths = %#v", spec.AuthorityPaths)
	}
}

func TestObserveAntigravityPluginsRequiresCompleteHostPair(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configRoot := filepath.Join(home, ".gemini", "config")
	pluginRoot := filepath.Join(configRoot, "plugins", "guidance")
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configRoot, "import_manifest.json"),
		[]byte(`{"imports":[{"name":"guidance","source":"antigravity"}]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pluginRoot, "plugin.json"),
		[]byte(`{"name":"guidance"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	carrier := mustAntigravityCarrierKey(t, "guidance@google")
	key := mustAntigravityCorrelationKey(t, carrier, "guidance")
	spec, err := observeAntigravityCLIPlugins(
		Input{},
		[]carrierRecord{{
			key:            key,
			carrierKey:     carrier,
			carrier:        desiredextension.CarrierAntigravityCLIPlugin,
			scope:          target.ScopeGlobal,
			desiredPresent: true,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Correlations) != 1 ||
		spec.Correlations[0].Result.State() != relationobserve.StateUnkeyedSameSubject {
		t.Fatalf("Antigravity correlations = %#v, want bounded unkeyed evidence", spec.Correlations)
	}
	gotPaths := make(map[string]bool, len(spec.AuthorityPaths))
	for _, path := range spec.AuthorityPaths {
		gotPaths[path.Path()] = true
	}
	for _, path := range []string{
		filepath.Join(configRoot, "import_manifest.json"),
		filepath.Join(pluginRoot, "plugin.json"),
	} {
		if !gotPaths[path] {
			t.Fatalf("Antigravity authority paths = %#v, missing %q", gotPaths, path)
		}
	}

	if err := os.Remove(filepath.Join(pluginRoot, "plugin.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := observeAntigravityCLIPlugins(
		Input{},
		[]carrierRecord{{
			key:            key,
			carrierKey:     carrier,
			carrier:        desiredextension.CarrierAntigravityCLIPlugin,
			scope:          target.ScopeGlobal,
			desiredPresent: true,
		}},
	); err == nil {
		t.Fatal("partial Antigravity import/bundle relation was accepted")
	}
}

func TestObserveAntigravityOpaqueSourceRemainsUnsupported(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	carrier := mustAntigravityCarrierKey(t, "./plugins/guidance")
	key := mustAntigravityCorrelationKey(t, carrier, "./plugins/guidance")
	spec, err := observeAntigravityCLIPlugins(
		Input{},
		[]carrierRecord{{
			key:        key,
			carrierKey: carrier,
			carrier:    desiredextension.CarrierAntigravityCLIPlugin,
			scope:      target.ScopeGlobal,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Correlations) != 1 ||
		spec.Correlations[0].Result.State() != relationobserve.StateUnsupported ||
		len(spec.AuthorityPaths) != 0 {
		t.Fatalf("opaque Antigravity observation = %#v", spec)
	}
}

func mustPiCorrelationKey(t *testing.T, source string) relationobserve.CorrelationKey {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "pi.package-carrier", "test")
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(source)
	if err != nil {
		t.Fatal(err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("host-relation:v1:test")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := relationobserve.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustCodexCorrelationKey(t *testing.T, source string) relationobserve.CorrelationKey {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "codex.plugin-carrier", "test")
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(source)
	if err != nil {
		t.Fatal(err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("host-relation:v1:test")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := relationobserve.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustAntigravityCarrierKey(
	t *testing.T,
	sourceRef string,
) desiredextension.CarrierKey {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		sourceRef,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	return carrier
}

func mustAntigravityCorrelationKey(
	t *testing.T,
	carrier desiredextension.CarrierKey,
	relationKey string,
) relationobserve.CorrelationKey {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"antigravity-cli.plugin-carrier",
		"test",
	)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(relationKey)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.Derive(carrier, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := relationobserve.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustOpenCodeCorrelationKey(t *testing.T, source string) relationobserve.CorrelationKey {
	return mustOpenCodeCorrelationKeyWithSubject(t, "test", source)
}

func mustOpenCodeCorrelationKeyWithSubject(
	t *testing.T,
	subjectName string,
	source string,
) relationobserve.CorrelationKey {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"opencode.plugin-carrier",
		subjectName,
	)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(source)
	if err != nil {
		t.Fatal(err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("host-relation:v1:test")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := relationobserve.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
