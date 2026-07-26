package lock

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
)

func TestRunLockWritesPublicMCPManifestAsProjectionSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env = { API_TOKEN = { from_env = "CONTEXT7_API_TOKEN" } }
`)

	dryRun, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("dry-run RunLock returned error: %v", err)
	}
	assertWorkflowMCPSubject(t, dryRun.Lockfile, "context7")
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile exists or stat failed unexpectedly: %v", err)
	}

	written, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("write RunLock returned error: %v", err)
	}
	assertWorkflowMCPSubject(t, written.Lockfile, "context7")
	loaded, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load written lockfile returned error: %v", err)
	}
	assertWorkflowMCPSubject(t, loaded, "context7")
}

func TestRunLockPreservesEmptyMCPArgument(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "empty-argument"
transport = "stdio"
command = "npx"
args = ["-y", "@example/server@1.2.3", "--label", ""]
`)

	written, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	entry := assertWorkflowMCPSubject(t, written.Lockfile, "empty-argument")
	if !slices.Equal(entry.Args, []string{"-y", "@example/server@1.2.3", "--label", ""}) {
		t.Fatalf("written projection args = %#v, want empty argument preserved", entry.Args)
	}

	loaded, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load written lockfile returned error: %v", err)
	}
	entry = assertWorkflowMCPSubject(t, loaded, "empty-argument")
	if !slices.Equal(entry.Args, []string{"-y", "@example/server@1.2.3", "--label", ""}) {
		t.Fatalf("loaded projection args = %#v, want empty argument preserved", entry.Args)
	}
}

func TestRunLockWritesPublicExtensionManifestAsClaudePluginCarrierSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
`)

	dryRun, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("dry-run RunLock returned error: %v", err)
	}
	assertWorkflowClaudePluginCarrierSubject(t, dryRun.Lockfile, "context7-managed", "context7@market")
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile exists or stat failed unexpectedly: %v", err)
	}

	written, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("write RunLock returned error: %v", err)
	}
	assertWorkflowClaudePluginCarrierSubject(t, written.Lockfile, "context7-managed", "context7@market")
	loaded, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load written lockfile returned error: %v", err)
	}
	assertWorkflowClaudePluginCarrierSubject(t, loaded, "context7-managed", "context7@market")
}

func TestRunLockWritesPublicGlobalExtensionManifestAsClaudePluginCarrierSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-global"
carrier = "claude-code-plugin"
scope = "global"
source = { marketplace = "context7@market" }
`)

	dryRun, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("dry-run RunLock returned error: %v", err)
	}
	assertWorkflowClaudePluginCarrierSubjectWithScope(t, dryRun.Lockfile, "context7-global", "context7@market", target.ScopeGlobal)
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile exists or stat failed unexpectedly: %v", err)
	}

	written, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("write RunLock returned error: %v", err)
	}
	assertWorkflowClaudePluginCarrierSubjectWithScope(t, written.Lockfile, "context7-global", "context7@market", target.ScopeGlobal)
	loaded, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load written lockfile returned error: %v", err)
	}
	assertWorkflowClaudePluginCarrierSubjectWithScope(t, loaded, "context7-global", "context7@market", target.ScopeGlobal)
	lockfileContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile written lockfile returned error: %v", err)
	}
	if !strings.Contains(string(lockfileContent), `scope = "global"`) ||
		strings.Contains(string(lockfileContent), `scope = "user"`) {
		t.Fatalf("lockfile content = %s, want public global scope without host user scope", lockfileContent)
	}
}

func TestRunLockRejectsClaudeGlobalExtensionFromDefaults(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["claude-code"]

[defaults]
scope = "global"

[[extension]]
id = "context7-global"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
`)

	_, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err == nil {
		t.Fatal("RunLock returned nil error, want explicit global scope rejection")
	}
	for _, want := range []string{
		"extension[0].scope",
		"requires explicit scope",
		`scope = "global"`,
		"defaults.scope does not authorize this host mutation",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q", err, want)
		}
	}
}

func TestRunLockWritesPublicExtensionManifestAsCodexPluginCarrierSubject(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeWorkflowTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[[extension]]
id = "documents-managed"
carrier = "codex-plugin"
scope = "global"
source = { marketplace = "documents@openai-primary-runtime" }
`)

	dryRun, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("dry-run RunLock returned error: %v", err)
	}
	assertWorkflowCodexPluginCarrierSubject(t, dryRun.Lockfile, "documents-managed", "documents@openai-primary-runtime")
	if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile exists or stat failed unexpectedly: %v", err)
	}

	written, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("write RunLock returned error: %v", err)
	}
	assertWorkflowCodexPluginCarrierSubject(t, written.Lockfile, "documents-managed", "documents@openai-primary-runtime")
	loaded, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("Load written lockfile returned error: %v", err)
	}
	assertWorkflowCodexPluginCarrierSubject(t, loaded, "documents-managed", "documents@openai-primary-runtime")
}
