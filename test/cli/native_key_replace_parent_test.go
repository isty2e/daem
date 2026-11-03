package cli_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyReplacementAllowsSafeAncestorGrowth(t *testing.T) {
	fixture := newNativeKeyScopeFixture(t, nativeKeyScopes()[0], true)
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), `{"theme":"dark"}`)
	fingerprint := nativeReplacementFingerprint(t, fixture.RecoveryFixture)
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), `{"theme":"dark","extensionPreferences":{"foreign":3}}`)
	if fresh := nativeReplacementFingerprint(t, fixture.RecoveryFixture); fresh != fingerprint {
		t.Fatal("an unrelated sibling creating a traversable parent invalidated consent")
	}
	nativefixture.RunRecoveryCommand(t, fixture.RecoveryFixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
	nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.Destination), `{"extensionPreferences":{"foreign":3,"managed":"after-model"}}`, true)
}
