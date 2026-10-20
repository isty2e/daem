package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/realization/lockfile"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestNonCanonicalLockKeyKeepsReadFailureGuidance(t *testing.T) {
	root := isolatedApplyAuthorityRoot(t)
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	if err := os.WriteFile("daem.toml", []byte("version=1\ntargets=[\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runWithoutTerminal([]string{"lock"}, &stdout, &stderr); code != 0 {
		t.Fatalf("seed lock code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
	}
	lockPath := filepath.Join(project, "daem.lock.toml")
	canonical, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(canonical), "version", "Version", 1)
	if mutated == string(canonical) {
		t.Fatal("generated lock has no version envelope")
	}
	if err := os.WriteFile(lockPath, []byte(mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	_, inputErr := lockfile.Load(context.Background(), lockPath)
	if inputErr == nil {
		t.Fatal("noncanonical lock was accepted")
	}
	expected := clipresent.FailureDiagnosticFromError(fmt.Errorf("%w: %w", applyworkflow.ErrReadLockfile, inputErr))
	before := snapshotApplyInputFiles(t, root)
	for _, jsonOutput := range []bool{false, true} {
		stdout.Reset()
		stderr.Reset()
		args := []string{"apply", "--dry-run"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
		}
		diagnostic := refusalDiagnosticText(t, "apply", &stdout, &stderr, jsonOutput)
		if !strings.Contains(diagnostic, expected.Message()) {
			t.Fatalf("lost lock read cause: %q", diagnostic)
		}
		for _, guidance := range expected.Guidance() {
			if !strings.Contains(diagnostic, guidance.Message()) {
				t.Fatalf("lost existing lock guidance: %q", guidance.Message())
			}
		}
		if !reflect.DeepEqual(before, snapshotApplyInputFiles(t, root)) {
			t.Fatal("invalid lock refusal changed fixture files")
		}
	}
}
