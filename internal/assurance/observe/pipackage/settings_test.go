package pipackage_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestReadSettingsKeepsProjectAndGlobalLayersIndependent(t *testing.T) {
	root := t.TempDir()
	agentRoot := filepath.Join(root, "agent")
	projectRoot := filepath.Join(root, "project")
	writeSettings(t, filepath.Join(agentRoot, "settings.json"), `{"packages":["npm:global-only"]}`)
	writeSettings(t, filepath.Join(projectRoot, ".pi", "settings.json"), `{"packages":["npm:project-only"]}`)

	global := mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot: agentRoot, WorkDir: projectRoot, ProjectRoot: projectRoot, Scope: target.ScopeGlobal,
	})
	project := mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot: agentRoot, WorkDir: projectRoot, ProjectRoot: projectRoot, Scope: target.ScopeProject,
	})
	assertCorrelationState(
		t,
		mustCorrelate(t, "npm:project-only", projectRoot, target.ScopeGlobal, global),
		observerelation.StateMissing,
	)
	assertCorrelationState(
		t,
		mustCorrelate(t, "npm:global-only", projectRoot, target.ScopeProject, project),
		observerelation.StateMissing,
	)
}

func TestMissingSettingsIsFreshScopedAbsence(t *testing.T) {
	root := t.TempDir()
	inventory := mustReadSettings(t, observepipackage.SettingsInput{
		ConfigRoot:  filepath.Join(root, "agent"),
		WorkDir:     filepath.Join(root, "project"),
		ProjectRoot: filepath.Join(root, "project"),
		Scope:       target.ScopeGlobal,
	})
	result := mustCorrelate(t, "npm:missing", filepath.Join(root, "project"), target.ScopeGlobal, inventory)
	assertCorrelationState(t, result, observerelation.StateMissing)
	if inventory.SettingsPath() != filepath.Join(root, "agent", "settings.json") {
		t.Fatalf("SettingsPath() = %q", inventory.SettingsPath())
	}
}

func TestReadSettingsRejectsAmbiguousOrUnsafeEvidence(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{name: "duplicate key", content: []byte(`{"packages":[],"packages":[]}`), want: "duplicate object key"},
		{name: "null packages", content: []byte(`{"packages":null}`), want: "must be an array"},
		{name: "scalar packages", content: []byte(`{"packages":1}`), want: "cannot unmarshal"},
		{name: "missing object source", content: []byte(`{"packages":[{"skills":[]}]}`), want: "source is required"},
		{name: "non-string object source", content: []byte(`{"packages":[{"source":1}]}`), want: "must be a string"},
		{name: "untrimmed source", content: []byte(`{"packages":[" npm:tools"]}`), want: "non-empty and trimmed"},
		{name: "invalid utf8", content: []byte{0xff}, want: "not valid UTF-8"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "agent", "settings.json")
			writeSettingsBytes(t, path, test.content)
			_, err := observepipackage.ReadSettings(observepipackage.SettingsInput{
				ConfigRoot: root + string(os.PathSeparator) + "agent",
				WorkDir:    root,
				Scope:      target.ScopeGlobal,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadSettings error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestReadSettingsRejectsSymlinkAndNonRegularPath(t *testing.T) {
	root := t.TempDir()
	realPath := filepath.Join(root, "real.json")
	writeSettings(t, realPath, `{"packages":[]}`)
	agentRoot := filepath.Join(root, "agent")
	if err := os.MkdirAll(agentRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(agentRoot, "settings.json")
	if err := os.Symlink(realPath, settingsPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := observepipackage.ReadSettings(observepipackage.SettingsInput{
		ConfigRoot: agentRoot,
		WorkDir:    root,
		Scope:      target.ScopeGlobal,
	})
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("symlink ReadSettings error = %v", err)
	}

	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(settingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = observepipackage.ReadSettings(observepipackage.SettingsInput{
		ConfigRoot: agentRoot,
		WorkDir:    root,
		Scope:      target.ScopeGlobal,
	})
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory ReadSettings error = %v", err)
	}
}

func TestReadSettingsRejectsOversizedInput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent", "settings.json")
	content := bytes.Repeat([]byte(" "), observepipackage.MaximumSettingsBytes+1)
	writeSettingsBytes(t, path, content)

	_, err := observepipackage.ReadSettings(observepipackage.SettingsInput{
		ConfigRoot: root + string(os.PathSeparator) + "agent",
		WorkDir:    root,
		Scope:      target.ScopeGlobal,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized ReadSettings error = %v", err)
	}
}

func TestInventoryEntriesPreserveStoredSourceAndClassifyLoadIdentity(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	writeSettings(t, settingsPath, `{
  "packages": [
    "npm:@acme/tool@1.2.3",
    "github:acme/tool#v2",
    "../plugins/local"
  ]
}`)

	inventory := mustReadSettings(t, observepipackage.SettingsInput{
		ProjectRoot: root,
		Scope:       target.ScopeProject,
	})
	entries, err := inventory.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("Entries = %#v", entries)
	}
	if entries[0].Source() != "npm:@acme/tool@1.2.3" ||
		entries[0].HostLoadIdentity() != "npm:@acme/tool" {
		t.Fatalf("npm entry = %#v", entries[0])
	}
	if entries[1].Source() != "github:acme/tool#v2" ||
		!strings.HasPrefix(entries[1].HostLoadIdentity(), "git:") {
		t.Fatalf("Git entry = %#v", entries[1])
	}
	localPath, ok := entries[2].LocalIdentity()
	if !ok || localPath != filepath.Join(root, "plugins", "local") {
		t.Fatalf("local identity = %q, %t", localPath, ok)
	}
	if entries[2].HostLoadIdentity() !=
		"local:project:"+filepath.Join(root, "plugins", "local") {
		t.Fatalf("local load identity = %q", entries[2].HostLoadIdentity())
	}
}

func TestInventoryEntriesUseCanonicalPackageGrammarForAliasesAndGitPlusSources(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	writeSettings(t, settingsPath, `{
  "packages": [
    "npm:tools-alias@npm:@acme/tools@1.2.3",
    "git+https://github.com/acme/tools.git#v1",
    "git+ssh://git@github.com/acme/other.git#v2"
  ]
}`)

	inventory := mustReadSettings(t, observepipackage.SettingsInput{
		ProjectRoot: root,
		Scope:       target.ScopeProject,
	})
	entries, err := inventory.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	want := []string{
		"npm:tools-alias",
		"git:github.com/acme/tools",
		"git:github.com/acme/other",
	}
	if len(entries) != len(want) {
		t.Fatalf("Entries = %#v", entries)
	}
	for index := range entries {
		if entries[index].HostLoadIdentity() != want[index] {
			t.Errorf(
				"entry %d load identity = %q, want %q",
				index,
				entries[index].HostLoadIdentity(),
				want[index],
			)
		}
	}
}

func mustReadSettings(t *testing.T, input observepipackage.SettingsInput) observepipackage.Inventory {
	t.Helper()
	inventory, err := observepipackage.ReadSettings(input)
	if err != nil {
		t.Fatalf("ReadSettings returned error: %v", err)
	}
	return inventory
}

func assertCorrelationState(
	t *testing.T,
	result observerelation.CorrelationResult,
	want observerelation.CorrelationState,
) {
	t.Helper()
	if result.State() != want {
		t.Fatalf("correlation state = %q, want %q (reason %q)", result.State(), want, result.Reason())
	}
}

func writeSettings(t *testing.T, path string, content string) {
	t.Helper()
	writeSettingsBytes(t, path, []byte(content))
}

func writeSettingsBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
