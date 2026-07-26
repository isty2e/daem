package generate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func TestBuildPropagatesCancellationBeforeSourceEvents(t *testing.T) {
	tempDir := t.TempDir()
	writeGenerateTestFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	paths, err := daempaths.Resolve(filepath.Join(tempDir, "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	sourceEvents := make([]acquisition.Event, 0)
	lockEvents := make([]lockbuild.Event, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = Build(ctx, Input{
		Paths:              paths,
		Environment:        environment,
		UsePersistentCache: false,
		SourceEvents: func(event acquisition.Event) {
			sourceEvents = append(sourceEvents, event)
		},
		Events: func(event lockbuild.Event) {
			lockEvents = append(lockEvents, event)
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Build error = %v, want context.Canceled", err)
	}
	if len(sourceEvents) != 0 || len(lockEvents) != 0 {
		t.Fatalf("canceled Build emitted events: source=%#v lock=%#v", sourceEvents, lockEvents)
	}
}

func TestBuildMCPOnlyDoesNotCreateSourceEvents(t *testing.T) {
	tempDir := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(tempDir, "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	sourceEvents := make([]acquisition.Event, 0)
	lockEvents := make([]lockbuild.Event, 0)

	result, err := Build(context.Background(), Input{
		Paths:              paths,
		Environment:        environment,
		UsePersistentCache: false,
		SourceEvents: func(event acquisition.Event) {
			sourceEvents = append(sourceEvents, event)
		},
		Events: func(event lockbuild.Event) {
			lockEvents = append(lockEvents, event)
		},
		MCPEncoder: mcpcodec.CanonicalMCPBindingContribution,
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(result.Snapshot().Locked.Subjects()) != 1 {
		t.Fatalf("locked subjects = %#v, want one MCP subject", result.Snapshot().Locked.Subjects())
	}
	if len(sourceEvents) != 0 {
		t.Fatalf("source events = %#v, want none for MCP-only manifest", sourceEvents)
	}
	if len(lockEvents) != 1 ||
		lockEvents[0].Kind != lockbuild.EventSnapshotValidated ||
		lockEvents[0].Count != 1 {
		t.Fatalf("lock events = %#v, want one snapshot validation event with count 1", lockEvents)
	}

	content := string(result.Content())
	mutated := result.Content()
	mutated[0] ^= 0xff
	if string(result.Content()) != content {
		t.Fatal("Result.Content exposed mutable serialized lockfile bytes")
	}
}

func TestBuildRequiresOnlyEncodersForDeclaredAggregateFamilies(t *testing.T) {
	tempDir := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(tempDir, "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	tests := []struct {
		name     string
		manifest string
		wantErr  string
	}{
		{
			name:     "no aggregate family",
			manifest: "version = 1\ntargets = [\"codex\"]\n",
		},
		{
			name: "Hook family",
			manifest: `version = 1
targets = ["codex"]

[[hook]]
name = "guard"
event = "Stop"
command = "make test"
targets = ["codex"]
`,
			wantErr: "Hook contribution encoder is required",
		},
		{
			name: "MCP family",
			manifest: `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
`,
			wantErr: "MCP contribution encoder is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment, err := declarationmanifest.Decode([]byte(test.manifest))
			if err != nil {
				t.Fatalf("Decode returned error: %v", err)
			}
			_, err = Build(context.Background(), Input{
				Paths:              paths,
				Environment:        environment,
				UsePersistentCache: false,
			})
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Build returned error without aggregate families: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Build error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestBuildRejectsMissingLocalSource(t *testing.T) {
	tempDir := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(tempDir, "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/missing.md"
targets = ["codex"]
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	_, err = Build(context.Background(), Input{
		Paths:              paths,
		Environment:        environment,
		UsePersistentCache: false,
	})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Build error = %v, want os.ErrNotExist", err)
	}
}

func TestBuildSequentialAndParallelProduceSameLockfileBytes(t *testing.T) {
	tempDir := t.TempDir()
	writeGenerateTestFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeGenerateTestFile(t, tempDir, "instructions/project.md", "project instructions\n")
	paths, err := daempaths.Resolve(filepath.Join(tempDir, "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	input := Input{
		Paths:              paths,
		Environment:        environment,
		UsePersistentCache: false,
	}

	zero, err := Build(context.Background(), input)
	if err != nil {
		t.Fatalf("Build zero-valued parallelism returned error: %v", err)
	}
	sequential, err := Build(context.Background(), Input{
		Paths:                input.Paths,
		Environment:          input.Environment,
		UsePersistentCache:   input.UsePersistentCache,
		MaxParallelSourceOps: 1,
	})
	if err != nil {
		t.Fatalf("Build sequential returned error: %v", err)
	}
	parallel, err := Build(context.Background(), Input{
		Paths:                input.Paths,
		Environment:          input.Environment,
		UsePersistentCache:   input.UsePersistentCache,
		MaxParallelSourceOps: 4,
	})
	if err != nil {
		t.Fatalf("Build parallel returned error: %v", err)
	}

	if string(zero.Content()) != string(sequential.Content()) {
		t.Fatalf("zero-valued lockfile bytes differ from sequential:\n%s\n---\n%s", zero.Content(), sequential.Content())
	}
	if string(parallel.Content()) != string(sequential.Content()) {
		t.Fatalf("parallel lockfile bytes differ from sequential:\n%s\n---\n%s", parallel.Content(), sequential.Content())
	}
}

func TestBuildPassesSourceAndLockEventSinks(t *testing.T) {
	tempDir := t.TempDir()
	writeGenerateTestFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeGenerateTestFile(t, tempDir, "instructions/project.md", "project instructions\n")
	paths, err := daempaths.Resolve(filepath.Join(tempDir, "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	var eventMu sync.Mutex
	sourceEvents := make([]acquisition.Event, 0)
	lockEvents := make([]lockbuild.Event, 0)

	_, err = Build(context.Background(), Input{
		Paths:                paths,
		Environment:          environment,
		UsePersistentCache:   false,
		MaxParallelSourceOps: 4,
		SourceEvents: func(event acquisition.Event) {
			eventMu.Lock()
			defer eventMu.Unlock()
			sourceEvents = append(sourceEvents, event)
		},
		Events: func(event lockbuild.Event) {
			eventMu.Lock()
			defer eventMu.Unlock()
			lockEvents = append(lockEvents, event)
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if !hasSourceEventKind(sourceEvents, acquisition.EventQueued) ||
		!hasSourceEventKind(sourceEvents, acquisition.EventStarted) ||
		!hasSourceEventKind(sourceEvents, acquisition.EventCompleted) {
		t.Fatalf("source events = %#v, want queued/started/completed", sourceEvents)
	}
	if !hasLockEventKind(lockEvents, lockbuild.EventResourceResolveStarted) ||
		!hasLockEventKind(lockEvents, lockbuild.EventResourceLocked) ||
		!hasLockEventKind(lockEvents, lockbuild.EventSnapshotValidated) {
		t.Fatalf("lock events = %#v, want resolve/lock/snapshot events", lockEvents)
	}
}

func TestBuildSourceEventsDoNotForceDirectCallerParallelism(t *testing.T) {
	tempDir := t.TempDir()
	writeGenerateTestFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	paths, err := daempaths.Resolve(filepath.Join(tempDir, "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	sourceEvents := make([]acquisition.Event, 0)
	lockEvents := make([]lockbuild.Event, 0)

	_, err = Build(context.Background(), Input{
		Paths:              paths,
		Environment:        environment,
		UsePersistentCache: false,
		SourceEvents: func(event acquisition.Event) {
			sourceEvents = append(sourceEvents, event)
		},
		Events: func(event lockbuild.Event) {
			lockEvents = append(lockEvents, event)
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if len(sourceEvents) != 0 {
		t.Fatalf("source events = %#v, want none for direct zero-valued parallelism", sourceEvents)
	}
	if !hasLockEventKind(lockEvents, lockbuild.EventResourceResolveStarted) ||
		!hasLockEventKind(lockEvents, lockbuild.EventResourceLocked) ||
		!hasLockEventKind(lockEvents, lockbuild.EventSnapshotValidated) {
		t.Fatalf("lock events = %#v, want sequential lock events", lockEvents)
	}
}

func writeGenerateTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func hasSourceEventKind(events []acquisition.Event, kind acquisition.EventKind) bool {
	for _, event := range events {
		if event.Kind() == kind {
			return true
		}
	}
	return false
}

func hasLockEventKind(events []lockbuild.Event, kind lockbuild.EventKind) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}
