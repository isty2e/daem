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
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyReplacementRecomposesAfterDisclosure(t *testing.T) {
	fixture := nativeReplacementRecoveryFixture(t, "managed")
	plan, err := applyworkflow.PlanNativeReplacement(t.Context(), applyworkflow.NativeReplacementInput{ManifestPath: fixture.Manifest, ID: "preferences"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	// A normal editor may replace the inode while preserving selected values and protection.
	temporary := fixture.Destination + ".editor"
	if err := os.WriteFile(temporary, []byte("# Fresh after disclosure\nmodel='external-model'\n"+nativefixture.RecoveryOutside), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, fixture.Destination); err != nil {
		t.Fatal(err)
	}
	if _, err := applyworkflow.ExecuteNativeReplacement(t.Context(), plan, plan.Disclosure().Fingerprint, applyworkflow.ExecuteOptions{}); err != nil {
		t.Fatal("unselected editor changes invalidated consent", err)
	}
	content := testkit.ReadFile(t, fixture.Destination)
	if !strings.Contains(string(content), "# Fresh after disclosure") {
		t.Fatal("replacement replayed stale document bytes")
	}
	nativefixture.RequireSharedValues(t, configdocument.TOML, content, "model='after-model'\n", true)
}

func TestNativeKeyReplacementDoesNotExecuteOtherWork(t *testing.T) {
	fixture := nativefixture.NewRecoveryFixture(t, "mixed")
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), strings.Replace(string(testkit.ReadFile(t, fixture.Destination)), "before-model", "external-model", 1))
	fingerprint := nativeReplacementFingerprint(t, fixture)
	nativefixture.RunRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
	content := testkit.ReadFile(t, fixture.Destination)
	nativefixture.RequireSharedValues(t, configdocument.TOML, content, "model='after-model'\n", true)
	nativefixture.RequireSharedValues(t, configdocument.TOML, content, "[mcp_servers.search]\ncommand='node'\nargs=['old.js']\n", true)
}

func TestNativeKeyReplacementRefusesForeignOwner(t *testing.T) {
	fixture := newNativeKeyScopeFixture(t, nativeKeyScopes()[2], true)
	nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "apply", "--yes")
	other := fixture.RecoveryFixture
	other.Manifest = filepath.Join(filepath.Dir(filepath.Dir(fixture.Manifest)), "other", "daem.toml")
	source := filepath.Join(filepath.Dir(fixture.Manifest), "preferences.toml")
	testkit.WriteFile(t, filepath.Dir(other.Manifest), "daem.toml", "version=1\ntargets=['codex']\n[[host_config]]\nid='preferences'\nscope='global'\npath='config.toml'\nsource="+`"`+source+`"`+"\nownership='keys'\n")
	nativefixture.RunRecoveryCommand(t, other, "none", "lock")
	before := nativefixture.SnapshotRecoveryEvidence(t, fixture.RecoveryFixture)
	if code, _, _ := nativefixture.InvokeRecoveryCommand(t, other, "none", "replace", "host-config", "preferences", "--dry-run", "--json"); code == 0 {
		t.Fatal("replacement acquired another manifest's claim")
	}
	if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, fixture.RecoveryFixture)) {
		t.Fatal("foreign-owner refusal changed evidence")
	}
}

func TestNativeKeyReplacementRefusesScalarAncestor(t *testing.T) {
	fixture := newNativeKeyScopeFixture(t, nativeKeyScopes()[0], true)
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), `{"extensionPreferences":"local scalar","theme":"dark"}`)
	before := nativefixture.SnapshotRecoveryEvidence(t, fixture.RecoveryFixture)
	if code, _, _ := nativefixture.InvokeRecoveryCommand(t, fixture.RecoveryFixture, "none", "replace", "host-config", "preferences", "--dry-run", "--json"); code == 0 {
		t.Fatal("replacement expanded child consent to its scalar ancestor")
	}
	if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, fixture.RecoveryFixture)) {
		t.Fatal("ancestor refusal changed evidence")
	}
}

func TestNativeKeyReplacementLiteralJSONNames(t *testing.T) {
	fixture := newNativeKeyScopeFixture(t, nativeKeyScopes()[0], true)
	workspace := filepath.Dir(fixture.Manifest)
	testkit.WriteFile(t, workspace, "preferences.json", `{"ext":{"a/b":2,"~":true,"":false}}`)
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), `{"ext":{"a/b":0,"~":false,"":true},"foreign":3}`)
	nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "lock")
	_, stdout, _ := nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "replace", "host-config", "preferences", "--dry-run", "--json")
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
	nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", preview.Fingerprint, "--json")
	nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.Destination), `{"ext":{"a/b":2,"~":true,"":false},"foreign":3}`, true)
}
