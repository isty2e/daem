package cli_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/doctorenv"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeStateSupportsOutputInventoryAndDoctor(t *testing.T) {
	fixture := nativefixture.NewPresentation(t, "model='native-test'\n", "foreign=true\n")
	workspace := filepath.Dir(fixture.Manifest)
	testkit.WriteFile(t, workspace, "skills/sample/SKILL.md", "---\nname: sample\ndescription: Sample.\n---\n")
	testkit.WriteFile(t, workspace, "daem.toml", `version=1
targets=['codex']
[[host_config]]
id='preferences'
path='config.toml'
source='./preferences.toml'
ownership='keys'
[[skill]]
name='sample'
source={path='skills/sample',mode='vendor'}
`)
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", fixture.Manifest)
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	doctorenv.WithFakeGit(t, "git version test")

	for _, command := range []string{"outputs", "doctor"} {
		t.Run(command, func(t *testing.T) {
			args := []string{"doctor", "--manifest", fixture.Manifest, "--json"}
			if command == "outputs" {
				args = []string{"list", "outputs", "--manifest", fixture.Manifest, "--json"}
			}
			payload := nativefixture.RunLifecycleCommand(t, args...)
			if command == "outputs" {
				var inventory outputInventoryJSON
				if err := json.Unmarshal([]byte(payload), &inventory); err != nil {
					t.Fatal(err)
				}
				if inventory.SchemaVersion != contractversion.OutputInventoryJSON || inventory.UnmanagedCount != 0 || inventory.BlockedCount != 0 {
					t.Fatalf("clean managed outputs were misclassified: %s", payload)
				}

				testkit.WriteFile(t, workspace, "preferences.toml", "[")
				nativefixture.RunLifecycleCommand(t, args...)
				testkit.WriteFile(t, workspace, "preferences.toml", "model='native-test'\n")
			}
		})
	}
	testkit.AssertNoRecoveryArtifacts(t, workspace)
}
