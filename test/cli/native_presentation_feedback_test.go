package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/pelletier/go-toml/v2"
)

func nativePresentationFixture(t *testing.T, source, current string) nativeRecoveryFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	destination := filepath.Join(workspace, ".codex", "config.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "preferences.toml", source)
	testkit.WriteFile(t, filepath.Dir(destination), filepath.Base(destination), current)
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
	return nativeRecoveryFixture{manifest: manifest, destination: destination}
}

func TestNativeReplacementHumanOutcome(t *testing.T) {
	for _, kind := range []string{"replace", "adopt"} {
		t.Run(kind, func(t *testing.T) {
			current := "model='local'\n"
			if kind == "adopt" {
				current = "model='declared'\n"
			}
			fixture := nativePresentationFixture(t, "model='declared'\n", current)
			original, err := os.Stat(fixture.destination)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint := nativeReplacementFingerprint(t, fixture)
			_, stdout, _ := runNativeRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
			outcome := "Applied declared values to 1 host configuration key: model"
			if kind == "adopt" {
				outcome = "Adopted 1 host configuration key without changing its value: model"
			}
			if !strings.Contains(stdout, outcome) || strings.Contains(stdout, "current:") || strings.Contains(stdout, "fingerprint:") {
				t.Fatalf("execution must report its outcome rather than replay the preview: %s", stdout)
			}
			after, err := os.Stat(fixture.destination)
			if err != nil || kind == "adopt" && !os.SameFile(original, after) {
				t.Fatal("adoption must retain the host file", err)
			}
			var values struct{ Model string }
			content := testkit.ReadFile(t, fixture.destination)
			if err := toml.Unmarshal(content, &values); err != nil || values.Model != "declared" {
				t.Fatalf("unexpected declared result: %s %v", content, err)
			}

			fingerprint = nativeReplacementFingerprint(t, fixture)
			_, stdout, _ = runNativeRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
			if !strings.Contains(stdout, "No changes were needed") || strings.Contains(stdout, "Applied declared values") {
				t.Fatalf("a true no-op must not claim another write: %s", stdout)
			}
			stable, err := os.Stat(fixture.destination)
			if err != nil || !os.SameFile(after, stable) {
				t.Fatal("a no-op must not replace the host file", err)
			}
		})
	}
}

func TestNativeReplacementGuidanceCoversBlockedDeclaration(t *testing.T) {
	fixture := nativePresentationFixture(
		t,
		"model='declared'\nretry=true\n[tui]\ntheme='dark'\n",
		"model='local'\nretry=false\n[tui]\ntheme='light'\n",
	)
	before := testkit.ReadFile(t, fixture.destination)
	code, stdout, stderr := runOwnershipCLI("apply", "--manifest", fixture.manifest, "--yes")
	combined := stdout + stderr
	if code == 0 || !bytes.Equal(testkit.ReadFile(t, fixture.destination), before) {
		t.Fatal("ordinary apply must refuse all mismatches without effects")
	}
	if strings.Count(combined, "daem replace host-config preferences") != 1 || strings.Contains(combined, "--key") {
		t.Fatalf("multiple blocked keys need one declaration-wide preview command: %s", combined)
	}
	for _, key := range []string{"model", "retry", "tui.theme"} {
		if !strings.Contains(combined, key) {
			t.Fatalf("blocked key %s was omitted: %s", key, combined)
		}
	}
}
