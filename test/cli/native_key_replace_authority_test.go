package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeKeyReplacementRecomposesAfterDisclosure(t *testing.T) {
	fixture := nativeReplacementRecoveryFixture(t, "managed")
	plan, err := applyworkflow.PlanNativeReplacement(t.Context(), applyworkflow.NativeReplacementInput{ManifestPath: fixture.manifest, ID: "preferences"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	// A normal editor may replace the inode while preserving selected values and protection.
	temporary := fixture.destination + ".editor"
	if err := os.WriteFile(temporary, []byte("# Fresh after disclosure\nmodel='external-model'\n"+nativeRecoveryOutside), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, fixture.destination); err != nil {
		t.Fatal(err)
	}
	if _, err := applyworkflow.ExecuteNativeReplacement(t.Context(), plan, plan.Disclosure().Fingerprint, applyworkflow.ExecuteOptions{}); err != nil {
		t.Fatal("unselected editor changes invalidated consent", err)
	}
	content := testkit.ReadFile(t, fixture.destination)
	if !strings.Contains(string(content), "# Fresh after disclosure") {
		t.Fatal("replacement replayed stale document bytes")
	}
	requireNativeSharedValues(t, configdocument.TOML, content, "model='after-model'\n", true)
}

func TestNativeKeyReplacementDoesNotExecuteOtherWork(t *testing.T) {
	fixture := newNativeRecoveryFixture(t, "mixed")
	testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), strings.Replace(string(testkit.ReadFile(t, fixture.destination)), "before-model", "external-model", 1))
	fingerprint := nativeReplacementFingerprint(t, fixture)
	runNativeRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
	content := testkit.ReadFile(t, fixture.destination)
	requireNativeSharedValues(t, configdocument.TOML, content, "model='after-model'\n", true)
	requireNativeSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['old.js']\n", true)
}

func TestNativeKeyReplacementRefusesForeignOwner(t *testing.T) {
	fixture := newNativeKeyScopeFixture(t, nativeKeyScopes()[2], true)
	runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "apply", "--yes")
	other := fixture.nativeRecoveryFixture
	other.manifest = filepath.Join(filepath.Dir(filepath.Dir(fixture.manifest)), "other", "daem.toml")
	source := filepath.Join(filepath.Dir(fixture.manifest), "preferences.toml")
	testkit.WriteFile(t, filepath.Dir(other.manifest), "daem.toml", "version=1\ntargets=['codex']\n[[host_config]]\nid='preferences'\nscope='global'\npath='config.toml'\nsource="+`"`+source+`"`+"\nownership='keys'\n")
	runNativeRecoveryCommand(t, other, "none", "lock")
	before := snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)
	if code, _, _ := invokeNativeRecoveryCommand(t, other, "none", "replace", "host-config", "preferences", "--dry-run", "--json"); code == 0 {
		t.Fatal("replacement acquired another manifest's claim")
	}
	if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)) {
		t.Fatal("foreign-owner refusal changed evidence")
	}
}

func TestNativeKeyReplacementRefusesScalarAncestor(t *testing.T) {
	fixture := newNativeKeyScopeFixture(t, nativeKeyScopes()[0], true)
	testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), `{"extensionPreferences":"local scalar","theme":"dark"}`)
	before := snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)
	if code, _, _ := invokeNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "replace", "host-config", "preferences", "--dry-run", "--json"); code == 0 {
		t.Fatal("replacement expanded child consent to its scalar ancestor")
	}
	if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)) {
		t.Fatal("ancestor refusal changed evidence")
	}
}

func TestNativeKeyReplacementLiteralJSONNames(t *testing.T) {
	fixture := newNativeKeyScopeFixture(t, nativeKeyScopes()[0], true)
	workspace := filepath.Dir(fixture.manifest)
	testkit.WriteFile(t, workspace, "preferences.json", `{"ext":{"a/b":2,"~":true,"":false}}`)
	testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), `{"ext":{"a/b":0,"~":false,"":true},"foreign":3}`)
	runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "lock")
	_, stdout, _ := runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "replace", "host-config", "preferences", "--dry-run", "--json")
	var preview struct {
		Fingerprint string `json:"fingerprint"`
		Keys        []struct {
			Path []string `json:"key_path"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]bool)
	for _, key := range preview.Keys {
		encoded, _ := json.Marshal(key.Path)
		paths[string(encoded)] = true
	}
	for _, path := range []string{`["ext","a/b"]`, `["ext","~"]`, `["ext",""]`} {
		if !paths[path] {
			t.Fatal("literal JSON key was lost", path, stdout)
		}
	}
	runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", preview.Fingerprint, "--json")
	requireNativeSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.destination), `{"ext":{"a/b":2,"~":true,"":false},"foreign":3}`, true)
}
