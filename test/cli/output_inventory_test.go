package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestListOutputsUsesLockedSupplyAndClassifiesAggregateSubjects(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle.\n---\n")
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[[hook]]
name = "review"
event = "Stop"
command = "make review"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI(
		[]string{"lock", "--manifest", manifestPath},
		&stdout,
		&stderr,
	); exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if err := os.RemoveAll(filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, root, ".agents/skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle.\n---\n")
	testkit.WriteFile(t, root, ".codex/hooks.json", `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "make review"
          }
        ]
      }
    ]
  }
}
`)

	stdout.Reset()
	stderr.Reset()
	exitCode := testkit.RunVerboseCLI(
		[]string{"list", "outputs", "--manifest", manifestPath, "--target", "codex", "--json"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("list outputs exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	var inventory outputInventoryJSON
	if err := json.Unmarshal(stdout.Bytes(), &inventory); err != nil {
		t.Fatalf("decode list outputs JSON: %v\n%s", err, stdout.String())
	}
	if inventory.SchemaVersion != 3 || inventory.UnmanagedCount != 2 ||
		len(inventory.Unmanaged) != 2 {
		t.Fatalf("inventory = %#v, want two unmanaged rows", inventory)
	}
	resources := []string{inventory.Unmanaged[0].ResourceID, inventory.Unmanaged[1].ResourceID}
	if !slices.Equal(resources, []string{"skill/oracle", "hook/review"}) {
		t.Fatalf("unmanaged resources = %#v", resources)
	}
	for _, row := range inventory.Unmanaged {
		if row.Subject == "" || len(row.Targets) != 1 || row.Targets[0] != "codex" ||
			row.Scope != "project" || row.Reason != "unmanaged_output_exists" {
			t.Fatalf("unmanaged row = %#v", row)
		}
		if row.ResourceID == "hook/review" && row.Hash != "" {
			t.Fatalf("aggregate inventory fabricated whole-document hash: %#v", row)
		}
		if row.ResourceID == "hook/review" && row.ContentPath != "/hooks" {
			t.Fatalf("aggregate inventory omitted contribution path: %#v", row)
		}
		if row.ResourceID == "skill/oracle" && row.Hash == "" {
			t.Fatalf("managed-path inventory omitted live hash: %#v", row)
		}
	}

	stdout.Reset()
	stderr.Reset()
	statusExit := testkit.RunVerboseCLI(
		[]string{"status", "--manifest", manifestPath, "--target", "codex", "--check"},
		&stdout,
		&stderr,
	)
	if statusExit == 0 {
		t.Fatalf("status unexpectedly ignored missing source: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestListOutputsReportsForeignOwnedAggregateSubject(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	ownerManifest := writeGlobalMCPWorkspace(t, filepath.Join(root, "owner"), "alpha")
	foreignManifest := writeGlobalMCPWorkspace(t, filepath.Join(root, "foreign"), "alpha")
	if exitCode, _, stderr := runOwnershipCLI(
		"apply",
		"--manifest",
		ownerManifest,
		"--yes",
	); exitCode != 0 {
		t.Fatalf("owner apply exit=%d stderr=%q", exitCode, stderr)
	}

	exitCode, stdout, stderr := runOwnershipCLI(
		"list",
		"outputs",
		"--manifest",
		foreignManifest,
		"--target",
		"claude-code",
		"--json",
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("foreign inventory exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	var inventory outputInventoryJSON
	if err := json.Unmarshal([]byte(stdout), &inventory); err != nil {
		t.Fatalf("decode foreign inventory: %v\n%s", err, stdout)
	}
	if inventory.BlockedCount != 1 || len(inventory.Blocked) != 1 {
		t.Fatalf("blocked inventory = %#v, want one aggregate ownership conflict", inventory)
	}
	row := inventory.Blocked[0]
	if row.ResourceID != "" || row.Subject != "projection/claude-code.global.mcp-server/alpha" ||
		!slices.Equal(row.Targets, []string{"claude-code"}) || row.Scope != "global" ||
		row.Path != "~/.claude.json" || row.Hash != "" ||
		row.ContentPath != "/mcpServers/alpha" || row.Reason != "ownership_conflict" {
		t.Fatalf("blocked aggregate row = %#v", row)
	}
}

func TestListOutputsPreservesSubjectsSharingOneAggregatePathAndTargetFilter(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["claude-code", "codex"]

[[mcp_server]]
name = "beta"
targets = ["claude-code"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@example/beta"]

[[mcp_server]]
name = "alpha"
targets = ["claude-code"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@example/alpha"]

[[mcp_server]]
name = "gamma"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@example/gamma"]
`)
	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI(
		[]string{"lock", "--manifest", manifestPath},
		&lockStdout,
		&lockStderr,
	); exitCode != 0 || lockStderr.Len() != 0 {
		t.Fatalf("lock exit=%d stdout=%q stderr=%q", exitCode, lockStdout.String(), lockStderr.String())
	}
	testkit.WriteFile(t, home, ".claude.json", `{
  "mcpServers": {
    "alpha": {"type": "stdio", "command": "npx", "args": ["-y", "@example/alpha"]},
    "beta": {"type": "stdio", "command": "npx", "args": ["-y", "@example/beta"]}
  }
}
`)

	exitCode, stdout, stderr := runOwnershipCLI(
		"list",
		"outputs",
		"--manifest",
		manifestPath,
		"--target",
		"claude-code",
		"--json",
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("list outputs exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	var inventory outputInventoryJSON
	if err := json.Unmarshal([]byte(stdout), &inventory); err != nil {
		t.Fatalf("decode shared aggregate inventory: %v\n%s", err, stdout)
	}
	if inventory.UnmanagedCount != 2 || len(inventory.Unmanaged) != 2 {
		t.Fatalf("shared aggregate inventory = %#v", inventory)
	}
	wantSubjects := []string{
		"projection/claude-code.global.mcp-server/alpha",
		"projection/claude-code.global.mcp-server/beta",
	}
	wantContentPaths := []string{"/mcpServers/alpha", "/mcpServers/beta"}
	for index, wantSubject := range wantSubjects {
		row := inventory.Unmanaged[index]
		if row.ResourceID != "" || row.Subject != wantSubject || row.Path != "~/.claude.json" ||
			!slices.Equal(row.Targets, []string{"claude-code"}) || row.Hash != "" ||
			row.ContentPath != wantContentPaths[index] {
			t.Fatalf("shared aggregate row[%d] = %#v", index, row)
		}
	}
}

type outputInventoryJSON struct {
	SchemaVersion  int                      `json:"schema_version"`
	UnmanagedCount int                      `json:"unmanaged_count"`
	BlockedCount   int                      `json:"blocked_count"`
	Unmanaged      []outputInventoryJSONRow `json:"unmanaged"`
	Blocked        []outputInventoryJSONRow `json:"blocked"`
}

type outputInventoryJSONRow struct {
	ResourceID  string   `json:"resource_id"`
	Subject     string   `json:"subject"`
	Targets     []string `json:"targets"`
	Scope       string   `json:"scope"`
	Path        string   `json:"path"`
	ContentPath string   `json:"content_path"`
	Hash        string   `json:"hash"`
	Reason      string   `json:"reason"`
}
