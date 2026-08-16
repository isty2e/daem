package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/test/testkit"
)

func TestGlobalOutputOwnershipBlocksForeignManifestUntilOwnerReleases(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	leftRoot := filepath.Join(root, "left")
	rightRoot := filepath.Join(root, "right")
	leftManifest := writeLockedInstructionWorkspace(t, leftRoot, "same\n", true)
	rightManifest := writeLockedInstructionWorkspace(t, rightRoot, "same\n", true)
	destination := filepath.Join(home, ".codex", "AGENTS.md")

	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", leftManifest, "--yes"); exitCode != 0 {
		t.Fatalf("owner apply exit=%d stderr=%q", exitCode, stderr)
	}
	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", rightManifest, "--manage-existing", "--yes"); exitCode == 0 ||
		!strings.Contains(stderr, "ownership conflict") || strings.Contains(stderr, leftManifest) {
		t.Fatalf("foreign manage-existing exit=%d stderr=%q, want path-neutral ownership conflict", exitCode, stderr)
	}
	testkit.AssertFileContent(t, destination, "same\n")
	if _, err := os.Stat(filepath.Join(rightRoot, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("foreign statefile exists or stat failed: %v", err)
	}
	testkit.WriteFile(t, rightRoot, "source.md", "different\n")
	if exitCode, _, stderr := runOwnershipCLI("lock", "--manifest", rightManifest); exitCode != 0 {
		t.Fatalf("foreign relock exit=%d stderr=%q", exitCode, stderr)
	}
	if exitCode, stdout, stderr := runOwnershipCLI("status", "--manifest", rightManifest); exitCode != 0 || stderr != "" ||
		!strings.Contains(stdout, "ownership conflict") || strings.Contains(stdout, leftManifest) {
		t.Fatalf("foreign status exit=%d stdout=%q stderr=%q, want path-neutral ownership conflict", exitCode, stdout, stderr)
	}
	if exitCode, stdout, stderr := runOwnershipCLI("status", "--manifest", rightManifest, "--verbose"); exitCode != 0 || stderr != "" ||
		!strings.Contains(stdout, "ownership_conflict") || !strings.Contains(stdout, leftManifest) {
		t.Fatalf("verbose foreign status exit=%d stdout=%q stderr=%q, want owner-attributed conflict", exitCode, stdout, stderr)
	}
	if exitCode, stdout, stderr := runOwnershipCLI("list", "outputs", "--manifest", rightManifest); exitCode != 0 || stderr != "" ||
		!strings.Contains(stdout, "blocked: 1") || !strings.Contains(stdout, `reason="ownership_conflict"`) || !strings.Contains(stdout, leftManifest) {
		t.Fatalf("foreign inventory exit=%d stdout=%q stderr=%q, want owner-attributed blocked output", exitCode, stdout, stderr)
	}
	if exitCode, stdout, stderr := runOwnershipCLI("list", "outputs", "--manifest", rightManifest, "--json"); exitCode != 0 || stderr != "" {
		t.Fatalf("foreign JSON inventory exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	} else {
		var inventory struct {
			SchemaVersion int `json:"schema_version"`
			BlockedCount  int `json:"blocked_count"`
			Blocked       []struct {
				Reason string `json:"reason"`
				Detail string `json:"detail"`
			} `json:"blocked"`
		}
		if err := json.Unmarshal([]byte(stdout), &inventory); err != nil {
			t.Fatalf("decode blocked inventory: %v", err)
		}
		if inventory.SchemaVersion != contractversion.OutputInventoryJSON || inventory.BlockedCount != 1 || len(inventory.Blocked) != 1 ||
			inventory.Blocked[0].Reason != "ownership_conflict" || !strings.Contains(inventory.Blocked[0].Detail, leftManifest) {
			t.Fatalf("blocked inventory = %#v, want versioned owner-attributed conflict", inventory)
		}
	}
	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", rightManifest, "--yes"); exitCode == 0 ||
		!strings.Contains(stderr, "ownership conflict") || strings.Contains(stderr, leftManifest) {
		t.Fatalf("foreign differing apply exit=%d stderr=%q, want path-neutral ownership conflict", exitCode, stderr)
	}

	testkit.WriteFile(t, leftRoot, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	if exitCode, _, stderr := runOwnershipCLI("lock", "--manifest", leftManifest); exitCode != 0 {
		t.Fatalf("owner relock exit=%d stderr=%q", exitCode, stderr)
	}
	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", leftManifest, "--yes"); exitCode != 0 {
		t.Fatalf("owner release exit=%d stderr=%q", exitCode, stderr)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("released destination exists or stat failed: %v", err)
	}

	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", rightManifest, "--yes"); exitCode != 0 {
		t.Fatalf("successor apply exit=%d stderr=%q", exitCode, stderr)
	}
	testkit.AssertFileContent(t, destination, "different\n")
}

func TestGlobalOutputOwnershipAllowsDisjointAggregateProjections(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	leftManifest := writeGlobalMCPWorkspace(t, filepath.Join(root, "left"), "alpha")
	rightManifest := writeGlobalMCPWorkspace(t, filepath.Join(root, "right"), "beta")
	hostConfig := filepath.Join(home, ".claude.json")

	for _, manifest := range []string{leftManifest, rightManifest} {
		if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", manifest, "--yes"); exitCode != 0 {
			t.Fatalf("apply %s exit=%d stderr=%q", manifest, exitCode, stderr)
		}
	}
	testkit.AssertClaudeGlobalMCPConfigEquivalent(t, hostConfig, "alpha", "npx", []string{"-y", "@example/alpha"}, nil)
	testkit.AssertClaudeGlobalMCPConfigEquivalent(t, hostConfig, "beta", "npx", []string{"-y", "@example/beta"}, nil)

	leftRoot := filepath.Dir(leftManifest)
	testkit.WriteFile(t, leftRoot, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	if exitCode, _, stderr := runOwnershipCLI("lock", "--manifest", leftManifest); exitCode != 0 {
		t.Fatalf("relock left exit=%d stderr=%q", exitCode, stderr)
	}
	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", leftManifest, "--yes"); exitCode != 0 {
		t.Fatalf("remove alpha exit=%d stderr=%q", exitCode, stderr)
	}
	testkit.AssertClaudeGlobalMCPConfigMissing(t, hostConfig, "alpha")
	testkit.AssertClaudeGlobalMCPConfigEquivalent(t, hostConfig, "beta", "npx", []string{"-y", "@example/beta"}, nil)
}

func TestConcurrentDisjointGlobalProjectionsConvergeAfterStaleRetry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	leftManifest := writeGlobalMCPWorkspace(t, filepath.Join(root, "left"), "alpha")
	rightManifest := writeGlobalMCPWorkspace(t, filepath.Join(root, "right"), "beta")
	hostConfig := filepath.Join(home, ".claude.json")

	left, right, leftErr, rightErr := runBlockedPhysicalMutationPair(
		t, hostConfig, "claude-code", "global",
		[]string{"apply", "--manifest", leftManifest, "--yes"},
		[]string{"apply", "--manifest", rightManifest, "--yes"},
	)
	if (leftErr == nil) == (rightErr == nil) {
		t.Fatalf("concurrent disjoint errors=%v/%v stderr=%q/%q, want one current plan and one stale", leftErr, rightErr, left.stderr.String(), right.stderr.String())
	}
	retryManifest := leftManifest
	staleOutput := left.stderr.String()
	if rightErr != nil {
		retryManifest = rightManifest
		staleOutput = right.stderr.String()
	}
	if !strings.Contains(
		staleOutput,
		"apply failed: authoritative inputs changed before apply completed",
	) || strings.Contains(staleOutput, "stale_snapshot") {
		t.Fatalf("losing disjoint apply stderr=%q, want typed stale detail", staleOutput)
	}
	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", retryManifest, "--yes"); exitCode != 0 {
		t.Fatalf("disjoint retry exit=%d stderr=%q", exitCode, stderr)
	}
	testkit.AssertClaudeGlobalMCPConfigEquivalent(t, hostConfig, "alpha", "npx", []string{"-y", "@example/alpha"}, nil)
	testkit.AssertClaudeGlobalMCPConfigEquivalent(t, hostConfig, "beta", "npx", []string{"-y", "@example/beta"}, nil)
}

func TestMovedManifestCannotSilentlyInheritGlobalOwnership(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	originalRoot := filepath.Join(root, "original")
	originalManifest := writeGlobalMCPWorkspace(t, originalRoot, "alpha")
	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", originalManifest, "--yes"); exitCode != 0 {
		t.Fatalf("original apply exit=%d stderr=%q", exitCode, stderr)
	}
	movedRoot := filepath.Join(root, "moved")
	if err := os.Rename(originalRoot, movedRoot); err != nil {
		t.Fatalf("rename owner root returned error: %v", err)
	}
	movedManifest := filepath.Join(movedRoot, "daem.toml")
	if exitCode, stdout, stderr := runOwnershipCLI("status", "--manifest", movedManifest); exitCode != 0 || stderr != "" ||
		!strings.Contains(stdout, "ownership conflict") || strings.Contains(stdout, originalManifest) {
		t.Fatalf("moved status exit=%d stdout=%q stderr=%q, want path-neutral ownership conflict", exitCode, stdout, stderr)
	}
	if exitCode, stdout, stderr := runOwnershipCLI("status", "--manifest", movedManifest, "--verbose"); exitCode != 0 || stderr != "" ||
		!strings.Contains(stdout, "ownership_conflict") || !strings.Contains(stdout, originalManifest) {
		t.Fatalf("verbose moved status exit=%d stdout=%q stderr=%q, want original-owner conflict", exitCode, stdout, stderr)
	}
}

func TestStaleWholePathClaimBlocksForeignProjectionWithoutAutoTransfer(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	ownerRoot := filepath.Join(root, "stale-owner")
	testkit.WriteFile(t, ownerRoot, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	ownerManifest := filepath.Join(ownerRoot, "daem.toml")
	testkit.WriteActiveOwnershipClaim(t, ownerManifest, "~/.claude.json", "")
	if err := os.RemoveAll(ownerRoot); err != nil {
		t.Fatalf("remove stale owner root returned error: %v", err)
	}
	foreignManifest := writeGlobalMCPWorkspace(t, filepath.Join(root, "foreign"), "alpha")
	if exitCode, _, stderr := runOwnershipCLI("apply", "--manifest", foreignManifest, "--yes"); exitCode == 0 ||
		!strings.Contains(stderr, "ownership conflict") || strings.Contains(stderr, ownerManifest) {
		t.Fatalf("foreign projection exit=%d stderr=%q, want path-neutral stale-owner conflict", exitCode, stderr)
	}
}

func writeGlobalMCPWorkspace(t *testing.T, root string, name string) string {
	t.Helper()
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "`+name+`"
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@example/`+name+`"]
`)
	manifestPath := filepath.Join(root, "daem.toml")
	if exitCode, _, stderr := runOwnershipCLI("lock", "--manifest", manifestPath); exitCode != 0 {
		t.Fatalf("lock %s exit=%d stderr=%q", manifestPath, exitCode, stderr)
	}
	return manifestPath
}

func runOwnershipCLI(args ...string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := clipkg.RunWithOptions(args, clipkg.RunOptions{
		Context: context.Background(), Stdout: &stdout, Stderr: &stderr,
	})
	return exitCode, stdout.String(), stderr.String()
}
