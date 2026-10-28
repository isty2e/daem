package cli_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeKeyReplacementAllowsSafeAncestorGrowth(t *testing.T) {
	fixture := newNativeKeyScopeFixture(t, nativeKeyScopes()[0], true)
	testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), `{"theme":"dark"}`)
	fingerprint := nativeReplacementFingerprint(t, fixture.nativeRecoveryFixture)
	testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), `{"theme":"dark","extensionPreferences":{"foreign":3}}`)
	if fresh := nativeReplacementFingerprint(t, fixture.nativeRecoveryFixture); fresh != fingerprint {
		t.Fatal("an unrelated sibling creating a traversable parent invalidated consent")
	}
	runNativeRecoveryCommand(t, fixture.nativeRecoveryFixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
	requireNativeSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.destination), `{"extensionPreferences":{"foreign":3,"managed":"after-model"}}`, true)
}
