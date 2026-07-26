package authoring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestAuthoringUseCasesCoverResourceFamilies(t *testing.T) {
	type resourceCase struct {
		name  string
		setup func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error))
	}

	resources := []resourceCase{
		{
			name: "add skill",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := "version = 1\ntargets = [\"codex\"]\n"
				writeTestFile(t, root, "daem.toml", original)
				writeTestFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return AddSkill(context.Background(), options, AddSkillRequest{
						SourceArg: filepath.Join(root, "skills", "oracle"),
						Targets:   []string{"codex"},
					})
				}
			},
		},
		{
			name: "remove skill",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := `version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`
				writeTestFile(t, root, "daem.toml", original)
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return RemoveSkill(context.Background(), options, RemoveSkillRequest{ResourceKey: "oracle"})
				}
			},
		},
		{
			name: "add instruction",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := "version = 1\ntargets = [\"codex\"]\n"
				writeTestFile(t, root, "daem.toml", original)
				writeTestFile(t, root, "AGENTS.md", "Project guidance.\n")
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return AddInstruction(context.Background(), options, AddInstructionRequest{
						Name:      "project",
						SourceArg: filepath.Join(root, "AGENTS.md"),
						Targets:   []string{"codex"},
					})
				}
			},
		},
		{
			name: "remove instruction",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := `version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]
`
				writeTestFile(t, root, "daem.toml", original)
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return RemoveInstruction(context.Background(), options, RemoveInstructionRequest{ResourceName: "project"})
				}
			},
		},
		{
			name: "add hook",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := "version = 1\ntargets = [\"codex\"]\n"
				writeTestFile(t, root, "daem.toml", original)
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return AddHook(context.Background(), options, AddHookRequest{
						Name:    "protect-env",
						Event:   "PreToolUse",
						Command: "python3 hooks/protect.py",
						Targets: []string{"codex"},
					})
				}
			},
		},
		{
			name: "add mcp server",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := "version = 1\ntargets = [\"opencode\"]\n"
				writeTestFile(t, root, "daem.toml", original)
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return AddMCPServer(context.Background(), options, AddMCPServerRequest{
						Name:    "context7",
						Command: "npx",
						Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
						Targets: []string{"opencode"},
					})
				}
			},
		},
		{
			name: "remove mcp server",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := `version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`
				writeTestFile(t, root, "daem.toml", original)
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return RemoveMCPServer(context.Background(), options, RemoveMCPServerRequest{
						Name:    "context7",
						Targets: []string{"opencode"},
					})
				}
			},
		},
		{
			name: "remove hook",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := `version = 1
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "python3 hooks/protect.py"
targets = ["codex"]
`
				writeTestFile(t, root, "daem.toml", original)
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return RemoveHook(context.Background(), options, RemoveHookRequest{ResourceName: "protect-env"})
				}
			},
		},
		{
			name: "add skill_group",
			setup: func(t *testing.T, root string) (string, func(ExecutionOptions) (OperationResult, error)) {
				original := "version = 1\ntargets = [\"codex\"]\n"
				writeTestFile(t, root, "daem.toml", original)
				writeTestFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
				return original, func(options ExecutionOptions) (OperationResult, error) {
					return AddSkillGroup(context.Background(), options, AddSkillGroupRequest{
						SourceArg: filepath.Join(root, "skills"),
						Names:     []string{"oracle"},
						Targets:   []string{"codex"},
					})
				}
			},
		},
	}

	for _, resource := range resources {
		for _, mode := range []AuthoringMode{AuthoringModeDryRun, AuthoringModeWrite} {
			t.Run(string(mode)+" "+resource.name, func(t *testing.T) {
				root := t.TempDir()
				manifestPath := filepath.Join(root, "daem.toml")
				lockfilePath := filepath.Join(root, "daem.lock.toml")
				original, run := resource.setup(t, root)

				result, err := run(ExecutionOptions{
					ManifestPath: manifestPath,
					Mode:         mode,
				})
				if err != nil {
					t.Fatalf("operation returned error: %v", err)
				}
				if result.Mode != mode {
					t.Fatalf("Mode = %q, want %q", result.Mode, mode)
				}
				if result.ManifestPath != manifestPath {
					t.Fatalf("ManifestPath = %q, want %q", result.ManifestPath, manifestPath)
				}
				if result.ResourceID == "" {
					t.Fatal("ResourceID is empty")
				}
				if result.ChangeKind == "" {
					t.Fatal("ChangeKind is empty")
				}
				if result.Lockfile.Path() != lockfilePath {
					t.Fatalf("lockfile path = %q, want %q", result.Lockfile.Path(), lockfilePath)
				}

				switch mode {
				case AuthoringModeDryRun:
					if result.Lockfile.Status() != LockfileStatusWouldWrite {
						t.Fatalf("lockfile status = %q, want %q", result.Lockfile.Status(), LockfileStatusWouldWrite)
					}
					assertFileContent(t, manifestPath, original)
					assertPathMissing(t, lockfilePath)
				case AuthoringModeWrite:
					if result.Lockfile.Status() != LockfileStatusWritten {
						t.Fatalf("lockfile status = %q, want %q", result.Lockfile.Status(), LockfileStatusWritten)
					}
					written, err := os.ReadFile(manifestPath)
					if err != nil {
						t.Fatalf("ReadFile returned error: %v", err)
					}
					if string(written) == original {
						t.Fatalf("manifest was not changed for %s", resource.name)
					}
					assertPathExists(t, lockfilePath)
				}
			})
		}
	}
}

func TestAuthoringOperationModesSelectSourceCachePersistence(t *testing.T) {
	requireTestGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := initTestGitRepository(t, tempDir)
	writeTestFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	commitTestGitRepository(t, repoPath, "add oracle skill")
	writeTestFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	request := AddSkillRequest{
		SourceArg:  repoPath,
		SourcePath: "skills/oracle",
		Ref:        "main",
		Targets:    []string{"codex"},
	}
	if _, err := AddSkill(context.Background(), ExecutionOptions{
		ManifestPath: manifestPath,
		Mode:         AuthoringModeDryRun,
	}, request); err != nil {
		t.Fatalf("dry-run AddSkill returned error: %v", err)
	}
	assertPathMissing(t, filepath.Join(tempDir, ".daem", "cache", "sources"))

	if _, err := AddSkill(context.Background(), ExecutionOptions{
		ManifestPath: manifestPath,
		Mode:         AuthoringModeWrite,
	}, request); err != nil {
		t.Fatalf("write AddSkill returned error: %v", err)
	}
	assertPathExists(t, filepath.Join(tempDir, ".daem", "cache", "sources"))
}

func TestAuthoringWriteDoesNotRediscoverManifestAfterOptimisticSelection(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)

	var configRoot string
	if runtime.GOOS == "windows" {
		configRoot = filepath.Join(root, "appdata", "roaming")
		t.Setenv("APPDATA", configRoot)
		t.Setenv("LOCALAPPDATA", filepath.Join(root, "appdata", "local"))
	} else {
		configRoot = filepath.Join(root, "config")
		t.Setenv("XDG_CONFIG_HOME", configRoot)
		t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	}

	defaultManifestPath := filepath.Join(configRoot, "daem", "daem.toml")
	defaultOriginal := "version = 1\ntargets = [\"codex\"]\n"
	cwdOriginal := "version = 1\ntargets = [\"claude-code\"]\n"
	writeTestFile(t, filepath.Dir(defaultManifestPath), filepath.Base(defaultManifestPath), defaultOriginal)

	buildCalls := 0
	build := func(document ManifestDocument) (Change, error) {
		buildCalls++
		if buildCalls == 1 {
			writeTestFile(t, workspace, "daem.toml", cwdOriginal)
		}
		return Change{
			ManifestPath: document.Path,
			Original:     append([]byte(nil), document.Content...),
			Content:      append(append([]byte(nil), document.Content...), []byte("# authored\n")...),
			ResourceID:   "test",
			ChangeKind:   "test",
		}, nil
	}

	result, err := executeAuthoringOperation(t.Context(), ExecutionOptions{Mode: AuthoringModeWrite}, build)
	if err != nil {
		t.Fatalf("executeAuthoringOperation returned error: %v", err)
	}
	if buildCalls != 2 {
		t.Fatalf("build calls = %d, want optimistic and post-lease reload", buildCalls)
	}
	if result.ManifestPath != defaultManifestPath {
		t.Fatalf("manifest path = %q, want initially selected %q", result.ManifestPath, defaultManifestPath)
	}
	assertFileContent(t, defaultManifestPath, defaultOriginal+"# authored\n")
	assertFileContent(t, filepath.Join(workspace, "daem.toml"), cwdOriginal)
}

func TestAuthoringOperationLockFailureLeavesFilesUnchanged(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	original := `version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/missing-review", mode = "vendor" }
targets = ["codex"]
`
	writeTestFile(t, tempDir, "daem.toml", original)
	writeTestFile(t, tempDir, "daem.lock.toml", "lock stays\n")

	_, err := RemoveSkill(context.Background(), ExecutionOptions{
		ManifestPath: manifestPath,
		Mode:         AuthoringModeWrite,
	}, RemoveSkillRequest{ResourceKey: "oracle"})
	if err == nil {
		t.Fatal("RemoveSkill returned nil error")
	}
	var operationErr OperationError
	if !errors.As(err, &operationErr) {
		t.Fatalf("err = %T, want OperationError", err)
	}
	if operationErr.Phase != OperationPhaseBuildLockfile {
		t.Fatalf("phase = %q, want %q", operationErr.Phase, OperationPhaseBuildLockfile)
	}
	if !strings.Contains(err.Error(), "lock prospective manifest") {
		t.Fatalf("err = %v, want lock prospective manifest diagnostic", err)
	}
	assertFileContent(t, manifestPath, original)
	assertFileContent(t, lockfilePath, "lock stays\n")
	assertNoApplyStateOrJournal(t, manifestPath)
}

func TestOpenCodeMCPAuthoringSourceResolutionLockFailureLeavesFilesUnchanged(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	original := `version = 1
targets = ["codex", "opencode"]

[[skill]]
name = "review"
source = { path = "skills/missing-review", mode = "vendor" }
targets = ["codex"]
`
	writeTestFile(t, tempDir, "daem.toml", original)
	writeTestFile(t, tempDir, "daem.lock.toml", "lock stays\n")

	_, err := AddMCPServer(context.Background(), ExecutionOptions{
		ManifestPath: manifestPath,
		Mode:         AuthoringModeWrite,
	}, AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Targets: []string{"opencode"},
	})
	if err == nil {
		t.Fatal("AddMCPServer returned nil error")
	}
	var operationErr OperationError
	if !errors.As(err, &operationErr) {
		t.Fatalf("err = %T, want OperationError", err)
	}
	if operationErr.Phase != OperationPhaseBuildLockfile {
		t.Fatalf("phase = %q, want %q", operationErr.Phase, OperationPhaseBuildLockfile)
	}
	for _, want := range []string{
		"lock prospective manifest",
		"missing-review",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want %q", err, want)
		}
	}
	assertFileContent(t, manifestPath, original)
	assertFileContent(t, lockfilePath, "lock stays\n")
	assertNoApplyStateOrJournal(t, manifestPath)
}

func TestAuthoringWritePreservesUnchangedLockfileStatus(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `version = 1
targets = ["codex"]

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]
`
	writeTestFile(t, tempDir, "daem.toml", original)

	document := ManifestDocument{
		Path:    manifestPath,
		Root:    tempDir,
		Content: []byte(original),
	}
	change, err := BuildRemoveInstructionChange(document, RemoveInstructionRequest{ResourceName: "project"})
	if err != nil {
		t.Fatalf("BuildRemoveInstructionChange returned error: %v", err)
	}
	unchangedLockfile, err := BuildLockfileChange(context.Background(), LockfileChangeInput{
		ManifestPath:  manifestPath,
		ManifestBytes: change.Content,
	})
	if err != nil {
		t.Fatalf("BuildLockfileChange returned error: %v", err)
	}
	if err := os.WriteFile(unchangedLockfile.Path(), unchangedLockfile.content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	result, err := RemoveInstruction(context.Background(), ExecutionOptions{
		ManifestPath: manifestPath,
		Mode:         AuthoringModeWrite,
	}, RemoveInstructionRequest{ResourceName: "project"})
	if err != nil {
		t.Fatalf("RemoveInstruction returned error: %v", err)
	}
	if result.Lockfile.Status() != LockfileStatusUnchanged {
		t.Fatalf("lockfile status = %q, want %q", result.Lockfile.Status(), LockfileStatusUnchanged)
	}
	assertFileContent(t, manifestPath, string(change.Content))
	assertFileContent(t, unchangedLockfile.Path(), string(unchangedLockfile.content))
}

func assertNoApplyStateOrJournal(t *testing.T, manifestPath string) {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.StatefilePath, paths.RecoveryDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("authoring failure created apply artifact %q: %v", path, err)
		}
	}
}
