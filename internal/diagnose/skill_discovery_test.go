package diagnose

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestRetainedSkillDiscoveryFindsOnlyModeledSameScopePaths(t *testing.T) {
	projectRoot := t.TempDir()
	skill := discoveryTestSkill(t, projectRoot, "review", targetpkg.TargetOpenCode, targetpkg.ScopeProject, "")
	selection := discoveryTestSelection(t, targetpkg.TargetOpenCode)

	mkdirSkillDiscovery(t, projectRoot, ".claude/skills", "review")
	mkdirSkillDiscovery(t, projectRoot, ".agents/skills", "review")
	mkdirSkillDiscovery(t, projectRoot, ".pi/skills", "review")
	mkdirSkillDiscovery(t, projectRoot, ".config/opencode/skills", "review")

	found := inspectRetainedSkillDiscoveries(
		context.Background(),
		daempaths.Paths{ManifestRoot: projectRoot},
		[]desiredskill.Skill{skill},
		selection,
		nil,
		skillDiscoveryObserver{stat: os.Stat},
	)

	if len(found) != 2 {
		t.Fatalf("findings = %#v, want only two modeled OpenCode project duplicates", found)
	}
	if !strings.HasSuffix(found[0].observedPath, filepath.Join(".agents", "skills", "review")) ||
		!strings.HasSuffix(found[1].observedPath, filepath.Join(".claude", "skills", "review")) {
		t.Fatalf("observed paths = %q, %q, want deterministic modeled roots", found[0].observedPath, found[1].observedPath)
	}
	for _, finding := range found {
		if finding.code != skillDiscoveryDuplicateRetainedCode {
			t.Fatalf("code = %q, want %q", finding.code, skillDiscoveryDuplicateRetainedCode)
		}
		if finding.target != targetpkg.TargetOpenCode || finding.scope != targetpkg.ScopeProject {
			t.Fatalf("finding axes = target %q scope %q", finding.target, finding.scope)
		}
	}
}

func TestRetainedSkillDiscoveryHonorsSelectedAlternateRoot(t *testing.T) {
	projectRoot := t.TempDir()
	skill := discoveryTestSkill(
		t,
		projectRoot,
		"review",
		targetpkg.TargetOpenCode,
		targetpkg.ScopeProject,
		".agents/skills",
	)
	selection := discoveryTestSelection(t, targetpkg.TargetOpenCode)

	mkdirSkillDiscovery(t, projectRoot, ".agents/skills", "review")
	mkdirSkillDiscovery(t, projectRoot, ".opencode/skills", "review")

	found := inspectRetainedSkillDiscoveries(
		context.Background(),
		daempaths.Paths{ManifestRoot: projectRoot},
		[]desiredskill.Skill{skill},
		selection,
		nil,
		skillDiscoveryObserver{stat: os.Stat},
	)

	if len(found) != 1 {
		t.Fatalf("findings = %#v, want default root retained beside selected alternate root", found)
	}
	if !strings.HasSuffix(found[0].selectedPath, filepath.Join(".agents", "skills", "review")) ||
		!strings.HasSuffix(found[0].observedPath, filepath.Join(".opencode", "skills", "review")) {
		t.Fatalf("selected=%q observed=%q", found[0].selectedPath, found[0].observedPath)
	}
}

func TestRetainedSkillDiscoverySuppressesHandledRelocationAndSymlinkAlias(t *testing.T) {
	projectRoot := t.TempDir()
	skill := discoveryTestSkill(t, projectRoot, "review", targetpkg.TargetOpenCode, targetpkg.ScopeProject, "")
	selection := discoveryTestSelection(t, targetpkg.TargetOpenCode)

	selected := mkdirSkillDiscovery(t, projectRoot, ".opencode/skills", "review")
	handled := mkdirSkillDiscovery(t, projectRoot, ".claude/skills", "review")
	alias := filepath.Join(projectRoot, ".agents", "skills", "review")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatalf("create alias parent: %v", err)
	}
	if err := os.Symlink(selected, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	handledDestination, err := output.Parse(".claude/skills/review")
	if err != nil {
		t.Fatalf("parse handled destination: %v", err)
	}

	found := inspectRetainedSkillDiscoveries(
		context.Background(),
		daempaths.Paths{ManifestRoot: projectRoot},
		[]desiredskill.Skill{skill},
		selection,
		[]skillDiscoveryCoverage{{
			entityID:    skill.ID(),
			target:      targetpkg.TargetOpenCode,
			scope:       targetpkg.ScopeProject,
			destination: handledDestination,
		}},
		skillDiscoveryObserver{stat: os.Stat},
	)

	if len(found) != 0 {
		t.Fatalf("findings = %#v, want handled path and selected symlink alias suppressed; handled=%s", found, handled)
	}
}

func TestRetainedSkillDiscoverySuppressesPhysicalAliasOfHandledPath(t *testing.T) {
	projectRoot := t.TempDir()
	skill := discoveryTestSkill(t, projectRoot, "review", targetpkg.TargetOpenCode, targetpkg.ScopeProject, "")
	selection := discoveryTestSelection(t, targetpkg.TargetOpenCode)
	handled := mkdirSkillDiscovery(t, projectRoot, ".claude/skills", "review")
	alias := filepath.Join(projectRoot, ".agents", "skills", "review")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatalf("create alias parent: %v", err)
	}
	if err := os.Symlink(handled, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	handledDestination, err := output.Parse(".claude/skills/review")
	if err != nil {
		t.Fatalf("parse handled destination: %v", err)
	}

	found := inspectRetainedSkillDiscoveries(
		context.Background(),
		daempaths.Paths{ManifestRoot: projectRoot},
		[]desiredskill.Skill{skill},
		selection,
		[]skillDiscoveryCoverage{{
			entityID:    skill.ID(),
			target:      targetpkg.TargetOpenCode,
			scope:       targetpkg.ScopeProject,
			destination: handledDestination,
		}},
		skillDiscoveryObserver{stat: os.Stat},
	)
	if len(found) != 0 {
		t.Fatalf("findings = %#v, want exact handled path and its physical alias suppressed", found)
	}
}

func TestRetainedSkillDiscoveryReportsBoundedObservationFailure(t *testing.T) {
	projectRoot := t.TempDir()
	skill := discoveryTestSkill(t, projectRoot, "review", targetpkg.TargetPi, targetpkg.ScopeProject, "")
	selection := discoveryTestSelection(t, targetpkg.TargetPi)
	calls := 0

	found := inspectRetainedSkillDiscoveries(
		context.Background(),
		daempaths.Paths{ManifestRoot: projectRoot},
		[]desiredskill.Skill{skill},
		selection,
		nil,
		skillDiscoveryObserver{stat: func(path string) (fs.FileInfo, error) {
			calls++
			if strings.Contains(path, filepath.Join(".agents", "skills", "review")) {
				return nil, os.ErrPermission
			}
			return nil, fs.ErrNotExist
		}},
	)

	if calls != 2 {
		t.Fatalf("stat calls = %d, want selected path plus one alternate modeled path", calls)
	}
	if len(found) != 1 || found[0].code != skillDiscoveryObservationFailedCode {
		t.Fatalf("findings = %#v, want one bounded observation failure", found)
	}
	if !errors.Is(found[0].cause, os.ErrPermission) {
		t.Fatalf("cause = %v, want permission failure", found[0].cause)
	}
}

func TestRetainedSkillDiscoveryIgnoresNonDirectoryEntry(t *testing.T) {
	projectRoot := t.TempDir()
	skill := discoveryTestSkill(t, projectRoot, "review", targetpkg.TargetPi, targetpkg.ScopeProject, "")
	selection := discoveryTestSelection(t, targetpkg.TargetPi)
	testkitPath := filepath.Join(projectRoot, ".agents", "skills", "review")
	if err := os.MkdirAll(filepath.Dir(testkitPath), 0o755); err != nil {
		t.Fatalf("create discovery root: %v", err)
	}
	if err := os.WriteFile(testkitPath, []byte("not a skill directory"), 0o600); err != nil {
		t.Fatalf("write non-directory discovery entry: %v", err)
	}

	found := inspectRetainedSkillDiscoveries(
		context.Background(),
		daempaths.Paths{ManifestRoot: projectRoot},
		[]desiredskill.Skill{skill},
		selection,
		nil,
		skillDiscoveryObserver{stat: os.Stat},
	)
	if len(found) != 0 {
		t.Fatalf("findings = %#v, want a regular file ignored as a skill discovery entry", found)
	}
}

func TestRetainedSkillDiscoveryDoesNotCrossScopeRoots(t *testing.T) {
	projectRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skill := discoveryTestSkill(t, projectRoot, "review", targetpkg.TargetPi, targetpkg.ScopeProject, "")
	selection := discoveryTestSelection(t, targetpkg.TargetPi)
	mkdirSkillDiscovery(t, home, ".agents/skills", "review")

	found := inspectRetainedSkillDiscoveries(
		context.Background(),
		daempaths.Paths{ManifestRoot: projectRoot},
		[]desiredskill.Skill{skill},
		selection,
		nil,
		skillDiscoveryObserver{stat: os.Stat},
	)
	if len(found) != 0 {
		t.Fatalf("findings = %#v, want global same-name entry excluded from project-scope inspection", found)
	}
}

func TestRetainedSkillDiscoveryStopsBeforeObservationWhenCanceled(t *testing.T) {
	projectRoot := t.TempDir()
	skill := discoveryTestSkill(t, projectRoot, "review", targetpkg.TargetPi, targetpkg.ScopeProject, "")
	selection := discoveryTestSelection(t, targetpkg.TargetPi)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	statCalls := 0

	found := inspectRetainedSkillDiscoveries(
		ctx,
		daempaths.Paths{ManifestRoot: projectRoot},
		[]desiredskill.Skill{skill},
		selection,
		nil,
		skillDiscoveryObserver{stat: func(path string) (fs.FileInfo, error) {
			statCalls++
			return nil, fs.ErrNotExist
		}},
	)
	if len(found) != 0 || statCalls != 0 {
		t.Fatalf("findings=%#v statCalls=%d, want canceled inspection to perform no observations", found, statCalls)
	}
}

func TestRetainedSkillDiscoveryChecksWithoutSkillsDoNotReadState(t *testing.T) {
	checks := RetainedSkillDiscoveryChecks(
		context.Background(),
		daempaths.Paths{StatefilePath: t.TempDir()},
		nil,
		nil,
		targetselection.Selection{},
	)
	if len(checks) != 0 {
		t.Fatalf("checks = %#v, want no skill diagnostics or state dependency without skills", checks)
	}
}

func TestRetainedSkillDiscoveryCoversCodexLegacyAndPiCompatibleRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	codexSkill := discoveryTestSkill(t, home, "review", targetpkg.TargetCodex, targetpkg.ScopeGlobal, "")
	piSkill := discoveryTestSkill(t, home, "lint", targetpkg.TargetPi, targetpkg.ScopeGlobal, "")
	selection := discoveryTestSelection(t, targetpkg.TargetCodex, targetpkg.TargetPi)
	mkdirSkillDiscovery(t, home, ".codex/skills", "review")
	mkdirSkillDiscovery(t, home, ".agents/skills", "lint")

	found := inspectRetainedSkillDiscoveries(
		context.Background(),
		daempaths.Paths{ManifestRoot: t.TempDir()},
		[]desiredskill.Skill{piSkill, codexSkill},
		selection,
		nil,
		skillDiscoveryObserver{stat: os.Stat},
	)

	if len(found) != 2 {
		t.Fatalf("findings = %#v, want Codex legacy and Pi compatible-root duplicates", found)
	}
	if found[0].entityID.Name() != "lint" || found[1].entityID.Name() != "review" {
		t.Fatalf("finding order = %q, %q, want canonical entity ordering", found[0].entityID.Name(), found[1].entityID.Name())
	}
}

func TestRetainedSkillDiscoveryFindingProjectionsShareOwnedMessage(t *testing.T) {
	projectRoot := t.TempDir()
	skill := discoveryTestSkill(t, projectRoot, "review", targetpkg.TargetPi, targetpkg.ScopeProject, "")
	finding := newRetainedSkillDiscoveryFinding(
		skill,
		targetpkg.TargetPi,
		filepath.Join(projectRoot, ".pi", "skills", "review"),
		filepath.Join(projectRoot, ".agents", "skills", "review"),
	)

	check := finding.check()
	diagnostic := finding.diagnostic()
	if check.Detail != diagnostic.Detail || check.NextStep != diagnostic.NextStep {
		t.Fatalf("doctor/apply projections drifted: check=%#v diagnostic=%#v", check, diagnostic)
	}
	if !strings.HasPrefix(check.Name, skillDiscoveryDuplicateRetainedCode+" ") {
		t.Fatalf("check name = %q, want stable reason prefix", check.Name)
	}
	if diagnostic.Code != skillDiscoveryDuplicateRetainedCode ||
		diagnostic.EntityID != skill.ID() ||
		diagnostic.Target != targetpkg.TargetPi ||
		diagnostic.Scope != targetpkg.ScopeProject {
		t.Fatalf("diagnostic lost canonical axes: %#v", diagnostic)
	}
	if !strings.Contains(diagnostic.Detail, "will be retained") ||
		!strings.Contains(diagnostic.NextStep, "manually") {
		t.Fatalf("diagnostic lacks explicit non-action or next step: %#v", diagnostic)
	}
}

func discoveryTestSkill(
	t *testing.T,
	root string,
	name string,
	selectedTarget targetpkg.Target,
	scope targetpkg.Scope,
	installTo string,
) desiredskill.Skill {
	t.Helper()
	placements := make(map[targetpkg.Target]desiredskill.TargetPlacement)
	if installTo != "" {
		placement, err := desiredskill.NewTargetPlacement(scope, installTo)
		if err != nil {
			t.Fatalf("build target placement: %v", err)
		}
		placements[selectedTarget] = placement
	}
	return testfixture.Skill(t, desiredskill.Spec{
		Name:         name,
		Source:       sourcetest.Local(t, filepath.Join(root, "sources", name), sourcepkg.LocalSourceModeVendor),
		Targets:      []targetpkg.Target{selectedTarget},
		Placements:   placements,
		Scope:        scope,
		InstallMode:  desiredskill.InstallModeCopy,
		Portable:     scope == targetpkg.ScopeGlobal,
		CompatRepair: false,
	})
}

func discoveryTestSelection(t *testing.T, targets ...targetpkg.Target) targetselection.Selection {
	t.Helper()
	values := make([]string, 0, len(targets))
	for _, selectedTarget := range targets {
		values = append(values, string(selectedTarget))
	}
	selection, err := targetselection.ForDiagnostics(values)
	if err != nil {
		t.Fatalf("build target selection: %v", err)
	}
	return selection
}

func mkdirSkillDiscovery(t *testing.T, root string, relativeRoot string, name string) string {
	t.Helper()
	destination := filepath.Join(root, filepath.FromSlash(relativeRoot), name)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("create skill discovery %s: %v", destination, err)
	}
	return destination
}
