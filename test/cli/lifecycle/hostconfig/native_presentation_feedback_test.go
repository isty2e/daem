package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
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
			_, stdout, _ := nativefixture.RunRecoveryCommand(t, fixture, "none", "replace", "agent-config", "preferences", "--yes", "--expect-plan", fingerprint)
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
			_, stdout, _ = nativefixture.RunRecoveryCommand(t, fixture, "none", "replace", "agent-config", "preferences", "--yes", "--expect-plan", fingerprint)
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
	if strings.Count(combined, "daem replace agent-config preferences") != 1 || strings.Contains(combined, "--key") {
		t.Fatalf("multiple blocked keys need one declaration-wide preview command: %s", combined)
	}
	for _, key := range []string{"model", "retry", "tui.theme"} {
		if !strings.Contains(combined, key) {
			t.Fatalf("blocked key %s was omitted: %s", key, combined)
		}
	}
}

func TestNativeRemovedDeclarationOffersManagementRelease(t *testing.T) {
	fixture := nativefixture.NewPresentation(t, "model='declared'\n", "unrelated=true\n")
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	const drifted = "model='local'\nunrelated=true\n"
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), drifted)
	nativefixture.RunLifecycleCommand(t, "remove", "agent-config", "preferences", "--manifest", fixture.Manifest)

	for _, command := range [][]string{{"apply", "--dry-run"}, {"status", "--check"}, {"apply", "--yes"}} {
		args := append(command, "--manifest", fixture.Manifest)
		code, stdout, stderr := testkit.RunCapturedCLI(args...)
		if code != 1 || !strings.Contains(stdout+stderr, "daem unmanage agent-config preferences") || strings.Contains(stdout+stderr, "daem replace agent-config") {
			t.Errorf("%v: want release advice and no replacement for a removed declaration, got %d:\n%s%s", command, code, stdout, stderr)
		}
	}
	code, stdout, stderr := testkit.RunCapturedCLI("apply", "--dry-run", "--json", "--manifest", fixture.Manifest)
	if code != 1 || stderr != "" {
		t.Fatalf("want a structured refusal: %d\n%s%s", code, stdout, stderr)
	}
	failure := clijson.DecodeFailure(t, stdout, "apply", "operation")
	var preview []string
	for _, guidance := range failure.Guidance {
		if len(guidance.Argv) < 3 || guidance.Argv[2] != "agent-config" {
			continue
		}
		if guidance.Argv[1] != "unmanage" || preview != nil || !slices.Contains(guidance.Argv, "--dry-run") {
			t.Fatalf("unexpected command guidance: %v", guidance.Argv)
		}
		preview = guidance.Argv[1:]
	}
	if preview == nil {
		t.Fatal("no release preview in the structured refusal")
	}
	nativefixture.RunLifecycleCommand(t, preview...)
	testkit.AssertFileContent(t, fixture.Destination, drifted)

	t.Run("same ID on another target", func(t *testing.T) {
		testkit.WriteFile(t, filepath.Dir(fixture.Manifest), "daem.toml", `version=1
targets=['codex','pi']
[[agent_config]]
id='preferences'
targets=['pi']
path='settings.json'
source='./pi-settings.json'
`)
		testkit.WriteFile(t, filepath.Dir(fixture.Manifest), "pi-settings.json", `{"fixture":true}`)
		nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
		code, stdout, stderr := testkit.RunCapturedCLI("apply", "--target", "codex", "--dry-run", "--manifest", fixture.Manifest)
		if code != 1 || !strings.Contains(stdout+stderr, "drifted output") || strings.Contains(stdout+stderr, "daem unmanage agent-config") || strings.Contains(stdout+stderr, "daem replace agent-config") {
			t.Errorf("target filtering must not turn an existing declaration into a removed one: %d\n%s%s", code, stdout, stderr)
		}
		testkit.WriteFile(t, filepath.Dir(fixture.Manifest), "daem.toml", "version=1\ntargets=['codex']\n")
		nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
	})

	// Manually removing the retired key is already enough to settle key absence.
	testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), "unrelated=true\n")
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	testkit.AssertFileContent(t, fixture.Destination, "unrelated=true\n")
	nativefixture.RequireLifecycleMetadata(t, fixture.Manifest, false)
}

func TestNativePartiallyRemovedDeclarationGuidance(t *testing.T) {
	for _, example := range []struct {
		name, source string
		wantReplace  bool
	}{
		{"mixed drift and retirement", "kept='declared'\n", true},
		{"declared with no keys", "# no keys selected\n", false},
	} {
		t.Run(example.name, func(t *testing.T) {
			fixture := nativefixture.NewPresentation(t, "kept='declared'\nobsolete=true\n", "unrelated=true\n")
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
			const drifted = "kept='local'\nobsolete=false\nunrelated=true\n"
			testkit.WriteFile(t, filepath.Dir(fixture.Destination), filepath.Base(fixture.Destination), drifted)
			testkit.WriteFile(t, filepath.Dir(fixture.Manifest), "preferences.toml", example.source)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)

			code, stdout, stderr := testkit.RunCapturedCLI("apply", "--dry-run", "--manifest", fixture.Manifest)
			combined := stdout + stderr
			if code != 1 || strings.Contains(combined, "daem unmanage agent-config") {
				t.Fatalf("a still-declared resource must not suggest whole-declaration release: %d %s", code, combined)
			}
			if example.wantReplace {
				argv := []string{"replace", "agent-config", "preferences", "--key", "/kept", "--dry-run", "--manifest", fixture.Manifest}
				expected, err := clipresent.ShellCommand(append([]string{"daem"}, argv...)...)
				if err != nil || !strings.Contains(combined, expected) || strings.Count(combined, "daem replace agent-config") != 1 || strings.Contains(combined, "--key /obsolete") {
					t.Fatalf("replacement advice must select only the still-desired key: %v\n%s", err, combined)
				}
				nativefixture.RunLifecycleCommand(t, argv...)
			} else if strings.Contains(combined, "daem replace agent-config") {
				t.Fatalf("there are no desired keys to replace: %s", combined)
			}
			testkit.AssertFileContent(t, fixture.Destination, drifted)
		})
	}
}
