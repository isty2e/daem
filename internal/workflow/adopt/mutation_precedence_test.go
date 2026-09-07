//go:build darwin || linux

package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestImportMutationEvidencePreservesPathErrorBeforeLaterRevisionConflict(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	bounded, err := adoptmodel.NewBoundedFileScanEvidence(1024)
	if err != nil {
		t.Fatal(err)
	}
	scanPath := filepath.Join(root, "scan")
	plan := precedenceAdoptPlan(
		t,
		root,
		filepath.Join(loop, "hook.json"),
		[]adoptmodel.Scan{
			precedenceScan("listing", scanPath, adoptmodel.DirectoryListingScanEvidence()),
			precedenceScan("file", scanPath, bounded),
		},
	)

	_, _, _, err = importMutationEvidence(plan, mustImportBarrier(t, plan))
	if err == nil || !strings.Contains(err.Error(), "canonicalize mutation path") {
		t.Fatalf("importMutationEvidence error = %v, want earlier hook path error", err)
	}
}

func TestImportMutationEvidencePreservesRevisionConflictBeforeLaterPathError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	bounded, err := adoptmodel.NewBoundedFileScanEvidence(1024)
	if err != nil {
		t.Fatal(err)
	}
	scanPath := filepath.Join(root, "scan")
	plan := precedenceAdoptPlan(
		t,
		root,
		filepath.Join(root, "hook.json"),
		[]adoptmodel.Scan{
			precedenceScan("listing", scanPath, adoptmodel.DirectoryListingScanEvidence()),
			precedenceScan("file", scanPath, bounded),
			precedenceScan("late", filepath.Join(loop, "late"), adoptmodel.DirectoryListingScanEvidence()),
		},
	)

	_, _, _, err = importMutationEvidence(plan, mustImportBarrier(t, plan))
	if err == nil || !strings.Contains(err.Error(), "conflicting revision semantics") {
		t.Fatalf("importMutationEvidence error = %v, want earlier revision conflict", err)
	}
}

func precedenceAdoptPlan(
	t *testing.T,
	root string,
	hookPath string,
	scans []adoptmodel.Scan,
) adoptmodel.Plan {
	t.Helper()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := adoptmodel.NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		profile.ImportableTargets(),
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := adoptmodel.NewCandidateSet(adoptmodel.CandidateSetInput{
		Hooks: []adoptmodel.Hook{{
			ResourceName: "hook",
			Target:       target.TargetCodex,
			Scope:        target.ScopeProject,
			LivePath:     hookPath,
			Event:        "SessionStart",
			Command:      "echo ready",
		}},
		Scans: scans,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := adoptmodel.NewPlan(request, nil, []byte("version = 1\n"), candidates, nil)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func precedenceScan(
	name string,
	path string,
	evidence adoptmodel.ScanEvidence,
) adoptmodel.Scan {
	return adoptmodel.Scan{
		ResourceKind: "hook",
		ResourceName: name,
		Target:       target.TargetCodex,
		Scope:        target.ScopeProject,
		LivePath:     path,
		Status:       "scanned",
		Evidence:     evidence,
	}
}
