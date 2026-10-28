package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeKeyReplacementAdoptionAndRecreation(t *testing.T) {
	for _, kind := range []string{"adopt", "record", "missing-key", "missing-file"} {
		t.Run(kind, func(t *testing.T) {
			base := "existing"
			if kind == "adopt" {
				base = "absent"
			}
			fixture := newNativeRecoveryFixture(t, base)
			var original os.FileInfo
			switch kind {
			case "adopt", "record":
				testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), "model='after-model'\n"+nativeRecoveryOutside)
				if err := os.Chmod(fixture.destination, 0o644); err != nil {
					t.Fatal(err)
				}
				var err error
				original, err = os.Stat(fixture.destination)
				if err != nil {
					t.Fatal(err)
				}
			case "missing-key":
				testkit.WriteFile(t, filepath.Dir(fixture.destination), filepath.Base(fixture.destination), nativeRecoveryOutside)
			case "missing-file":
				if err := os.Remove(fixture.destination); err != nil {
					t.Fatal(err)
				}
			}
			fingerprint := nativeReplacementFingerprint(t, fixture)
			runNativeRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
			requireNativeSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.destination), "model='after-model'\n", true)
			after, err := os.Stat(fixture.destination)
			if err != nil {
				t.Fatal(err)
			}
			if (kind == "adopt" || kind == "record") && (!os.SameFile(original, after) || original.Mode().Perm() != after.Mode().Perm()) {
				t.Fatal("exact adoption rewrote the matched host file")
			}
			if kind == "missing-file" && after.Mode().Perm() != 0o600 {
				t.Fatal("recreated document did not receive private permissions")
			}
			_, status, _ := runNativeRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
			requireNativeLifecycleClean(t, status)
		})
	}
}
