package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeOrdinaryInstructionApply(t *testing.T) {
	for _, test := range []struct {
		name          string
		ordinaryFirst bool
		missingNative bool
	}{
		{"first apply missing document", false, true},
		{"first apply existing document", false, false},
		{"ordinary before native", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := nativefixture.NewPresentation(t, "model='native-test'\n", "# User preferences\nforeign=true\n")
			workspace := filepath.Dir(fixture.Manifest)
			instruction := filepath.Join(workspace, "AGENTS.md")
			if test.missingNative {
				if err := os.Remove(fixture.Destination); err != nil {
					t.Fatal(err)
				}
			}
			writeManifest := func(native bool) {
				t.Helper()
				content := "version=1\ntargets=['codex']\n[instructions.project]\nsource='./instructions.md'\n"
				if native {
					content += "[[host_config]]\nid='preferences'\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
				}
				testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), content)
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
			}
			testkit.WriteFile(t, workspace, "instructions.md", "instructions version one\n")
			if test.ordinaryFirst {
				writeManifest(false)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
			}
			writeManifest(true)

			for _, version := range []string{"instructions version one\n", "instructions version two\n"} {
				testkit.WriteFile(t, workspace, "instructions.md", version)
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--dry-run")
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
				if !bytes.Contains(testkit.ReadFile(t, instruction), []byte(version)) {
					t.Fatalf("ordinary instruction was omitted from mixed Apply: %s", testkit.ReadFile(t, instruction))
				}
				nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "model='native-test'\n", true)
				if !test.missingNative {
					nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, fixture.Destination), "foreign=true\n", true)
				}
				status := nativefixture.RunLifecycleCommand(t, "status", "--manifest", fixture.Manifest, "--check", "--json")
				nativefixture.RequireLifecycleClean(t, status)
				if plan := clijson.DecodePlan(t, []byte(status)); len(plan.Actions) != 2 {
					t.Fatalf("mixed state lost an ordinary or native subject: %s", status)
				}
				beforeInstruction := testkit.ReadFile(t, instruction)
				beforeNative := testkit.ReadFile(t, fixture.Destination)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
				if !bytes.Equal(beforeInstruction, testkit.ReadFile(t, instruction)) || !bytes.Equal(beforeNative, testkit.ReadFile(t, fixture.Destination)) {
					t.Fatal("repeated mixed Apply rewrote current outputs")
				}
			}
		})
	}
}
