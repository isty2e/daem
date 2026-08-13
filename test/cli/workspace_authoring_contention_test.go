package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestEightConcurrentAddSkillsConvergeAfterExplicitRetries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	const writerCount = 8
	helpers := make([]*workspaceMutationHelper, 0, writerCount)
	for index := range writerCount {
		name := fmt.Sprintf("writer-%d", index)
		sourcePath := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(sourcePath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(sourcePath, "SKILL.md"),
			fmt.Appendf(nil, "---\nname: %s\ndescription: Concurrent fixture.\n---\n", name),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		helper := startWorkspaceMutationHelper(t, []string{
			"add", "skill", sourcePath,
			"--manifest", manifestPath, "--target", "codex",
		})
		helpers = append(helpers, helper)
		t.Cleanup(helper.kill)
	}

	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	manifestDomain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
		Path: manifestPath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry,
	})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(context.Background(), manifestDomain)
	if err != nil {
		t.Fatal(err)
	}
	holderReleased := false
	defer func() {
		if !holderReleased {
			_ = holder.Release()
		}
	}()

	for _, helper := range helpers {
		helper.start(t)
	}
	// The held OS lease is the synchronization barrier. This delay only gives
	// every ready child time to reach it; completion remains impossible while held.
	time.Sleep(250 * time.Millisecond)
	for index, helper := range helpers {
		select {
		case err := <-helper.done:
			t.Fatalf("helper %d completed while manifest lease was held: %v; stderr=%s", index, err, helper.stderr.String())
		default:
		}
	}
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}
	holderReleased = true

	winners := 0
	stale := make([]*workspaceMutationHelper, 0, writerCount-1)
	for index, helper := range helpers {
		err := waitWorkspaceMutationHelper(t, helper)
		if err == nil {
			winners++
			continue
		}
		if !strings.Contains(helper.stderr.String(), "stale_snapshot") {
			t.Fatalf("helper %d error = %v, stderr=%q; want stale_snapshot", index, err, helper.stderr.String())
		}
		stale = append(stale, helper)
	}
	if winners != 1 || len(stale) != writerCount-1 {
		t.Fatalf("initial results = %d winners/%d stale; want 1/%d", winners, len(stale), writerCount-1)
	}

	for index, helper := range stale {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := clipkg.RunWithOptions(helper.args, clipkg.RunOptions{
			Context: context.Background(), Stdout: &stdout, Stderr: &stderr,
		}); exitCode != 0 {
			t.Fatalf("retry %d exitCode=%d stderr=%s", index, exitCode, stderr.String())
		}
	}

	manifestContent, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := lockfile.Load(t.Context(), paths.LockfilePath)
	if err != nil {
		t.Fatalf("load final lockfile: %v", err)
	}
	if len(testkit.LockedSkills(t, loaded)) != writerCount {
		t.Fatalf("locked skills = %d, want %d", len(testkit.LockedSkills(t, loaded)), writerCount)
	}
	lockedNames := make(map[string]bool, writerCount)
	for _, skill := range testkit.LockedSkills(t, loaded) {
		lockedNames[skill.Name] = true
	}
	for index := range writerCount {
		name := fmt.Sprintf("writer-%d", index)
		if !strings.Contains(string(manifestContent), fmt.Sprintf("name = %q", name)) {
			t.Fatalf("final manifest missing %q:\n%s", name, manifestContent)
		}
		if !lockedNames[name] {
			t.Fatalf("final lockfile missing %q: %#v", name, testkit.LockedSkills(t, loaded))
		}
	}
}
