package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyYesUsesDirectLockForCompatibleSkillWithCompatRepair(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	sourcePath := filepath.Join(tempDir, "skills/ast-grep")
	testkit.WriteFile(
		t,
		tempDir,
		"skills/ast-grep/SKILL.md",
		"---\nname: ast-grep\ndescription: Use for structural code search.\n---\n",
	)
	sourceHash := testkit.HashDirectory(t, sourcePath)
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "ast-grep"
source = { path = "skills/ast-grep", mode = "vendor" }
targets = ["opencode"]
compat_repair = true
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	skills := testkit.LockedSkills(t, locked)
	if len(skills) != 1 || skills[0].Repair != nil {
		t.Fatalf("locked skills = %#v, want unchanged direct lock entry", skills)
	}
	if skills[0].ContentHash != sourceHash {
		t.Fatalf("locked hash = %q, want original hash %q", skills[0].ContentHash, sourceHash)
	}
	derivation, ok := skills[0].Contract.Derivation()
	if !ok || derivation.Kind() != lock.DerivationDirectResolution {
		t.Fatalf("locked derivation = %#v, present=%t, want direct resolution", derivation, ok)
	}
	if replay := skills[0].Contract.ReplayCoverage().Derivation(); replay != lock.ReplayNotApplicable {
		t.Fatalf("locked derivation replay = %q, want %q", replay, lock.ReplayNotApplicable)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "opencode", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("apply exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(
		t,
		filepath.Join(tempDir, ".opencode/skills/ast-grep/SKILL.md"),
		"---\nname: ast-grep\ndescription: Use for structural code search.\n---\n",
	)
	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "ast-grep", "opencode", "project", ".opencode/skills/ast-grep", sourceHash)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--target", "opencode", "--check"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("status exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}

func TestRunApplyYesReplaysLockedSkillRepairRecipe(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]
compat_repair = true
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Repair == nil {
		t.Fatalf("locked skills = %#v, want repaired lock entry", testkit.LockedSkills(t, locked))
	}
	repairedHash := testkit.LockedSkills(t, locked)[0].ContentHash

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "opencode", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("apply exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".opencode/skills/oracle/SKILL.md"), "---\nname: oracle\ndescription: Use for oracle review.\n---\n")
	if testkit.HashDirectory(t, filepath.Join(tempDir, ".opencode/skills/oracle")) != repairedHash {
		t.Fatalf("installed repaired skill hash does not match lock hash")
	}
	testkit.AssertDirectoryEntryMissingExact(t, filepath.Join(tempDir, "skills/oracle"), "SKILL.md")
	testkit.AssertDirectoryEntryExistsExact(t, filepath.Join(tempDir, "skills/oracle"), "skill.md")

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "opencode", "project", ".opencode/skills/oracle", repairedHash)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--target", "opencode", "--check"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("status exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}

func TestRunApplyYesReplaysLockedSkillGroupRepairRecipe(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill_group]]
names = ["oracle"]
source = { path = "skills", mode = "vendor" }
targets = ["opencode"]
compat_repair = true
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Repair == nil {
		t.Fatalf("locked skills = %#v, want repaired group member", testkit.LockedSkills(t, locked))
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "opencode", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("apply exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".opencode/skills/oracle/SKILL.md"), "---\nname: oracle\ndescription: Use for oracle review.\n---\n")
}

func TestRunStatusReportsStaleLockWhenRepairedSkillOriginalSourceChanges(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]
compat_repair = true
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Changed after lock.\n---\n")

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--target", "opencode", "--check"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("status exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), `resource="skill/oracle"`) || !strings.Contains(stdout.String(), `reason=stale_lock`) {
		t.Fatalf("stdout = %q, want stale lock for repaired skill", stdout.String())
	}
}

func TestRunStatusFailsLockedSkillRepairPreconditionMismatch(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]
compat_repair = true
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	lockedSkill := testkit.LockedSkills(t, locked)[0].Contract
	recipe, ok := lockedSkill.RepairRecipe()
	if !ok {
		t.Fatal("locked skill has no repair recipe")
	}
	operations := recipe.Operations()
	rename, ok := operations[0].Rename()
	if !ok {
		t.Fatalf("first repair operation kind = %q, want rename", operations[0].Kind())
	}
	wrongFileHash := artifact.HashFileContent([]byte("wrong"))
	operations[0], err = skillrepair.NewRenameOperation(
		rename.From(),
		rename.To(),
		wrongFileHash,
		uint32(rename.Mode()),
	)
	if err != nil {
		t.Fatalf("NewRenameOperation returned error: %v", err)
	}
	frontmatter, ok := operations[1].SetFrontmatterString()
	if !ok {
		t.Fatalf("second repair operation kind = %q, want set_frontmatter_string", operations[1].Kind())
	}
	oldValue, hasOldValue := frontmatter.OldValue()
	var oldValuePointer *string
	if hasOldValue {
		oldValuePointer = &oldValue
	}
	operations[1], err = skillrepair.NewSetFrontmatterStringOperation(
		frontmatter.Path(),
		frontmatter.Field(),
		oldValuePointer,
		frontmatter.NewValue(),
		frontmatter.Offset(),
		frontmatter.Old(),
		frontmatter.New(),
		wrongFileHash,
		frontmatter.OutputHash(),
	)
	if err != nil {
		t.Fatalf("NewSetFrontmatterStringOperation returned error: %v", err)
	}
	tamperedRecipe, err := skillrepair.NewRecipe(recipe.Input(), recipe.Output(), operations)
	if err != nil {
		t.Fatalf("NewRecipe returned error: %v", err)
	}
	output, ok := lockedSkill.ExactSupply()
	if !ok {
		t.Fatal("locked repaired skill has no exact Supply")
	}
	locked = replaceLockedRepairContract(t, locked, lockedSkill, output, tamperedRecipe)
	testkit.WriteLockfile(t, lockfilePath, locked)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--target", "opencode", "--check"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("status exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "replay repair") || !strings.Contains(stderr.String(), "does not match expected") {
		t.Fatalf("stderr = %q, want repair precondition mismatch", stderr.String())
	}
}

func TestRunApplyYesFailsLockedSkillRepairFinalHashMismatch(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]
compat_repair = true
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	lockedSkill := testkit.LockedSkills(t, locked)[0].Contract
	recipe, ok := lockedSkill.RepairRecipe()
	if !ok {
		t.Fatal("locked skill has no repair recipe")
	}
	wrongOutputHash := artifact.HashFileContent([]byte("wrong repaired tree"))
	wrongOutput, err := artifact.NewExactIdentity(
		recipe.Output().SourceID(),
		recipe.Output().ResolvedRef(),
		recipe.Output().Kind(),
		wrongOutputHash,
	)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	tamperedRecipe, err := skillrepair.NewRecipe(recipe.Input(), wrongOutput, recipe.Operations())
	if err != nil {
		t.Fatalf("NewRecipe returned error: %v", err)
	}
	locked = replaceLockedRepairContract(t, locked, lockedSkill, wrongOutput, tamperedRecipe)
	testkit.WriteLockfile(t, lockfilePath, locked)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "opencode", "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("apply exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "replayed artifact identity does not match recipe output") {
		t.Fatalf("stderr = %q, want final hash mismatch", stderr.String())
	}
}

func replaceLockedRepairContract(
	t *testing.T,
	file lock.File,
	original lock.LockedSubjectContract,
	output artifact.ExactIdentity,
	recipe skillrepair.Recipe,
) lock.File {
	t.Helper()
	originalDerivation, ok := original.Derivation()
	if !ok {
		t.Fatal("locked repaired skill has no derivation")
	}
	transform, ok := originalDerivation.DeterministicTransform()
	if !ok {
		t.Fatalf("locked repaired skill derivation kind = %q, want deterministic transform", originalDerivation.Kind())
	}
	derivation, err := lock.NewDeterministicTransformDerivation(lock.DeterministicTransform{
		InputIdentity:          recipe.Input(),
		RecipeHash:             recipe.Hash(),
		AlgorithmID:            transform.AlgorithmID,
		AlgorithmVersion:       transform.AlgorithmVersion,
		ExecutionDomain:        transform.ExecutionDomain,
		ExpectedOutputIdentity: output,
	})
	if err != nil {
		t.Fatalf("NewDeterministicTransformDerivation returned error: %v", err)
	}
	var exactFileUse *lock.ExactFileUse
	if use, present := original.ExactFileUse(); present {
		exactFileUse = &use
	}
	var correlation *lock.SkillSetMemberCorrelation
	if value, present := original.SkillSetMemberCorrelation(); present {
		correlation = &value
	}
	replacement, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:                  original.EntityID(),
		SubjectID:                 original.SubjectID(),
		ExactSupply:               output,
		ExactFileUse:              exactFileUse,
		Derivation:                derivation,
		RepairRecipe:              &recipe,
		SkillSetMemberCorrelation: correlation,
	})
	if err != nil {
		t.Fatalf("NewExactSupplySubjectContract returned error: %v", err)
	}

	subjects := file.Locked.Subjects()
	replaced := false
	for index := range subjects {
		if subjects[index].SubjectID() != original.SubjectID() {
			continue
		}
		subjects[index] = replacement
		replaced = true
		break
	}
	if !replaced {
		t.Fatalf("locked subject %q not found", original.SubjectID())
	}
	file.Locked, err = lock.NewLockedSection(subjects)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	return file
}

func TestRunApplyYesCreatesPiGlobalSkillUnderHome(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	sourcePath := filepath.Join(tempDir, "skills", "oracle")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: portable-oracle\ndescription: Use for oracle review.\n---\n")
	skillHash := testkit.HashDirectory(t, sourcePath)

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["pi"]

[[skill]]
name = "oracle"
source = { path = "`+filepath.ToSlash(sourcePath)+`", mode = "vendor" }
targets = ["pi"]
scope = "global"
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:" + filepath.ToSlash(sourcePath) + "?mode=vendor", ContentHash: skillHash,
		Targets: []target.Target{target.TargetPi}, Scope: target.ScopeGlobal,
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "pi", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".pi/agent/skills/oracle/SKILL.md"), "---\nname: portable-oracle\ndescription: Use for oracle review.\n---\n")

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "pi", "global", "~/.pi/agent/skills/oracle", skillHash)
}
