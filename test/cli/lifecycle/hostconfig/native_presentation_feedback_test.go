package cli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
	"github.com/pelletier/go-toml/v2"
)

func TestNativeReplacementHumanOutcome(t *testing.T) {
	for _, kind := range []string{"replace", "adopt"} {
		t.Run(kind, func(t *testing.T) {
			current := "model='local'\n"
			if kind == "adopt" {
				current = "model='declared'\n"
			}
			fixture := nativefixture.NewPresentation(t, "model='declared'\n", current)
			original, err := os.Stat(fixture.Destination)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint := nativeReplacementFingerprint(t, fixture)
			_, stdout, _ := nativefixture.RunRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
			outcome := "Applied declared values to 1 host configuration key: model"
			if kind == "adopt" {
				outcome = "Adopted 1 host configuration key without changing its value: model"
			}
			if !strings.Contains(stdout, outcome) || !strings.Contains(stdout, "preferences") || !strings.Contains(stdout, fixture.Destination) || strings.Contains(stdout, "current:") || strings.Contains(stdout, "fingerprint:") {
				t.Fatalf("execution must report its outcome rather than replay the preview: %s", stdout)
			}
			after, err := os.Stat(fixture.Destination)
			if err != nil || kind == "adopt" && !os.SameFile(original, after) {
				t.Fatal("adoption must retain the host file", err)
			}
			var values struct{ Model string }
			content := testkit.ReadFile(t, fixture.Destination)
			if err := toml.Unmarshal(content, &values); err != nil || values.Model != "declared" {
				t.Fatalf("unexpected declared result: %s %v", content, err)
			}

			fingerprint = nativeReplacementFingerprint(t, fixture)
			_, stdout, _ = nativefixture.RunRecoveryCommand(t, fixture, "none", "replace", "host-config", "preferences", "--yes", "--expect-plan", fingerprint)
			if !strings.Contains(stdout, "No changes were needed") || strings.Contains(stdout, "Applied declared values") {
				t.Fatalf("a true no-op must not claim another write: %s", stdout)
			}
			stable, err := os.Stat(fixture.Destination)
			if err != nil || !os.SameFile(after, stable) {
				t.Fatal("a no-op must not replace the host file", err)
			}
		})
	}
}

func TestNativeReplacementGuidanceCoversBlockedDeclaration(t *testing.T) {
	fixture := nativefixture.NewPresentation(
		t,
		"model='declared'\nretry=true\n[tui]\ntheme='dark'\n",
		"model='local'\nretry=false\n[tui]\ntheme='light'\n",
	)
	before := testkit.ReadFile(t, fixture.Destination)
	code, stdout, stderr := testkit.RunCapturedCLI("apply", "--manifest", fixture.Manifest, "--yes")
	combined := stdout + stderr
	if code == 0 || !bytes.Equal(testkit.ReadFile(t, fixture.Destination), before) {
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
