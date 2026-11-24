package cli_test

import (
	"bytes"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestClaudeHookAndKeyRecoverTogether(t *testing.T) {
	for _, tc := range []struct {
		name, boundary string
		update         bool
	}{
		{"host", "host", false},
		{"state", "state", false},
		{"update with retained hook", "host", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.SetRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(workspace, ".claude", "settings.json")
			testkit.WriteFile(t, workspace, ".claude/settings.json", `{"outside":"before"}`)
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: destination, Paths: paths}
			var beforeModel string
			var beforeHooks []claudeStopHook
			afterHooks := []claudeStopHook{{"first", "echo first"}}
			if tc.update {
				second := claudeStopHook{"second", "echo retained"}
				beforeModel = "original"
				beforeHooks = []claudeStopHook{{"first", "echo original"}, second}
				afterHooks = append(afterHooks, second)
				writeClaudeHookKeyManifest(t, workspace, "project", beforeModel, beforeHooks...)
				nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
				nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
				requireClaudeHookKeyValues(t, testkit.ReadFile(t, destination), beforeModel, beforeHooks...)
			}

			writeClaudeHookKeyManifest(t, workspace, "project", "example", afterHooks...)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, fixture, tc.boundary, "apply", "--yes", "--verbose")
			if code != nativefixture.RecoveryExit || !strings.Contains(stderr, nativefixture.RecoveryReceipt(destination)) {
				t.Fatalf("apply did not reach %s publication: %d %s %s", tc.boundary, code, stdout, stderr)
			}
			nativefixture.RequireRecoveryJournalState(t, fixture, tc.boundary)

			after := testkit.ReadFile(t, destination)
			requireClaudeHookKeyValues(t, after, "example", afterHooks...)
			later := strings.Replace(string(after), `"before"`, `"later"`, 1)
			if tc.boundary == "host" {
				third := strings.Replace(later, afterHooks[len(afterHooks)-1].command, "echo third", 1)
				testkit.WriteFile(t, workspace, ".claude/settings.json", third)
				before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
				code, out, _ := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "recover", "--yes", "--json")
				if code == 0 || clijson.DecodeRecover(t, []byte(out)).Classification != "blocked" || !maps.Equal(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
					t.Fatal("recovery did not preserve the third-party hook edit and its journal", out)
				}
			}

			testkit.WriteFile(t, workspace, ".claude/settings.json", later)
			nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
			result := testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, configdocument.JSON, result, `{"outside":"later"}`, true)
			if tc.boundary == "host" {
				requireClaudeHookKeyValues(t, result, beforeModel, beforeHooks...)
			} else if !bytes.Equal(result, []byte(later)) {
				t.Fatal("finalization rewrote the committed host settings")
			}
		})
	}
}
