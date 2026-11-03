package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/test/ownershiptest"
)

func documentProtectionCommandFixture(t *testing.T, native bool) daempaths.Paths {
	t.Helper()
	storage, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(storage, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(storage, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(storage, "codex"))
	root := filepath.Join(storage, "workspace")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "version=1\ntargets=['codex']\n[instructions.project]\nsource='./instructions.md'\n"
	if native {
		manifest += "[[host_config]]\nid='preferences'\npath='config.toml'\nsource='./preferences.toml'\nownership='keys'\n"
	}
	for path, content := range map[string]string{"daem.toml": manifest, "instructions.md": "ordinary instructions\n", "preferences.toml": "model='native-test'\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: paths.ManifestPath}); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestPreparedNativeApplyAcceptsDisjointProtectionChangesAndPreservesThem(t *testing.T) {
	paths := documentProtectionCommandFixture(t, true)
	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	pending := ownershiptest.WritePendingDocumentProtection(t, paths.ManifestPath, filepath.Join(paths.ManifestRoot, "foreign.json"))
	if result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{}); err != nil || !result.ExecutionAttempted {
		t.Fatalf("disjoint arm invalidated prepared Apply: %v", err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"bind", "clear"} {
		if err := os.WriteFile(filepath.Join(paths.ManifestRoot, "instructions.md"), []byte(phase+" ordinary instructions\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: paths.ManifestPath}); err != nil {
			t.Fatal(err)
		}
		prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
		if err != nil {
			t.Fatal(err)
		}
		var transition ownership.ProtectionTransition
		if phase == "bind" {
			context, _ := executionidentity.NewContext(executionidentity.Linux, strings.Repeat("d", 64), "12345678-1234-1234-1234-123456789abc")
			execution, _ := executionidentity.NewBinding(pending.Invocation(), context, 123)
			transition, err = ownership.NewBindProtection(pending, execution)
			pending, _ = pending.Bind(execution)
		} else {
			transition, err = ownership.NewClearProtection(pending)
		}
		if err != nil {
			t.Fatal(err)
		}
		published, err := store.TransitionProtection(t.Context(), pending.Owner(), transition)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{}); err != nil {
			t.Fatalf("disjoint %s invalidated prepared Apply: %v", phase, err)
		}
		current, err := store.Load(t.Context())
		if err != nil || !current.Equal(published) {
			t.Fatalf("ordinary/native publication lost unrelated %s facts: %v", phase, err)
		}
	}
}

func TestProjectWriterWithoutNativeKeysRefusesRelevantProtectionBeforeEffects(t *testing.T) {
	for _, scenario := range []struct{ writer, timing string }{
		{"file", "before-plan"},
		{"file", "after-plan"},
		{"mcp", "before-plan"},
		{"mcp", "after-plan"},
	} {
		t.Run(scenario.writer+"/"+scenario.timing, func(t *testing.T) {
			paths := documentProtectionCommandFixture(t, false)
			protected := filepath.Join(paths.ManifestRoot, "AGENTS.md")
			if scenario.writer == "mcp" {
				manifest, err := os.ReadFile(paths.ManifestPath)
				if err != nil {
					t.Fatal(err)
				}
				manifest = append(manifest, []byte("\n[[mcp_server]]\nname='other'\ntransport='stdio'\ncommand='true'\n")...)
				if err := os.WriteFile(paths.ManifestPath, manifest, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: paths.ManifestPath}); err != nil {
					t.Fatal(err)
				}
				protected = filepath.Join(paths.ManifestRoot, ".codex", "config.toml")
			}
			var planned *PreparedWrite
			var err error
			if scenario.timing == "after-plan" {
				planned, err = PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
				if err != nil {
					t.Fatal(err)
				}
			}
			ownershiptest.WritePendingDocumentProtection(t, paths.ManifestPath, protected)
			if scenario.timing == "before-plan" {
				planned, err = PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
				if planned != nil {
					_ = planned.Close()
				}
			} else {
				var result CommandResult
				result, err = ExecuteWithOptions(t.Context(), planned, ExecuteOptions{})
				if result.ExecutionAttempted {
					t.Fatal("protected project writer attempted effects")
				}
			}
			var pending *ownership.ProtectionPendingError
			if !errors.As(err, &pending) {
				t.Fatalf("protected project writer was not refused by its safety record: %v", err)
			}
			for _, path := range []string{protected, filepath.Join(paths.ManifestRoot, "AGENTS.md"), paths.StatefilePath} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("protection refusal changed %s: %v", path, err)
				}
			}
		})
	}
}

func TestProtectedUnchangedSelectionAllowsNoOpAndDisjointWork(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "native"}[native], func(t *testing.T) {
			paths := documentProtectionCommandFixture(t, native)
			apply := func() CommandResult {
				t.Helper()
				planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
				if err != nil {
					t.Fatal(err)
				}
				result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			apply()
			protected := filepath.Join(paths.ManifestRoot, "AGENTS.md")
			if native {
				protected = filepath.Join(paths.ManifestRoot, ".codex", "config.toml")
			}
			pending := ownershiptest.WritePendingDocumentProtection(t, paths.ManifestPath, protected)
			before, err := os.ReadFile(protected)
			if err != nil {
				t.Fatal(err)
			}
			if result := apply(); result.ExecutionAttempted || result.ActionCount != 0 {
				t.Fatal("current protected selection became a mutation")
			}
			disjoint := filepath.Join(paths.ManifestRoot, "AGENTS.md")
			if native {
				if err := os.WriteFile(filepath.Join(paths.ManifestRoot, "instructions.md"), []byte("updated ordinary instructions\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				manifest, err := os.ReadFile(paths.ManifestPath)
				if err != nil {
					t.Fatal(err)
				}
				manifest = append(manifest, []byte("\n[[mcp_server]]\nname='other'\ntransport='stdio'\ncommand='true'\n")...)
				if err := os.WriteFile(paths.ManifestPath, manifest, 0o600); err != nil {
					t.Fatal(err)
				}
				disjoint = filepath.Join(paths.ManifestRoot, ".codex", "config.toml")
			}
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: paths.ManifestPath}); err != nil {
				t.Fatal(err)
			}
			apply()
			if after, err := os.ReadFile(protected); err != nil || string(after) != string(before) {
				t.Fatal("disjoint work changed the protected selection", err)
			}
			if _, err := os.Stat(disjoint); err != nil {
				t.Fatal("disjoint output was not published", err)
			}
			if native {
				content, err := os.ReadFile(disjoint)
				if err != nil || string(content) != "updated ordinary instructions\n" {
					t.Fatal("disjoint instruction update did not complete", err)
				}
			}
			store, _ := ownershipstore.New(paths.OwnershipRegistryPath)
			registry, err := store.Load(t.Context())
			if err != nil || len(registry.PendingProtections()) != 1 || !registry.PendingProtections()[0].Equal(pending) {
				t.Fatal("disjoint work lost its unchanged protection record", err)
			}
		})
	}
}

func TestProjectWriteAdmissionRetainsLeaseCoverageAgainstCooperatingArm(t *testing.T) {
	paths := documentProtectionCommandFixture(t, false)
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := store.Acquire(t.Context(), prepared.lifecycle.authorityEvidence.domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Release()
	physical, err := mutation.NewPhysicalAuthoritySet(mutation.PhysicalAuthorityRequest{Path: filepath.Join(paths.ManifestRoot, "AGENTS.md"), Target: "codex", Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if covered, err := writer.CoversPhysicalAuthority(physical); err != nil || !covered {
		t.Fatal("no-native project writer does not cover a cross-owner/target arm", err)
	}
	armer, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: filepath.Join(paths.ManifestRoot, "AGENTS.md"), Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry, Target: "pi", Scope: "global"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if other, err := store.Acquire(ctx, armer); err == nil {
		_ = other.Release()
		t.Fatal("cooperating arm entered after admission while the writer retained its domains")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("arm failed for an unrelated reason", err)
	}
	if err := writer.Release(); err != nil {
		t.Fatal(err)
	}
	other, err := store.Acquire(t.Context(), armer)
	if err != nil {
		t.Fatal("released writer did not unblock the same arm domains", err)
	}
	if err := other.Release(); err != nil {
		t.Fatal(err)
	}
}
