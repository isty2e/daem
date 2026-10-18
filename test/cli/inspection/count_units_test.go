package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestHumanPlanCountsOutputsRatherThanDeclarationsOrDocuments(t *testing.T) {
	for _, test := range []struct {
		name       string
		manifest   string
		resources  int
		outputs    int
		documents  int
		countLabel string
	}{
		{
			name: "one declaration with two destinations",
			manifest: `version = 1
targets = ["codex", "claude-code"]
[instructions.project]
source = "instructions.md"
`,
			resources: 1, outputs: 2, documents: 2, countLabel: "2 output entries",
		},
		{
			name: "three targets sharing one path",
			manifest: `version = 1
targets = ["opencode", "pi", "antigravity-cli"]
[instructions.project]
source = "instructions.md"
`,
			resources: 1, outputs: 1, documents: 1, countLabel: "1 output entry",
		},
		{
			name: "two contributions in one document",
			manifest: `version = 1
targets = ["claude-code"]
[[hook]]
name = "format"
event = "PostToolUse"
matcher = "Edit"
command = "make fmt"
[[hook]]
name = "test"
event = "Stop"
command = "make test"
`,
			resources: 2, outputs: 2, documents: 1, countLabel: "2 output entries",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			testkit.WithWorkingDirectory(t, root)
			testkit.WriteFile(t, root, "instructions.md", "count fixture\n")
			testkit.WriteFile(t, root, "daem.toml", test.manifest)
			manifest := filepath.Join(root, "daem.toml")
			runCountUnitCLI(t, manifest, "lock")
			locked, err := os.ReadFile(filepath.Join(root, "daem.lock.toml"))
			if err != nil {
				t.Fatal(err)
			}

			var resources struct {
				Count int `json:"resource_count"`
			}
			if err := json.Unmarshal(runCountUnitCLI(t, manifest, "list", "resources", "--json"), &resources); err != nil {
				t.Fatal(err)
			}
			if resources.Count != test.resources {
				t.Fatalf("declaration count = %d, want %d", resources.Count, test.resources)
			}

			var lock struct {
				Entries struct {
					Subjects *int `json:"subjects"`
				} `json:"entry_counts"`
			}
			if err := json.Unmarshal(runCountUnitCLI(t, manifest, "lock", "--dry-run", "--json"), &lock); err != nil {
				t.Fatal(err)
			}
			if lock.Entries.Subjects == nil {
				t.Fatal("lock JSON omits its entry count")
			}
			unit := "lock entries"
			if *lock.Entries.Subjects == 1 {
				unit = "lock entry"
			}
			outdated := runCountUnitCLI(t, manifest, "outdated")
			if want := fmt.Sprintf("(%d %s)\n", *lock.Entries.Subjects, unit); !bytes.HasSuffix(outdated, []byte(want)) {
				t.Fatalf("outdated output = %q, want canonical lock count %q", outdated, want)
			}

			before := runCountUnitCLI(t, manifest, "status", "--json")
			var plan struct {
				Count   int `json:"action_count"`
				Actions []struct {
					Destination string `json:"destination"`
				} `json:"actions"`
			}
			if err := json.Unmarshal(before, &plan); err != nil {
				t.Fatal(err)
			}
			destinations := make(map[string]bool)
			for _, action := range plan.Actions {
				destinations[action.Destination] = true
			}
			if plan.Count != test.outputs || len(plan.Actions) != test.outputs || len(destinations) != test.documents {
				t.Fatalf("JSON outputs=%d rows=%d destinations=%d, want %d/%d/%d", plan.Count, len(plan.Actions), len(destinations), test.outputs, test.outputs, test.documents)
			}

			for _, invocation := range []struct {
				args   []string
				prefix string
			}{
				{args: []string{"status"}, prefix: "status"},
				{args: []string{"apply", "--dry-run"}, prefix: "dry-run"},
			} {
				for _, verbose := range []bool{false, true} {
					args := append([]string(nil), invocation.args...)
					if verbose {
						args = append(args, "--verbose")
					}
					output := runCountUnitCLI(t, manifest, args...)
					want := invocation.prefix + ": " + test.countLabel
					found := false
					for line := range strings.Lines(string(output)) {
						line = strings.TrimSuffix(line, "\n")
						if line == want || strings.HasPrefix(line, want+" (") {
							found = true
						}
					}
					if !found {
						t.Fatalf("%v verbose=%t output = %q, want projection count %q", invocation.args, verbose, output, want)
					}
				}
			}
			if after := runCountUnitCLI(t, manifest, "status", "--json"); !bytes.Equal(before, after) {
				t.Fatalf("human inspection changed JSON result:\nbefore=%s\nafter=%s", before, after)
			}
			if after, err := os.ReadFile(filepath.Join(root, "daem.lock.toml")); err != nil || !bytes.Equal(locked, after) {
				t.Fatalf("inspection changed lockfile, error=%v", err)
			}
			for _, relative := range []string{"AGENTS.md", "CLAUDE.md", ".claude/settings.json", ".daem/state.json"} {
				if _, err := os.Stat(filepath.Join(root, relative)); !os.IsNotExist(err) {
					t.Fatalf("inspection wrote %s or failed to inspect it: %v", relative, err)
				}
			}
		})
	}
}

func TestHumanAppliedCountRetainsActionUnit(t *testing.T) {
	for _, test := range []struct {
		name     string
		targets  string
		resource bool
		want     string
		paths    []string
	}{
		{name: "zero", targets: `["codex"]`, want: "applied: 0 actions"},
		{name: "one", targets: `["codex"]`, resource: true, want: "applied: 1 action", paths: []string{"AGENTS.md"}},
		{name: "two", targets: `["codex", "claude-code"]`, resource: true, want: "applied: 2 actions", paths: []string{"AGENTS.md", "CLAUDE.md"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			testkit.WithWorkingDirectory(t, root)
			content := "applied count fixture\n"
			testkit.WriteFile(t, root, "instructions.md", content)
			manifestContent := "version = 1\ntargets = " + test.targets + "\n"
			if test.resource {
				manifestContent += "[instructions.project]\nsource = \"instructions.md\"\n"
			}
			testkit.WriteFile(t, root, "daem.toml", manifestContent)
			manifest := filepath.Join(root, "daem.toml")
			runCountUnitCLI(t, manifest, "lock")
			output := runCountUnitCLI(t, manifest, "apply", "--yes")
			found := false
			for line := range strings.Lines(string(output)) {
				if strings.TrimSuffix(line, "\n") == test.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("apply output = %q, want exact action count line %q", output, test.want)
			}
			for _, relative := range test.paths {
				testkit.AssertFileContent(t, filepath.Join(root, relative), content)
			}
		})
	}
}

func runCountUnitCLI(t *testing.T, manifest string, args ...string) []byte {
	t.Helper()
	args = append(append([]string(nil), args...), "--manifest", manifest)
	var output bytes.Buffer
	var diagnostic bytes.Buffer
	if code := testkit.RunCLI(args, &output, &diagnostic); code != 0 || diagnostic.Len() != 0 {
		t.Fatalf("%v: code=%d stderr=%q stdout=%q", args, code, diagnostic.String(), output.String())
	}
	return bytes.Clone(output.Bytes())
}
