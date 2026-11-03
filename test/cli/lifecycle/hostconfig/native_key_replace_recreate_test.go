package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyReplacementAdoptionAndRecreation(t *testing.T) {
	for _, kind := range []string{"adopt", "record", "missing-key", "missing-file"} {
		t.Run(kind, func(t *testing.T) {
			base := "existing"
			if kind == "adopt" {
				base = "absent"
			}
			fixture := nativefixture.NewRecoveryFixture(t, base)
			var original os.FileInfo
			switch kind {
			case "adopt", "record":
				testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), "model='after-model'\n"+nativefixture.RecoveryOutside)
				if err := os.Chmod(fixture.Destination, 0o644); err != nil {
					t.Fatal(err)
				}
				var err error
				original, err = os.Stat(fixture.Destination)
				if err != nil {
					t.Fatal(err)
				}
			case "missing-key":
				testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), nativefixture.RecoveryOutside)
			case "missing-file":
				if err := os.Remove(fixture.Destination); err != nil {
					t.Fatal(err)
				}
			}
			fingerprint := nativeReplacementFingerprint(t, fixture)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
			nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "model='after-model'\n", true)
			after, err := os.Stat(fixture.Destination)
			if err != nil {
				t.Fatal(err)
			}
			if (kind == "adopt" || kind == "record") && (!os.SameFile(original, after) || original.Mode().Perm() != after.Mode().Perm()) {
				t.Fatal("exact adoption rewrote the matched host file")
			}
			if kind == "missing-file" && after.Mode().Perm() != 0o600 {
				t.Fatal("recreated document did not receive private permissions")
			}
			_, status, _ := nativefixture.RunRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
			nativefixture.RequireLifecycleClean(t, status)
		})
	}
}
