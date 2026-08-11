package archguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPathStatusSharesOnePersistenceEpoch(t *testing.T) {
	root := findRepoRoot(t)
	checks := []struct {
		path         string
		stateLoad    string
		claimsLoad   string
		wantState    int
		wantClaims   int
		epochPurpose string
	}{
		{
			path:         "internal/workflow/status/command.go",
			stateLoad:    "statefile.LoadOptional(ctx, paths.StatefilePath)",
			claimsLoad:   "carrierStore.LoadForSelectedAuthority(",
			wantState:    1,
			wantClaims:   1,
			epochPurpose: "target availability",
		},
		{
			path:         "internal/workflow/readiness/assessment.go",
			stateLoad:    "statefile.LoadOptional(ctx, paths.StatefilePath)",
			claimsLoad:   "carrierClaimsStore.Load(ctx)",
			wantState:    0,
			wantClaims:   0,
			epochPurpose: "readiness planning receives canonical facts",
		},
	}

	totalStateLoads := 0
	totalClaimsLoads := 0
	for _, check := range checks {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(check.path)))
		if err != nil {
			t.Fatalf("ReadFile %q returned error: %v", check.path, err)
		}
		stateLoads := strings.Count(string(content), check.stateLoad)
		claimsLoads := strings.Count(string(content), check.claimsLoad)
		if stateLoads != check.wantState || claimsLoads != check.wantClaims {
			t.Fatalf(
				"%s persistence loads for %s = state:%d claims:%d, want state:%d claims:%d",
				check.path,
				check.epochPurpose,
				stateLoads,
				claimsLoads,
				check.wantState,
				check.wantClaims,
			)
		}
		totalStateLoads += stateLoads
		totalClaimsLoads += claimsLoads
	}

	if totalStateLoads != 1 || totalClaimsLoads != 1 {
		t.Fatalf(
			"status command persistence loads = state:%d claims:%d, want state:1 claims:1",
			totalStateLoads,
			totalClaimsLoads,
		)
	}

	statusContent, err := os.ReadFile(filepath.Join(root, "internal/workflow/status/command.go"))
	if err != nil {
		t.Fatalf("ReadFile status command: %v", err)
	}
	if !strings.Contains(
		string(statusContent),
		"PersistenceEpoch:        &loaded.PersistenceEpoch",
	) {
		t.Fatal("status readiness does not consume its target-selection persistence epoch")
	}
}

func TestReadPathApplyLoadsOnePersistencePairPerNamedPlanningEpoch(t *testing.T) {
	root := findRepoRoot(t)
	applyCommandPath := filepath.Join(root, "internal/workflow/apply/command.go")
	applyContent, err := os.ReadFile(applyCommandPath)
	if err != nil {
		t.Fatalf("ReadFile apply command: %v", err)
	}
	content := string(applyContent)
	if got := strings.Count(
		content,
		"statefile.LoadOptional(ctx, paths.StatefilePath)",
	); got != 1 {
		t.Fatalf("apply persistence state load sites = %d, want 1", got)
	}
	if got := strings.Count(content, "carrierStore.LoadForSelectedAuthority("); got != 1 {
		t.Fatalf("apply persistence claim load sites = %d, want 1", got)
	}
	if !strings.Contains(
		content,
		"PersistenceEpoch:        &loaded.PersistenceEpoch",
	) {
		t.Fatal("apply readiness does not consume its planning persistence epoch")
	}

	executePath := filepath.Join(root, "internal/workflow/apply/execute_command.go")
	executeContent, err := os.ReadFile(executePath)
	if err != nil {
		t.Fatalf("ReadFile apply execution: %v", err)
	}
	if got := strings.Count(
		string(executeContent),
		"planReadinessAtPaths(ctx, currentInput, execution.operationContext, planned.context.Paths)",
	); got != 1 {
		t.Fatalf("post-lease apply readiness rebuild sites = %d, want 1", got)
	}
}

func TestReadPathBaselineCacheHashAndVerificationSites(t *testing.T) {
	root := findRepoRoot(t)
	checks := []struct {
		path   string
		needle string
		want   int
		role   string
	}{
		{
			path:   "internal/supply/source/cache/rooted_entry.go",
			needle: "storagecommit.SnapshotRootedDirectory(",
			want:   2,
			role:   "rooted entry verification and bounded file snapshot",
		},
		{
			path:   "internal/supply/source/cache/rooted_publish.go",
			needle: "storagecommit.SnapshotRootedDirectory(",
			want:   2,
			role:   "private-stage copy and content verification",
		},
		{
			path:   "internal/supply/source/backend/gitcli/export.go",
			needle: "access.HashPath(ctx, contentPath)",
			want:   1,
			role:   "Git export identity hash",
		},
		{
			path:   "internal/supply/source/backend/s3object/immutable_reuse.go",
			needle: "sourcecache.VerifyFileRooted(",
			want:   1,
			role:   "bounded immutable S3 file verification",
		},
		{
			path:   "internal/supply/source/backend/s3object/immutable_reuse.go",
			needle: "sourcecache.VerifyDirectoryRooted(",
			want:   1,
			role:   "immutable S3 directory verification",
		},
	}

	for _, check := range checks {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(check.path)))
		if err != nil {
			t.Fatalf("ReadFile %q returned error: %v", check.path, err)
		}
		if got := strings.Count(string(content), check.needle); got != check.want {
			t.Fatalf(
				"%s sites in %s = %d, want %d",
				check.role,
				check.path,
				got,
				check.want,
			)
		}
	}
}
