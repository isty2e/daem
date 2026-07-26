package pipackage_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestReadSettingsCorrelatesOnlyExactManagedPiSource(t *testing.T) {
	root := t.TempDir()
	agentRoot := filepath.Join(root, "agent")
	projectRoot := filepath.Join(root, "project")
	writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{
  "packages": [
    {"source":"npm:@acme/tools@1.0.0","extensions":["extensions/main.ts"]}
  ],
  "apiKey": "must-not-be-observed"
}`)

	inventory := mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot:  agentRoot,
		WorkDir:     projectRoot,
		ProjectRoot: projectRoot,
		Scope:       target.ScopeGlobal,
	})
	result := mustCorrelate(t, "npm:@acme/tools@1.0.0", projectRoot, target.ScopeGlobal, inventory)
	assertCorrelationState(t, result, observerelation.StateExactCorrelation)
	if len(result.Rows()) != 1 || len(result.ManagedKeyRows()) != 1 {
		t.Fatalf("exact correlation row counts = rows:%d keyed:%d, want 1/1", len(result.Rows()), len(result.ManagedKeyRows()))
	}
}

func TestCorrelateBlocksPiEquivalentButTextuallyDifferentSources(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		observed string
	}{
		{
			name:     "npm version",
			expected: "npm:@acme/tools@1.0.0",
			observed: "npm:@acme/tools@2.0.0",
		},
		{
			name:     "git transport and ref",
			expected: "https://github.com/acme/tools.git@v1",
			observed: "git:https://github.com/acme/tools@v2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			agentRoot := filepath.Join(root, "agent")
			projectRoot := filepath.Join(root, "project")
			writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+quoted(test.observed)+`]}`)

			inventory := mustReadSettings(t, observepipackage.SettingsInput{
				ConfigRoot:  agentRoot,
				WorkDir:     projectRoot,
				ProjectRoot: projectRoot,
				Scope:       target.ScopeGlobal,
			})
			result := mustCorrelate(t, test.expected, projectRoot, target.ScopeGlobal, inventory)
			assertCorrelationState(t, result, observerelation.StateUnkeyedSameSubject)
			if len(result.ManagedKeyRows()) != 0 {
				t.Fatalf("equivalent source received exact correlation key: %#v", result.ManagedKeyRows())
			}
		})
	}
}

func TestCorrelateMatchesPiNPMNameRulesWithoutCollapsingMalformedSuffix(t *testing.T) {
	tests := []struct {
		name      string
		expected  string
		observed  string
		wantState observerelation.CorrelationState
	}{
		{
			name:      "unscoped slash name",
			expected:  "npm:vendor/tools@1.0.0",
			observed:  "npm:vendor/tools@2.0.0",
			wantState: observerelation.StateUnkeyedSameSubject,
		},
		{
			name:      "trailing at remains name",
			expected:  "npm:tools@1.0.0",
			observed:  "npm:tools@",
			wantState: observerelation.StateMissing,
		},
		{
			name:      "leading double at remains distinct",
			expected:  "npm:@",
			observed:  "npm:@@2.0.0",
			wantState: observerelation.StateMissing,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			agentRoot := filepath.Join(root, "agent")
			projectRoot := filepath.Join(root, "project")
			writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+quoted(test.observed)+`]}`)
			inventory := mustReadSettings(t, observepipackage.SettingsInput{
				ConfigRoot: agentRoot, WorkDir: projectRoot, ProjectRoot: projectRoot, Scope: target.ScopeGlobal,
			})
			result := mustCorrelate(t, test.expected, projectRoot, target.ScopeGlobal, inventory)
			assertCorrelationState(t, result, test.wantState)
			if len(result.ManagedKeyRows()) != 0 {
				t.Fatal("non-exact npm source received exact correlation key")
			}
		})
	}
}

func TestCorrelateDoesNotCollapseGitSpellingsPiKeepsDistinct(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		observed string
	}{
		{
			name:     "percent encoded path",
			expected: "git:example.com/acme/%74ools@v1",
			observed: "git:example.com/acme/tools@v2",
		},
		{
			name:     "host case",
			expected: "git:EXAMPLE.com/acme/tools@v1",
			observed: "git:example.com/acme/tools@v2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			agentRoot := filepath.Join(root, "agent")
			projectRoot := filepath.Join(root, "project")
			writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+quoted(test.observed)+`]}`)
			inventory := mustReadSettings(t, observepipackage.SettingsInput{
				ConfigRoot: agentRoot, WorkDir: projectRoot, ProjectRoot: projectRoot, Scope: target.ScopeGlobal,
			})
			result := mustCorrelate(t, test.expected, projectRoot, target.ScopeGlobal, inventory)
			assertCorrelationState(t, result, observerelation.StateMissing)
			if len(result.ManagedKeyRows()) != 0 {
				t.Fatal("Pi-distinct git source received exact correlation key")
			}
		})
	}
}

func TestCorrelateTreatsExactPlusEquivalentPiRowsAsShadow(t *testing.T) {
	root := t.TempDir()
	agentRoot := filepath.Join(root, "agent")
	projectRoot := filepath.Join(root, "project")
	writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{
  "packages": ["npm:tools@1.0.0", "npm:tools@2.0.0"]
}`)

	inventory := mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot:  agentRoot,
		WorkDir:     projectRoot,
		ProjectRoot: projectRoot,
		Scope:       target.ScopeGlobal,
	})
	result := mustCorrelate(t, "npm:tools@1.0.0", projectRoot, target.ScopeGlobal, inventory)
	assertCorrelationState(t, result, observerelation.StateSameSubjectShadow)
	if len(result.SameSubjectRows()) != 2 || len(result.ManagedKeyRows()) != 1 {
		t.Fatalf("shadow row counts = same:%d managed:%d, want 2/1", len(result.SameSubjectRows()), len(result.ManagedKeyRows()))
	}
}

func TestCorrelateUsesPiSettingsBaseForLocalSourceIdentity(t *testing.T) {
	root := t.TempDir()
	agentRoot := filepath.Join(root, "home", ".pi", "agent")
	projectRoot := filepath.Join(root, "work", "project")
	sourceRoot := filepath.Join(root, "work", "packages", "tools")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	expectedSource := filepath.Join("..", "packages", "tools")
	storedSource, err := filepath.Rel(agentRoot, sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+quoted(storedSource)+`]}`)

	inventory := mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot:  agentRoot,
		WorkDir:     projectRoot,
		ProjectRoot: projectRoot,
		Scope:       target.ScopeGlobal,
	})
	result := mustCorrelate(t, expectedSource, projectRoot, target.ScopeGlobal, inventory)
	assertCorrelationState(t, result, observerelation.StateExactCorrelation)

	writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+quoted(storedSource+string(os.PathSeparator)+".")+`]}`)
	inventory = mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot:  agentRoot,
		WorkDir:     projectRoot,
		ProjectRoot: projectRoot,
		Scope:       target.ScopeGlobal,
	})
	result = mustCorrelate(t, expectedSource, projectRoot, target.ScopeGlobal, inventory)
	assertCorrelationState(t, result, observerelation.StateUnkeyedSameSubject)
	if len(result.ManagedKeyRows()) != 0 {
		t.Fatal("equivalent local spelling received exact correlation key")
	}
}

func TestCorrelateNormalizesPiLocalFileURLAgainstSettingsBase(t *testing.T) {
	root := t.TempDir()
	agentRoot := filepath.Join(root, "agent")
	projectRoot := filepath.Join(root, "project")
	sourceRoot := filepath.Join(root, "packages", "tools")
	storedSource, err := filepath.Rel(agentRoot, sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[`+quoted(storedSource)+`]}`)

	inventory := mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot: agentRoot, WorkDir: projectRoot, ProjectRoot: projectRoot, Scope: target.ScopeGlobal,
	})
	sourceURLPath := filepath.ToSlash(sourceRoot)
	if !strings.HasPrefix(sourceURLPath, "/") {
		sourceURLPath = "/" + sourceURLPath
	}
	expected := (&url.URL{Scheme: "file", Path: sourceURLPath}).String()
	result := mustCorrelate(t, expected, projectRoot, target.ScopeGlobal, inventory)
	assertCorrelationState(t, result, observerelation.StateExactCorrelation)
}

func TestCorrelateRejectsUnsafePiLocalFileURL(t *testing.T) {
	root := t.TempDir()
	agentRoot := filepath.Join(root, "agent")
	projectRoot := filepath.Join(root, "project")
	writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{"packages":[]}`)
	inventory := mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot: agentRoot, WorkDir: projectRoot, ProjectRoot: projectRoot, Scope: target.ScopeGlobal,
	})

	key := mustCorrelationKey(t, "file://remote.example/package")
	expected, err := observepipackage.NewScopedRelation(key, target.ScopeGlobal, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observepipackage.Correlate(expected, inventory); err == nil ||
		!strings.Contains(err.Error(), "unsupported authority") {
		t.Fatalf("Correlate error = %v, want unsafe file URL rejection", err)
	}
}

func mustCorrelate(
	t *testing.T,
	source string,
	commandRoot string,
	scope target.Scope,
	inventory observepipackage.Inventory,
) observerelation.CorrelationResult {
	t.Helper()
	key := mustCorrelationKey(t, source)
	expected, err := observepipackage.NewScopedRelation(key, scope, commandRoot)
	if err != nil {
		t.Fatalf("NewScopedRelation returned error: %v", err)
	}
	result, err := observepipackage.Correlate(expected, inventory)
	if err != nil {
		t.Fatalf("Correlate returned error: %v", err)
	}
	return result
}

func mustCorrelationKey(t *testing.T, source string) observerelation.CorrelationKey {
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
	key, err := observerelation.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func quoted(value string) string {
	return strconv.Quote(value)
}
