package cli_test

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestClaudeHookAndKeyRecoverTogether(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			writeClaudeHookKeyManifest(t, workspace, "project", claudeStopHook{"first", "echo first"})
			manifest := filepath.Join(workspace, "daem.toml")
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(workspace, ".claude", "settings.json")
			testkit.WriteFile(t, workspace, ".claude/settings.json", `{"outside":"before"}`)
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: destination, Paths: paths}
			nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
			if code != nativefixture.RecoveryExit || !strings.Contains(stderr, nativefixture.RecoveryReceipt(destination)) {
				t.Fatalf("apply did not reach %s publication: %d %s %s", boundary, code, stdout, stderr)
			}
			nativefixture.RequireRecoveryJournalState(t, fixture, boundary)

			const selected = `{"model":"example","hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo first"}]}]}}`
			after := testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, configdocument.JSON, after, selected, true)
			later := strings.Replace(string(after), `"before"`, `"later"`, 1)
			if boundary == "host" {
				testkit.WriteFile(t, workspace, ".claude/settings.json", strings.Replace(later, "echo first", "echo third", 1))
				before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
				code, out, _ := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "recover", "--yes", "--json")
				if code == 0 || clijson.DecodeRecover(t, []byte(out)).Classification != "blocked" || !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
					t.Fatal("recovery did not preserve the third-party hook edit and its journal", out)
				}
			}

			testkit.WriteFile(t, workspace, ".claude/settings.json", later)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
			result := testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, configdocument.JSON, result, `{"outside":"later"}`, true)
			nativefixture.RequireSharedValues(t, configdocument.JSON, result, selected, boundary == "state")
			if boundary == "state" && !bytes.Equal(result, []byte(later)) {
				t.Fatal("finalization rewrote the committed host settings")
			}
		})
	}
}
