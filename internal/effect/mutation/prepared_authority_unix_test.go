//go:build darwin || linux

package mutation

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type preparedWorkBudget struct {
	paths, entries        int
	bytes                 int64
	pathLimit, entryLimit int
	byteLimit             int64
}

func preparedBudget() *preparedWorkBudget {
	return &preparedWorkBudget{pathLimit: 64 << 20, entryLimit: 400_000, byteLimit: 16 << 30}
}

func (budget *preparedWorkBudget) AdmitPathComponents(count int) error {
	if count < 0 || count > budget.pathLimit-budget.paths {
		return fmt.Errorf("prepared test path allocation exhausted")
	}
	budget.paths += count
	return nil
}

func (budget *preparedWorkBudget) AdmitPhysicalWork(paths, entries int, bytes int64) error {
	if entries < 0 || entries > budget.entryLimit-budget.entries || bytes < 0 || bytes > budget.byteLimit-budget.bytes {
		return fmt.Errorf("prepared test artifact allocation exhausted")
	}
	if err := budget.AdmitPathComponents(paths); err != nil {
		return err
	}
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

func TestPreparedExclusiveProjectionCoversWholeHeldSetAndFreshChecks(t *testing.T) {
	root := t.TempDir()
	control, document, peer := filepath.Join(root, "state.json"), filepath.Join(root, "config.toml"), filepath.Join(root, "unrelated")
	for _, path := range []string{control, document, peer} {
		if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var domains []Domain
	for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
		for _, path := range []string{control, peer} {
			domain, err := NewLogicalPathDomain(LogicalPathRequest{Path: path, Access: AccessExclusive, Effect: effect})
			if err != nil {
				t.Fatal(err)
			}
			domains = append(domains, domain)
		}
		domain, err := NewPhysicalPathDomain(PhysicalPathRequest{Path: document, Access: AccessExclusive, Effect: effect, Target: "codex", Scope: "project"})
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, domain)
	}
	leases, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer leases.Release()
	requests := []PhysicalAuthorityRequest{{Path: document, Target: "codex", Scope: "project"}}
	prepared, err := leases.PrepareCurrentExclusiveRequestsBounded(t.Context(), []string{control}, requests, 256, preparedBudget())
	if err != nil {
		t.Fatal(err)
	}
	requests[0].Target = "pi"
	projection := preparedBudget()
	if err := prepared.ReserveCheck(projection); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		allocation := &preparedWorkBudget{pathLimit: projection.paths, entryLimit: projection.entries, byteLimit: projection.bytes}
		if matched, err := prepared.MatchCurrent(t.Context(), allocation); err != nil || !matched {
			t.Fatalf("prepared bounded check failed: %v actual=%+v projection=%+v", err, allocation, projection)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "replacement"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "replacement"), document); err != nil {
		t.Fatal(err)
	}
	if matched, err := prepared.MatchCurrent(t.Context(), preparedBudget()); err != nil || !matched {
		t.Fatal("new inode changed exact path coverage", err)
	}
	if err := os.Remove(peer); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(document, peer); err != nil {
		t.Fatal(err)
	}
	if err := prepared.ReserveCheck(preparedBudget()); err != nil {
		t.Fatal("pure projection inspected the changed peer", err)
	}
	if matched, err := prepared.MatchCurrent(t.Context(), preparedBudget()); matched || err == nil {
		t.Fatal("unrelated held domain bypassed fresh prepared validation", err)
	}
	leases.Release()
	if matched, err := prepared.MatchCurrent(t.Context(), preparedBudget()); matched || err == nil {
		t.Fatal("prepared facts extended released lease lifetime", err)
	}
}

func TestPreparedDirectoryEntryProjectsIndependentReadOnlyAuthority(t *testing.T) {
	root := t.TempDir()
	parent, alias := filepath.Join(root, "real"), filepath.Join(root, "alias")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", alias); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(alias, "state.json")
	plan, err := PrepareDirectoryEntryAuthorityBounded(t.Context(), path, 256, preparedBudget())
	if err != nil {
		t.Fatal(err)
	}
	projection := preparedBudget()
	if err := plan.ReserveObservation(projection); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	allocation := &preparedWorkBudget{pathLimit: projection.paths, entryLimit: projection.entries, byteLimit: projection.bytes}
	observed, err := plan.ObserveBounded(t.Context(), allocation)
	if err != nil {
		t.Fatal("planned absent-to-existing observation failed", err)
	}
	legacy, err := ObserveDirectoryEntryAuthorityBounded(path, 256, preparedBudget())
	if err != nil {
		t.Fatal(err)
	}
	actual, actualExact := observed.Exact()
	expected, expectedExact := legacy.Exact()
	if !actualExact || !expectedExact || !actual.Equal(expected) {
		t.Fatal("prepared observation changed canonical namespace meaning")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := plan.ReserveObservation(preparedBudget()); err != nil {
		t.Fatal("read-only reservation inspected the current alias", err)
	}
	if _, err := plan.ObserveBounded(t.Context(), preparedBudget()); err == nil {
		t.Fatal("missing alias reused stale authority")
	}
}
