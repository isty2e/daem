package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

type nativeKeyScope struct {
	name, target, scope, filename string
	syntax                        configdocument.Syntax
	original, outside, parent     string
	comments                      []string
}

func nativeKeyScopes() []nativeKeyScope {
	return []nativeKeyScope{
		{
			name: "pi-project", target: "pi", scope: "project", filename: "settings.json", syntax: configdocument.JSON,
			original: "{\n  \"theme\": \"dark\",\n  \"extensionPreferences\": {}\n}\n",
			outside:  `{"theme":"dark"}`, parent: "/extensionPreferences",
		},
		{
			name: "pi-global", target: "pi", scope: "global", filename: "settings.json", syntax: configdocument.JSON,
			original: "{\n  \"theme\": \"dark\",\n  \"extensionPreferences\": {}\n}\n",
			outside:  `{"theme":"dark"}`, parent: "/extensionPreferences",
		},
		{
			name: "codex-global", target: "codex", scope: "global", filename: "config.toml", syntax: configdocument.TOML,
			original: nativeRecoveryOutside + "\n# User feature preferences\n[features]\n",
			outside:  nativeRecoveryOutside, parent: "/features",
			comments: []string{"# User preferences\n", "# User theme\n", "# User feature preferences\n"},
		},
	}
}

type nativeKeyScopeFixture struct {
	nativeRecoveryFixture
	selected nativeKeyScope
	existing bool
}

func TestNativeKeyScopeCLILifecycle(t *testing.T) {
	for _, selected := range nativeKeyScopes() {
		for _, existing := range []bool{true, false} {
			t.Run(selected.name+"/"+nativeKeyScopeFileKind(existing), func(t *testing.T) {
				fixture := newNativeKeyScopeFixture(t, selected, existing)
				for _, model := range []string{"after-model", "updated-model"} {
					writeNativeKeyScopeDesired(t, fixture, model)
					runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "lock")
					_, _, stderr := runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes", "--verbose")
					if !strings.Contains(stderr, nativeRecoveryReceipt(fixture.destination)) || !strings.Contains(stderr, nativeRecoveryReceipt(fixture.paths.StatefilePath)) {
						t.Fatal("Apply did not confirm real host and state publications", stderr)
					}
					requireNativeKeyScopeDesired(t, fixture, model)
				}

				addNativeKeyScopeUserEdit(t, fixture)
				testkit.WriteFile(t, filepath.Dir(fixture.manifest), "daem.toml", fmt.Sprintf("version = 1\ntargets = [%q]\n", selected.target))
				runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "lock")
				runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
				content := testkit.ReadFile(t, fixture.destination)
				requireNativeSharedValues(t, selected.syntax, content, nativeKeyScopeSource(selected, "updated-model"), false)
				requireNativeEmptyParent(t, selected.syntax, content, selected.parent)
				requireNativeKeyScopeOutside(t, fixture, content)
				requireNativeKeyScopeUserEdit(t, fixture)
				requireNativeSharedMode(t, fixture.destination, nativeKeyScopeMode(existing))
				requireNativeKeyScopeSettled(t, fixture, false)
			})
		}
	}
}

func newNativeKeyScopeFixture(t *testing.T, selected nativeKeyScope, existing bool) nativeKeyScopeFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setNativeRecoveryEnvironment(t, root)
	for _, variable := range []string{"TMP", "TEMP"} {
		path := filepath.Join(root, strings.ToLower(variable))
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(variable, path)
	}
	workspace := filepath.Join(root, "workspace")
	hostRoot := filepath.Join(workspace, ".pi")
	if selected.scope == "global" {
		variable := "PI_CODING_AGENT_DIR"
		if selected.target == "codex" {
			variable = "CODEX_HOME"
		}
		hostRoot = os.Getenv(variable)
	}
	fixture := nativeKeyScopeFixture{
		nativeRecoveryFixture: nativeRecoveryFixture{
			manifest: filepath.Join(workspace, "daem.toml"), destination: filepath.Join(hostRoot, selected.filename),
			kind: nativeKeyScopeFileKind(existing),
		},
		selected: selected, existing: existing,
	}
	if existing {
		testkit.WriteFile(t, hostRoot, selected.filename, selected.original)
		if err := os.Chmod(fixture.destination, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeNativeKeyScopeDesired(t, fixture, "after-model")
	fixture.paths, err = daempaths.Resolve(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "lock")
	for _, path := range []string{fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath} {
		testkit.AssertPathMissing(t, path)
	}
	if !existing {
		testkit.AssertPathMissing(t, fixture.destination)
	}
	return fixture
}

func nativeKeyScopeFileKind(existing bool) string {
	if existing {
		return "existing"
	}
	return "absent"
}

func nativeKeyScopeMode(existing bool) os.FileMode {
	if existing {
		return 0o644
	}
	return 0o600
}

func nativeKeyScopeSource(selected nativeKeyScope, model string) string {
	if selected.syntax == configdocument.JSON {
		return fmt.Sprintf("{\"extensionPreferences\":{\"managed\":%q}}\n", model)
	}
	return fmt.Sprintf("[features]\nmanaged = %q\n", model)
}

func writeNativeKeyScopeDesired(t *testing.T, fixture nativeKeyScopeFixture, model string) {
	t.Helper()
	selected := fixture.selected
	source := "preferences" + filepath.Ext(selected.filename)
	workspace := filepath.Dir(fixture.manifest)
	sourcePath := "./" + source
	if selected.scope == "global" {
		sourcePath = filepath.Join(workspace, source)
	}
	manifest := fmt.Sprintf(`version = 1
targets = [%q]
[[host_config]]
id = "preferences"
scope = %q
path = %q
source = %q
ownership = "keys"
`, selected.target, selected.scope, selected.filename, sourcePath)
	testkit.WriteFile(t, workspace, "daem.toml", manifest)
	testkit.WriteFile(t, workspace, source, nativeKeyScopeSource(selected, model))
}

func requireNativeKeyScopeOutside(t *testing.T, fixture nativeKeyScopeFixture, content []byte) {
	t.Helper()
	if fixture.existing {
		requireNativeSharedValues(t, fixture.selected.syntax, content, fixture.selected.outside, true)
		for _, comment := range fixture.selected.comments {
			if !bytes.Contains(content, []byte(comment)) {
				t.Fatalf("native key edit lost foreign comment %q:\n%s", comment, content)
			}
		}
	}
}

func requireNativeKeyScopeDesired(t *testing.T, fixture nativeKeyScopeFixture, model string) {
	t.Helper()
	content := testkit.ReadFile(t, fixture.destination)
	requireNativeSharedValues(t, fixture.selected.syntax, content, nativeKeyScopeSource(fixture.selected, model), true)
	requireNativeKeyScopeOutside(t, fixture, content)
	requireNativeSharedMode(t, fixture.destination, nativeKeyScopeMode(fixture.existing))
	requireNativeKeyScopeSettled(t, fixture, true)
}

func requireNativeKeyScopeSettled(t *testing.T, fixture nativeKeyScopeFixture, managed bool) {
	t.Helper()
	requireNativeKeyScopeMetadata(t, fixture, managed)
	_, status, _ := runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "status", "--check", "--json")
	requireNativeLifecycleClean(t, status)
	before := snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)
	runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
	if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)) {
		t.Fatal("repeat Apply changed settled host, state, Registry or recovery evidence")
	}
	testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
}

func requireNativeKeyScopeMetadata(t *testing.T, fixture nativeKeyScopeFixture, managed bool) {
	t.Helper()
	requireNativeLifecycleMetadataForScope(t, fixture.manifest, managed, target.Scope(fixture.selected.scope))
}

func addNativeKeyScopeUserEdit(t *testing.T, fixture nativeKeyScopeFixture) {
	t.Helper()
	content := testkit.ReadFile(t, fixture.destination)
	changed := append([]byte(nativeRecoveryLater), content...)
	if fixture.selected.syntax == configdocument.JSON {
		trimmed := bytes.TrimSpace(content)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			t.Fatal("user edit requires a JSON object")
		}
		changed = append([]byte("{\n  \"recovery_note\": \"keep\","), trimmed[1:]...)
	}
	if err := os.WriteFile(fixture.destination, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	requireNativeKeyScopeUserEdit(t, fixture)
}

func requireNativeKeyScopeUserEdit(t *testing.T, fixture nativeKeyScopeFixture) {
	t.Helper()
	fragment := nativeRecoveryLater
	if fixture.selected.syntax == configdocument.JSON {
		fragment = `{"recovery_note":"keep"}`
	}
	content := testkit.ReadFile(t, fixture.destination)
	requireNativeSharedValues(t, fixture.selected.syntax, content, fragment, true)
	if fixture.selected.syntax == configdocument.TOML && !bytes.Contains(content, []byte(nativeRecoveryLater)) {
		t.Fatal("lost foreign value/comment added after process loss")
	}
}
