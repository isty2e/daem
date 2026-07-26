package build

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildGitSkillGroupSharesRepositorySnapshotAndRefreshesNextBuild(t *testing.T) {
	requireBuildGit(t)
	tempDir := t.TempDir()
	repositoryPath := filepath.Join(tempDir, "repository")
	runBuildGit(t, "", "init", repositoryPath)
	runBuildGit(t, repositoryPath, "checkout", "-b", "main")
	runBuildGit(t, repositoryPath, "config", "user.email", "daem@example.invalid")
	runBuildGit(t, repositoryPath, "config", "user.name", "Agent Env Test")
	writeSkillContent(t, repositoryPath, "skills/alpha", "---\nname: alpha\ndescription: alpha one\n---\n")
	writeSkillContent(t, repositoryPath, "skills/beta", "---\nname: beta\ndescription: beta\n---\n")
	firstCommit := commitBuildGit(t, repositoryPath, "initial skills")

	paths, err := daempaths.Resolve(filepath.Join(tempDir, "project", "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve paths returned error: %v", err)
	}
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	input := desired.Spec{SkillSets: []skill.SkillSet{desiredtest.SkillSet(t, skill.SkillSetSpec{
		Source:      mustGitSource(t, repositoryPath, "skills", "main"),
		Include:     []skill.Selector{desiredtest.Selector(t, "glob:*")},
		Targets:     []target.Target{target.TargetClaudeCode},
		Scope:       target.ScopeProject,
		InstallMode: skill.InstallModeCopy,
	})}}

	firstEvents := newConcurrentSourceEventRecorder()
	first, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{
		MaxParallelSourceOps: 4,
		SourceEvents:         firstEvents.record,
	})
	if err != nil {
		t.Fatalf("first BuildWithOptions returned error: %v", err)
	}
	assertGitGroupBuild(t, first, firstCommit)
	assertSourceEventCount(t, firstEvents.snapshot(), acquisition.EventFetch, 1)
	assertSourceEventRequestIDs(t, firstEvents.snapshot(), acquisition.EventFetch, []acquisition.RequestID{"skill_group_root:000000"})

	writeSkillContent(t, repositoryPath, "skills/alpha", "---\nname: alpha\ndescription: alpha two\n---\n")
	secondCommit := commitBuildGit(t, repositoryPath, "update alpha")

	secondEvents := newConcurrentSourceEventRecorder()
	second, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{
		MaxParallelSourceOps: 4,
		SourceEvents:         secondEvents.record,
	})
	if err != nil {
		t.Fatalf("second BuildWithOptions returned error: %v", err)
	}
	assertGitGroupBuild(t, second, secondCommit)
	assertSourceEventCount(t, secondEvents.snapshot(), acquisition.EventFetch, 1)
	firstAlpha := mustExactSupply(t, mustLockedSubject(t, first, entity.KindSkill, "alpha"))
	secondAlpha := mustExactSupply(t, mustLockedSubject(t, second, entity.KindSkill, "alpha"))
	if firstAlpha.ContentHash() == secondAlpha.ContentHash() {
		t.Fatalf("alpha ContentHash did not change across fresh resolution sessions: %q", firstAlpha.ContentHash())
	}
}

func TestBuildDirectGitPathsShareRepositorySnapshot(t *testing.T) {
	requireBuildGit(t)
	tempDir := t.TempDir()
	repositoryPath := filepath.Join(tempDir, "repository")
	runBuildGit(t, "", "init", repositoryPath)
	runBuildGit(t, repositoryPath, "checkout", "-b", "main")
	runBuildGit(t, repositoryPath, "config", "user.email", "daem@example.invalid")
	runBuildGit(t, repositoryPath, "config", "user.name", "Agent Env Test")
	writeSkillContent(t, repositoryPath, "skills/alpha", "---\nname: alpha\ndescription: alpha\n---\n")
	writeSkillContent(t, repositoryPath, "skills/beta", "---\nname: beta\ndescription: beta\n---\n")
	commit := commitBuildGit(t, repositoryPath, "add skills")

	paths, err := daempaths.Resolve(filepath.Join(tempDir, "project", "daem.toml"))
	if err != nil {
		t.Fatalf("Resolve paths returned error: %v", err)
	}
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	input := desired.Spec{Skills: []skill.Skill{
		desiredtest.Skill(t, skill.Spec{
			Name: "alpha", Source: mustGitSource(t, repositoryPath, "skills/alpha", "main"),
			Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
		}),
		desiredtest.Skill(t, skill.Spec{
			Name: "beta", Source: mustGitSource(t, repositoryPath, "skills/beta", "main"),
			Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
		}),
	}}

	events := newConcurrentSourceEventRecorder()
	locked, err := buildWithTestOptions(context.Background(), lockEnvironment(t, input), resolver, Options{
		MaxParallelSourceOps: 4,
		SourceEvents:         events.record,
	})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	assertGitGroupBuild(t, locked, commit)
	assertSourceEventCount(t, events.snapshot(), acquisition.EventFetch, 1)
	assertSourceEventRequestIDs(t, events.snapshot(), acquisition.EventFetch, []acquisition.RequestID{"skill:000000"})
}

type concurrentSourceEventRecorder struct {
	mu     sync.Mutex
	events []acquisition.Event
}

func newConcurrentSourceEventRecorder() *concurrentSourceEventRecorder {
	return &concurrentSourceEventRecorder{}
}

func (recorder *concurrentSourceEventRecorder) record(event acquisition.Event) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.events = append(recorder.events, event)
}

func (recorder *concurrentSourceEventRecorder) snapshot() []acquisition.Event {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]acquisition.Event(nil), recorder.events...)
}

func assertGitGroupBuild(t *testing.T, lockedFile lock.File, commit string) {
	t.Helper()
	lockedSkills := lockedExactSupplySubjectsOfKind(lockedFile, entity.KindSkill)
	if len(lockedSkills) != 2 {
		t.Fatalf("locked skill count = %d, want 2: %#v", len(lockedSkills), lockedSkills)
	}
	wantNames := []string{"alpha", "beta"}
	for index, lockedSkill := range lockedSkills {
		if lockedSkill.EntityID().Name() != wantNames[index] {
			t.Fatalf("locked skill[%d].Name = %q, want %q", index, lockedSkill.EntityID().Name(), wantNames[index])
		}
		identity := mustExactSupply(t, lockedSkill)
		if string(identity.ResolvedRef()) != commit {
			t.Fatalf("locked skill[%d].ResolvedRef = %q, want %q", index, identity.ResolvedRef(), commit)
		}
		if identity.ContentHash() == "" {
			t.Fatalf("locked skill[%d].ContentHash is empty", index)
		}
	}
}

func assertSourceEventCount(t *testing.T, events []acquisition.Event, kind acquisition.EventKind, want int) {
	t.Helper()
	count := 0
	for _, event := range events {
		if event.Kind() == kind {
			count++
		}
	}
	if count != want {
		t.Fatalf("%s event count = %d, want %d: %#v", kind, count, want, events)
	}
}

func assertSourceEventRequestIDs(t *testing.T, events []acquisition.Event, kind acquisition.EventKind, want []acquisition.RequestID) {
	t.Helper()
	got := make([]acquisition.RequestID, 0)
	for _, event := range events {
		if event.Kind() == kind {
			got = append(got, event.Request().ID())
		}
	}
	if strings.Join(requestIDsToStrings(got), ",") != strings.Join(requestIDsToStrings(want), ",") {
		t.Fatalf("%s request IDs = %#v, want %#v", kind, got, want)
	}
}

func requestIDsToStrings(ids []acquisition.RequestID) []string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = string(id)
	}
	return values
}

func requireBuildGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is required")
	}
}

func runBuildGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v returned error: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func commitBuildGit(t *testing.T, repositoryPath string, message string) string {
	t.Helper()
	runBuildGit(t, repositoryPath, "add", ".")
	runBuildGit(t, repositoryPath, "commit", "-m", message)
	return runBuildGit(t, repositoryPath, "rev-parse", "HEAD")
}
