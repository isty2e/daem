//go:build darwin || linux

package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func TestValidateAbsentProjectRecoveryPathRejectsAliasesAndDrift(t *testing.T) {
	tests := []struct {
		name        string
		prepare     func(t *testing.T, root string, outside string)
		wantFailure rootedpath.FailureKind
		wantText    string
	}{
		{
			name: "ancestor symlink",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
					t.Fatalf("create ancestor symlink: %v", err)
				}
			},
			wantFailure: rootedpath.FailureAncestorSymlink,
		},
		{
			name: "dangling ancestor symlink",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, ".agents")); err != nil {
					t.Fatalf("create dangling ancestor symlink: %v", err)
				}
			},
			wantFailure: rootedpath.FailureDanglingAncestorSymlink,
		},
		{
			name: "final symlink",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o700); err != nil {
					t.Fatalf("create project ancestry: %v", err)
				}
				if err := os.Symlink(outside, filepath.Join(root, ".agents", "skills", "review")); err != nil {
					t.Fatalf("create final symlink: %v", err)
				}
			},
			wantFailure: rootedpath.FailureFinalSymlink,
		},
		{
			name: "appeared destination",
			prepare: func(t *testing.T, root string, _ string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, ".agents", "skills", "review"), 0o700); err != nil {
					t.Fatalf("create appeared destination: %v", err)
				}
			},
			wantText: "appeared after the live observation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "project")
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatalf("create project root: %v", err)
			}
			if err := os.Mkdir(outside, 0o700); err != nil {
				t.Fatalf("create outside root: %v", err)
			}
			tt.prepare(t, root, outside)
			session := mustProjectAuthoritySession(t, root)
			defer session.root.Close()
			mutation := pathMutation{
				Kind:        pathMutationCreate,
				Scope:       target.ScopeProject,
				Destination: output.Destination(".agents/skills/review"),
			}

			err := validateAbsentProjectRecoveryPath(
				context.Background(),
				journalTestFilesystem(),
				mutation,
				session,
			)
			if err == nil {
				t.Fatal("validateAbsentProjectRecoveryPath returned nil, want rejection")
			}
			if tt.wantFailure != "" && !hasRootedPathFailureKind(err, tt.wantFailure) {
				t.Fatalf("error = %v, want %s", err, tt.wantFailure)
			}
			if tt.wantText != "" && !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("error = %v, want text %q", err, tt.wantText)
			}
		})
	}
}

func TestValidateAbsentProjectRecoveryPathAcceptsMissingDestination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	session := mustProjectAuthoritySession(t, root)
	defer session.root.Close()
	mutation := pathMutation{
		Kind:        pathMutationCreate,
		Scope:       target.ScopeProject,
		Destination: output.Destination(".agents/skills/review"),
	}

	if err := validateAbsentProjectRecoveryPath(
		context.Background(),
		journalTestFilesystem(),
		mutation,
		session,
	); err != nil {
		t.Fatalf("validateAbsentProjectRecoveryPath returned error: %v", err)
	}
}
