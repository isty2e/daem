package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeKeyReplacementTargetScopes(t *testing.T) {
	for _, selected := range nativeKeyScopes() {
		for _, existing := range []bool{false, true} {
			t.Run(selected.name+"/"+nativeKeyScopeFileKind(existing), func(t *testing.T) {
				fixture := newNativeKeyScopeFixture(t, selected, existing)
				if existing {
					content := testkit.ReadFile(t, fixture.destination)
					if selected.syntax == configdocument.JSON {
						var values map[string]json.RawMessage
						if err := json.Unmarshal(content, &values); err != nil {
							t.Fatal(err)
						}
						values["extensionPreferences"] = json.RawMessage(`{"managed":"local-model"}`)
						content, _ = json.Marshal(values)
					} else {
						content = append(content, []byte("managed='local-model'\n")...)
					}
					testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), string(content))
				}
				fingerprint := nativeReplacementFingerprint(t, fixture.nativeRecoveryFixture)
				runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint, "--json")
				requireNativeKeyScopeDesired(t, fixture, "after-model")
				fingerprint = nativeReplacementFingerprint(t, fixture.nativeRecoveryFixture)
				before := snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)
				runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
				requireNativeKeyScopeDesired(t, fixture, "after-model")
				for path, image := range before {
					if current := snapshotNativeRecoveryEvidence(t, fixture.nativeRecoveryFixture)[path]; current != image {
						t.Fatal("current replacement changed settled evidence", path)
					}
				}
			})
		}
	}
}

func TestNativeKeyReplacementBindsProtectionAndSource(t *testing.T) {
	for _, change := range []string{"mode", "source", "selected-value", "unknown-version", "undeclared-selector"} {
		t.Run(change, func(t *testing.T) {
			fixture := nativeReplacementRecoveryFixture(t, "managed")
			fingerprint := nativeReplacementFingerprint(t, fixture)
			extra := []string{}
			switch change {
			case "mode":
				if err := os.Chmod(fixture.destination, 0o600); err != nil {
					t.Fatal(err)
				}
			case "source":
				writeNativeRecoveryDesired(t, fixture, "new-source", "old.js")
			case "selected-value":
				testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), "model='other-local'\n"+nativeRecoveryOutside)
			case "unknown-version":
				fingerprint = strings.Replace(fingerprint, "v1:", "v2:", 1)
			case "undeclared-selector":
				extra = append(extra, "--key", "/outside")
			}
			before := snapshotNativeRecoveryEvidence(t, fixture)
			args := append([]string{"replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint}, extra...)
			if code, _, _ := invokeNativeRecoveryCommand(t, fixture, "none", args...); code == 0 {
				t.Fatal("changed replacement authority was accepted")
			}
			for path, image := range before {
				if current := snapshotNativeRecoveryEvidence(t, fixture)[path]; current != image {
					t.Fatal("refused replacement changed evidence", path)
				}
			}
		})
	}
}
