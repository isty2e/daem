package authoring

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	lockmodel "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestBuildLockfileChangeWouldWriteFromProspectiveManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	originalManifest := "version = 1\ntargets = [\"codex\"]\n"
	writeTestFile(t, tempDir, "daem.toml", originalManifest)
	writeTestFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	prospectiveManifest := originalManifest + `
[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`
	change, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      []byte(prospectiveManifest),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("BuildLockfileChange returned error: %v", err)
	}
	if change.Path() != lockfilePath {
		t.Fatalf("Path = %q, want %q", change.Path(), lockfilePath)
	}
	if change.Status() != LockfileStatusWouldWrite {
		t.Fatalf("Status = %q, want %q", change.Status(), LockfileStatusWouldWrite)
	}

	lockfileContent := string(change.content)
	for _, want := range []string{
		fmt.Sprintf("version = %d", lockmodel.CurrentVersion),
		"[[locked.subject]]",
		`entity_id = "skill:oracle"`,
		`subject_id = "resource/skill/oracle"`,
		`source_id = "local:skills/oracle?mode=vendor"`,
		`content_hash = "` + hashTestPath(t, filepath.Join(tempDir, "skills", "oracle"), artifact.ArtifactKindDirectory) + `"`,
	} {
		if !strings.Contains(lockfileContent, want) {
			t.Fatalf("lockfile content = %q, want %q", lockfileContent, want)
		}
	}
	assertFileContent(t, manifestPath, originalManifest)
	assertPathMissing(t, lockfilePath)
	assertPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestBuildLockfileChangeRejectsFutureLockfileWithoutReplacingIt(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	manifest := "version = 1\ntargets = [\"codex\"]\n"
	futureLockfile := "version = 7\nfuture_payload = \"preserve\"\n"
	writeTestFile(t, tempDir, "daem.toml", manifest)
	writeTestFile(t, tempDir, "daem.lock.toml", futureLockfile)

	_, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:  manifestPath,
		ManifestBytes: []byte(manifest),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported lockfile version 7") {
		t.Fatalf("BuildLockfileChange error = %v, want future-schema rejection", err)
	}
	assertFileContent(t, lockfilePath, futureLockfile)
}

func TestBuildLockfileChangeRejectsMalformedCurrentLockfileWithoutReplacingIt(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	manifest := "version = 1\ntargets = [\"codex\"]\n"
	malformedCurrent := fmt.Sprintf(
		"version = %d\nunknown_current_authority = true\n",
		lockmodel.CurrentVersion,
	)
	writeTestFile(t, tempDir, "daem.toml", manifest)
	writeTestFile(t, tempDir, "daem.lock.toml", malformedCurrent)

	_, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:  manifestPath,
		ManifestBytes: []byte(manifest),
	})
	if err == nil || !strings.Contains(err.Error(), "unknown lockfile key") {
		t.Fatalf("BuildLockfileChange error = %v, want strict current-schema rejection", err)
	}
	assertFileContent(t, lockfilePath, malformedCurrent)
}

func TestBuildLockfileChangePreservesSelectedManifestOrigin(t *testing.T) {
	tempDir := t.TempDir()
	manifestRoot := filepath.Join(tempDir, "config", "daem")
	manifestPath := filepath.Join(manifestRoot, "daem.toml")
	paths := daempaths.Paths{
		ManifestPath:   manifestPath,
		ManifestRoot:   manifestRoot,
		ManifestOrigin: daempaths.ManifestOriginUserDefault,
		LockfilePath:   filepath.Join(manifestRoot, "daem.lock.toml"),
	}
	prospectiveManifest := `version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`

	_, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		Paths:              paths,
		ManifestBytes:      []byte(prospectiveManifest),
		UsePersistentCache: false,
	})
	if err == nil {
		t.Fatal("BuildLockfileChange returned nil error, want project placement rejection")
	}
	for _, want := range []string{
		"invalid prospective manifest",
		"project-scoped skill \"oracle\" requires a project manifest",
		"use --manifest ./daem.toml or set scope = \"global\"",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q", err, want)
		}
	}
}

func TestBuildLockfileChangeIncludesMCPSubjectFromProspectiveManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	originalManifest := "version = 1\ntargets = [\"claude-code\"]\n"
	writeTestFile(t, tempDir, "daem.toml", originalManifest)

	prospectiveManifest := originalManifest + `
[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`
	change, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      []byte(prospectiveManifest),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("BuildLockfileChange returned error: %v", err)
	}
	if change.Path() != lockfilePath {
		t.Fatalf("Path = %q, want %q", change.Path(), lockfilePath)
	}

	lockfileContent := string(change.content)
	for _, want := range []string{
		"[[locked.subject]]",
		`entity_id = "mcp_server:context7"`,
		`subject_id = "projection/claude-code.project.mcp-server/context7"`,
		`on_absent = "remove_binding"`,
		`contribution_cardinality = "exclusive"`,
		`codec_contract = "claude-project-mcp-stdio-v1"`,
		`content_path = "/mcpServers/context7"`,
	} {
		if !strings.Contains(lockfileContent, want) {
			t.Fatalf("lockfile content = %q, want %q", lockfileContent, want)
		}
	}
	assertFileContent(t, manifestPath, originalManifest)
	assertPathMissing(t, lockfilePath)
}

func TestBuildLockfileChangeIncludesOpenCodeMCPSubjectFromProspectiveManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	originalManifest := "version = 1\ntargets = [\"opencode\"]\n"
	writeTestFile(t, tempDir, "daem.toml", originalManifest)

	prospectiveManifest := originalManifest + `
[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`
	change, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      []byte(prospectiveManifest),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("BuildLockfileChange returned error: %v", err)
	}
	if change.Path() != lockfilePath {
		t.Fatalf("Path = %q, want %q", change.Path(), lockfilePath)
	}

	lockfileContent := string(change.content)
	for _, want := range []string{
		"[[locked.subject]]",
		`entity_id = "mcp_server:context7"`,
		`subject_id = "projection/opencode.project.mcp-server/context7"`,
		`on_absent = "remove_binding"`,
		`contribution_cardinality = "exclusive"`,
		`codec_contract = "opencode-project-mcp-local-command-v1"`,
		`aggregate_root = "opencode.json"`,
		`content_path = "/mcp/context7"`,
	} {
		if !strings.Contains(lockfileContent, want) {
			t.Fatalf("lockfile content = %q, want %q", lockfileContent, want)
		}
	}
	assertFileContent(t, manifestPath, originalManifest)
	assertPathMissing(t, lockfilePath)
}

func TestBuildLockfileChangeIncludesExtensionSubjectFromProspectiveManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	originalManifest := "version = 1\ntargets = [\"claude-code\"]\n"
	writeTestFile(t, tempDir, "daem.toml", originalManifest)

	prospectiveManifest := originalManifest + `
[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
`
	change, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      []byte(prospectiveManifest),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("BuildLockfileChange returned error: %v", err)
	}
	if change.Path() != lockfilePath {
		t.Fatalf("Path = %q, want %q", change.Path(), lockfilePath)
	}

	lockfileContent := string(change.content)
	for _, want := range []string{
		"[[locked.subject]]",
		`entity_id = "extension:context7-managed"`,
		`subject_id = "host_relation/claude-code.plugin-carrier/context7-managed"`,
		`on_absent = "block"`,
		`route_contract_version = "claude-plugin-carrier-v1"`,
		`source_namespace = "marketplace:context7@market"`,
		`relation_subject_key = "context7@market"`,
		`route_id = "claude-code.plugin-carrier.install"`,
	} {
		if !strings.Contains(lockfileContent, want) {
			t.Fatalf("lockfile content = %q, want %q", lockfileContent, want)
		}
	}
	assertFileContent(t, manifestPath, originalManifest)
	assertPathMissing(t, lockfilePath)
}

func TestBuildLockfileChangeResolvesProspectivePiLocalCarrierSource(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
	prospectiveManifest := `version = 1
targets = ["pi"]

[[extension]]
id = "tools"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "./packages/../packages/tools" }
`
	change, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      []byte(prospectiveManifest),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("packages", "tools")
	for _, fragment := range []string{
		`source_namespace = "host-source:` + want + `"`,
		`relation_subject_key = "` + want + `"`,
	} {
		if !strings.Contains(string(change.Content()), fragment) {
			t.Fatalf("prospective lockfile = %q, want %q", change.Content(), fragment)
		}
	}
}

func TestBuildLockfileChangeReportsUnchangedLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	writeTestFile(t, tempDir, "instructions/project.md", "project instructions\n")

	firstChange, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      mustReadTestFile(t, manifestPath),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("first BuildLockfileChange returned error: %v", err)
	}
	if err := os.WriteFile(firstChange.Path(), firstChange.content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	secondChange, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      mustReadTestFile(t, manifestPath),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("second BuildLockfileChange returned error: %v", err)
	}
	if secondChange.Status() != LockfileStatusUnchanged {
		t.Fatalf("Status = %q, want %q", secondChange.Status(), LockfileStatusUnchanged)
	}
}

func TestCommitManifestAndLockfilePreservesUnchangedLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	writeTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`)
	writeTestFile(t, tempDir, "instructions/project.md", "project instructions\n")

	firstChange, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      mustReadTestFile(t, manifestPath),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("first BuildLockfileChange returned error: %v", err)
	}
	if err := os.WriteFile(firstChange.Path(), firstChange.content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	change, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      mustReadTestFile(t, manifestPath),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("second BuildLockfileChange returned error: %v", err)
	}
	writtenChange, err := commitManifestAndLockfile(context.Background(), manifestPath, mustReadTestFile(t, manifestPath), change)
	if err != nil {
		t.Fatalf("commitManifestAndLockfile returned error: %v", err)
	}
	if writtenChange.Status() != LockfileStatusUnchanged {
		t.Fatalf("Status = %q, want %q", writtenChange.Status(), LockfileStatusUnchanged)
	}
	assertFileContent(t, change.Path(), string(firstChange.content))
}

func TestCommitManifestAndLockfileWritesTransaction(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	originalManifest := "version = 1\ntargets = [\"codex\"]\n"
	writeTestFile(t, tempDir, "daem.toml", originalManifest)
	writeTestFile(t, tempDir, "instructions/project.md", "project instructions\n")

	prospectiveManifest := originalManifest + `
[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`
	change, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      []byte(prospectiveManifest),
		UsePersistentCache: true,
	})
	if err != nil {
		t.Fatalf("BuildLockfileChange returned error: %v", err)
	}

	writtenChange, err := commitManifestAndLockfile(context.Background(), manifestPath, []byte(prospectiveManifest), change)
	if err != nil {
		t.Fatalf("commitManifestAndLockfile returned error: %v", err)
	}
	if writtenChange.Status() != LockfileStatusWritten {
		t.Fatalf("Status = %q, want %q", writtenChange.Status(), LockfileStatusWritten)
	}
	assertFileContent(t, manifestPath, prospectiveManifest)
	assertFileContent(t, change.Path(), string(change.content))
	assertPathMissing(t, filepath.Join(tempDir, ".daem", "metadata-transaction"))
}

func TestBuildLockfileChangeUsesTemporaryCacheWhenPersistentCacheDisabled(t *testing.T) {
	requireTestGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := initTestGitRepository(t, tempDir)
	writeTestFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	commit := commitTestGitRepository(t, repoPath, "add oracle skill")
	writeTestFile(t, tempDir, "daem.toml", `version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "`+repoPath+`", path = "skills/oracle", ref = "main" }
targets = ["codex"]
`)

	temporaryChange, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      mustReadTestFile(t, manifestPath),
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatalf("temporary BuildLockfileChange returned error: %v", err)
	}
	if !strings.Contains(string(temporaryChange.content), `resolved_ref = "`+commit+`"`) {
		t.Fatalf("temporary lockfile content = %q, want resolved ref %q", temporaryChange.content, commit)
	}
	assertPathMissing(t, filepath.Join(tempDir, ".daem", "cache", "sources"))

	if _, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:       manifestPath,
		ManifestBytes:      mustReadTestFile(t, manifestPath),
		UsePersistentCache: true,
	}); err != nil {
		t.Fatalf("persistent BuildLockfileChange returned error: %v", err)
	}
	assertPathExists(t, filepath.Join(tempDir, ".daem", "cache", "sources"))
}

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q returned error: %v", path, err)
	}
	return content
}

func hashTestPath(t *testing.T, path string, wantKind artifact.ArtifactKind) string {
	t.Helper()

	contentHash, artifactKind, err := access.HashPath(context.Background(), path)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if artifactKind != wantKind {
		t.Fatalf("artifactKind = %q, want %q", artifactKind, wantKind)
	}
	return string(contentHash)
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q returned error: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content of %q = %q, want %q", path, string(content), want)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists or stat failed unexpectedly: %v", path, err)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat %q returned error: %v", path, err)
	}
}

func requireTestGit(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git executable is unavailable: %v", err)
	}
}

func initTestGitRepository(t *testing.T, root string) string {
	t.Helper()

	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	runTestGit(t, repoPath, "init")
	runTestGit(t, repoPath, "checkout", "-b", "main")
	runTestGit(t, repoPath, "config", "user.email", "daem@example.invalid")
	runTestGit(t, repoPath, "config", "user.Name", "Agent Env Test")

	return repoPath
}

func commitTestGitRepository(t *testing.T, repoPath string, message string) string {
	t.Helper()

	runTestGit(t, repoPath, "add", ".")
	runTestGit(t, repoPath, "commit", "-m", message)

	return strings.TrimSpace(runTestGit(t, repoPath, "rev-parse", "HEAD"))
}

func runTestGit(t *testing.T, repoPath string, args ...string) string {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = repoPath
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}

	return string(output)
}
