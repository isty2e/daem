package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestStatusJSONCheckCleanMatchesCheckExit(t *testing.T) {
	tests := []struct {
		name       string
		input      statusCheckFixtureInput
		wantClean  bool
		wantErrors bool
	}{
		{
			name:      "clean",
			input:     statusCheckFixtureInput{sourceContent: "managed\n", hostContent: "managed\n", writeLockfile: true},
			wantClean: true,
		},
		{
			name:  "pending without errors",
			input: statusCheckFixtureInput{sourceContent: "new\n", hostContent: "old\n", writeLockfile: true},
		},
		{
			name:       "missing lockfile",
			input:      statusCheckFixtureInput{sourceContent: "managed\n", hostContent: "managed\n"},
			wantErrors: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := writeStatusCheckFixture(t, test.input)
			root := filepath.Dir(fixture.manifestPath)
			paths := []string{
				fixture.manifestPath,
				fixture.lockfilePath,
				filepath.Join(root, ".daem", "state.json"),
				filepath.Join(root, "instructions", "AGENTS.md"),
				filepath.Join(root, "AGENTS.md"),
			}
			before := make(map[string][]byte, len(paths))
			for _, path := range paths {
				content, err := os.ReadFile(path)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				before[path] = content
			}

			for _, jsonOutput := range []bool{true, false} {
				for _, check := range []bool{false, true} {
					t.Run(fmt.Sprintf("json=%t/check=%t", jsonOutput, check), func(t *testing.T) {
						args := []string{"status", "--manifest", fixture.manifestPath}
						mode := "status"
						if check {
							args = append(args, "--check")
							mode = "check"
						}
						if jsonOutput {
							args = append(args, "--json")
						}

						var stdout, stderr bytes.Buffer
						exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
						wantExit := 0
						if check && !test.wantClean {
							wantExit = 1
						}
						if exitCode != wantExit || stderr.Len() != 0 {
							t.Fatalf("exit=%d, want %d; stderr=%q", exitCode, wantExit, stderr.String())
						}
						if !jsonOutput {
							return
						}

						var report struct {
							SchemaVersion int `json:"schema_version"`
							Mode          string
							CheckClean    *bool `json:"check_clean"`
							HasErrors     bool  `json:"has_errors"`
						}
						if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
							t.Fatalf("decode status report: %v", err)
						}
						if report.CheckClean == nil || *report.CheckClean != test.wantClean {
							t.Fatalf("check_clean=%v, want explicit %t", report.CheckClean, test.wantClean)
						}
						if report.SchemaVersion != contractversion.ReconciliationPlanJSON || report.Mode != mode || report.HasErrors != test.wantErrors {
							t.Fatalf("report=%+v, want mode=%s has_errors=%t", report, mode, test.wantErrors)
						}
					})
				}
			}

			for _, path := range paths {
				content, err := os.ReadFile(path)
				previous, existed := before[path]
				if !existed && os.IsNotExist(err) {
					continue
				}
				if err != nil || !existed || !bytes.Equal(content, previous) {
					t.Fatalf("status changed fixture %s; read error=%v", path, err)
				}
			}
		})
	}
}

func TestApplyDryRunJSONDoesNotClaimStatusCheckCleanliness(t *testing.T) {
	fixture := writeStatusCheckFixture(t, statusCheckFixtureInput{
		sourceContent: "new\n",
		hostContent:   "old\n",
		writeLockfile: true,
	})

	for _, manageExisting := range []bool{false, true} {
		t.Run(fmt.Sprintf("manage-existing=%t", manageExisting), func(t *testing.T) {
			args := []string{"apply", "--manifest", fixture.manifestPath, "--dry-run", "--json"}
			if manageExisting {
				args = append(args, "--manage-existing")
			}
			var stdout, stderr bytes.Buffer
			if exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr); exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("dry-run exit=%d stderr=%q", exitCode, stderr.String())
			}

			var report map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if _, present := report["check_clean"]; present {
				t.Fatalf("apply dry-run claims a status assessment: %s", report["check_clean"])
			}
			var version int
			if err := json.Unmarshal(report["schema_version"], &version); err != nil || version != contractversion.ReconciliationPlanJSON {
				t.Fatalf("schema_version=%d, decode error=%v", version, err)
			}
		})
	}

	testkit.AssertFileContent(t, filepath.Join(filepath.Dir(fixture.manifestPath), "AGENTS.md"), "old\n")
}

func TestStatusJSONCheckCleanUsesOnlySelectedTargets(t *testing.T) {
	fixture := writeStatusCheckFixture(t, statusCheckFixtureInput{
		sourceContent: "managed\n",
		hostContent:   "managed\n",
		writeLockfile: true,
	})
	root := filepath.Dir(fixture.manifestPath)
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]

[instructions.project.target.codex]
render_to = "AGENTS.md"

[instructions.secondary]
source = "instructions/AGENTS.md"
targets = ["claude-code"]
`)
	sourceHash := testkit.HashPath(t, filepath.Join(root, "instructions", "AGENTS.md"))
	testkit.WriteLockfile(t, fixture.lockfilePath, testkit.ExactSupplyLockfile(
		t,
		testkit.ExactSupplyFixture{
			Kind: testkit.ExactSupplyInstructions, Name: "project",
			SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: sourceHash,
		},
		testkit.ExactSupplyFixture{
			Kind: testkit.ExactSupplyInstructions, Name: "secondary",
			SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: sourceHash,
			Targets: []target.Target{target.TargetClaudeCode},
		},
	))

	tests := []struct {
		name      string
		target    string
		wantClean bool
	}{
		{name: "all declared targets"},
		{name: "clean selected target", target: "codex", wantClean: true},
		{name: "pending selected target", target: "claude-code"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, check := range []bool{false, true} {
				args := []string{"status", "--manifest", fixture.manifestPath, "--json"}
				if test.target != "" {
					args = append(args, "--target", test.target)
				}
				if check {
					args = append(args, "--check")
				}
				var stdout, stderr bytes.Buffer
				exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
				wantExit := 0
				if check && !test.wantClean {
					wantExit = 1
				}
				if exitCode != wantExit || stderr.Len() != 0 {
					t.Fatalf("check=%t exit=%d, want %d; stderr=%q", check, exitCode, wantExit, stderr.String())
				}

				report := clijson.DecodePlan(t, stdout.Bytes())
				if *report.CheckClean != test.wantClean || report.HasErrors {
					t.Fatalf("check_clean=%t has_errors=%t, want %t without errors", *report.CheckClean, report.HasErrors, test.wantClean)
				}
			}
		})
	}

	testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), "managed\n")
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("status created the pending output or stat failed: %v", err)
	}
}

func TestFailedStatusJSONDoesNotFabricateCheckAssessment(t *testing.T) {
	missingManifest := filepath.Join(t.TempDir(), "missing.toml")
	tests := []struct {
		name string
		args []string
		kind string
		exit int
	}{
		{name: "usage failure", args: []string{"status", "--unknown", "--json"}, kind: "usage", exit: 2},
		{
			name: "missing report",
			args: []string{"status", "--manifest", missingManifest, "--check", "--json"},
			kind: "operation", exit: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if exitCode := testkit.RunVerboseCLI(test.args, &stdout, &stderr); exitCode != test.exit || stderr.Len() != 0 {
				t.Fatalf("exit=%d, want %d; stderr=%q", exitCode, test.exit, stderr.String())
			}
			clijson.DecodeFailure(t, stdout.String(), "status", test.kind)
		})
	}
}
