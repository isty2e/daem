package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunImportYesEmitsSkillGroupForCompatibleGlobalSkills(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	alphaSkill := filepath.Join(homeDir, ".codex", "skills", "alpha")
	betaSkill := filepath.Join(homeDir, ".codex", "skills", "beta")
	testkit.WriteFile(t, alphaSkill, "SKILL.md", "---\nname: alpha\ndescription: Alpha\n---\n")
	testkit.WriteFile(t, betaSkill, "SKILL.md", "---\nname: beta\ndescription: Beta\n---\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "imported: 2 resources") {
		t.Fatalf("stdout = %q, want two imported resources", stdout.String())
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	if !strings.Contains(string(content), "[[skill_group]]") {
		t.Fatalf("generated manifest = %s, want skill_group", content)
	}
	if strings.Contains(string(content), "[[skill]]") {
		t.Fatalf("generated manifest = %s, want grouped skills without individual skill entries", content)
	}

	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	if len(config.Skills()) != 2 {
		t.Fatalf("skills = %#v, want two expanded skills", config.Skills())
	}
	alpha := findImportedSkill(t, config.Skills(), "alpha")
	beta := findImportedSkill(t, config.Skills(), "beta")
	alphaSource, ok := alpha.Source().Local()
	if !ok {
		t.Fatalf("alpha source = %#v, want local", alpha.Source())
	}
	betaSource, ok := beta.Source().Local()
	if !ok {
		t.Fatalf("beta source = %#v, want local", beta.Source())
	}
	if filepath.Dir(alphaSource.Path()) != filepath.Dir(betaSource.Path()) {
		t.Fatalf("group source roots differ: alpha=%q beta=%q", alphaSource.Path(), betaSource.Path())
	}
	testkit.AssertFileContent(t, filepath.Join(alphaSource.Path(), "SKILL.md"), "---\nname: alpha\ndescription: Alpha\n---\n")
	testkit.AssertFileContent(t, filepath.Join(betaSource.Path(), "SKILL.md"), "---\nname: beta\ndescription: Beta\n---\n")

	otherDir := filepath.Join(tempDir, "other")
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	testkit.WithWorkingDirectory(t, otherDir)
	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	lockExitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", outputPath, "--dry-run"}, &lockStdout, &lockStderr)
	if lockExitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q, stdout = %q", lockExitCode, lockStderr.String(), lockStdout.String())
	}
}

func TestRunImportYesCoalescesSameContentGlobalSkillsAcrossTargets(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	skillContent := "---\nname: review\ndescription: Review code\n---\n"
	codexSkill := filepath.Join(homeDir, ".codex", "skills", "review")
	claudeSkill := filepath.Join(homeDir, ".claude", "skills", "review")
	testkit.WriteFile(t, codexSkill, "SKILL.md", skillContent)
	testkit.WriteFile(t, claudeSkill, "SKILL.md", skillContent)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	copiedSkill := filepath.Join(tempDir, "daem.imported.d", "skills", "review", strings.ReplaceAll(testkit.HashDirectory(t, codexSkill), ":", "-"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--target", "claude-code", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "imported: 1 resources") {
		t.Fatalf("stdout = %q, want one coalesced resource", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(copiedSkill, "SKILL.md"), skillContent)

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	if strings.Contains(string(content), "install_name") {
		t.Fatalf("generated manifest = %s, want no install_name field", content)
	}
	if len(config.Skills()) != 1 {
		t.Fatalf("skills = %#v, want one coalesced skill", config.Skills())
	}
	skill := findImportedSkill(t, config.Skills(), "review")
	if skill.InstallName() != "review" {
		t.Fatalf("InstallName = %q, want review", skill.InstallName())
	}
	assertSkillLocalSource(t, skill, filepath.ToSlash(copiedSkill))
	targets := skill.Targets()
	if len(targets) != 2 || targets[0] != target.TargetCodex || targets[1] != target.TargetClaudeCode {
		t.Fatalf("Targets = %#v, want codex and claude-code", targets)
	}
}

func TestRunImportYesPreservesDivergentSameNameGlobalSkillsAcrossTargets(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	codexSkill := filepath.Join(homeDir, ".codex", "skills", "review")
	claudeSkill := filepath.Join(homeDir, ".claude", "skills", "review")
	testkit.WriteFile(t, codexSkill, "SKILL.md", "---\nname: review\ndescription: Review code for Codex\n---\n")
	testkit.WriteFile(t, claudeSkill, "SKILL.md", "---\nname: review\ndescription: Review code for Claude\n---\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	codexCopy := filepath.Join(tempDir, "daem.imported.d", "skills", "review", strings.ReplaceAll(testkit.HashDirectory(t, codexSkill), ":", "-"))
	claudeCopy := filepath.Join(tempDir, "daem.imported.d", "skills", "review", strings.ReplaceAll(testkit.HashDirectory(t, claudeSkill), ":", "-"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--target", "claude-code", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "imported: 2 resources") {
		t.Fatalf("stdout = %q, want two divergent resources", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(codexCopy, "SKILL.md"), "---\nname: review\ndescription: Review code for Codex\n---\n")
	testkit.AssertFileContent(t, filepath.Join(claudeCopy, "SKILL.md"), "---\nname: review\ndescription: Review code for Claude\n---\n")

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	if !strings.Contains(string(content), `id = "codex_global_review"`) || !strings.Contains(string(content), `id = "claude_code_global_review"`) {
		t.Fatalf("generated manifest = %s, want divergent skill ids", content)
	}
	if strings.Contains(string(content), "[[skill_group]]") {
		t.Fatalf("generated manifest = %s, want divergent same-name skills as individual entries", content)
	}
	if strings.Contains(string(content), "install_name") {
		t.Fatalf("generated manifest = %s, want no install_name field", content)
	}
	if len(config.Skills()) != 2 {
		t.Fatalf("skills = %#v, want two divergent skills", config.Skills())
	}
	codexImported := findImportedSkill(t, config.Skills(), "codex_global_review")
	if codexImported.InstallName() != "review" {
		t.Fatalf("codex InstallName = %q, want review", codexImported.InstallName())
	}
	assertSkillLocalSource(t, codexImported, filepath.ToSlash(codexCopy))
	codexTargets := codexImported.Targets()
	if len(codexTargets) != 1 || codexTargets[0] != target.TargetCodex {
		t.Fatalf("codex targets = %#v, want codex", codexTargets)
	}
	claudeImported := findImportedSkill(t, config.Skills(), "claude_code_global_review")
	if claudeImported.InstallName() != "review" {
		t.Fatalf("claude InstallName = %q, want review", claudeImported.InstallName())
	}
	assertSkillLocalSource(t, claudeImported, filepath.ToSlash(claudeCopy))
	claudeTargets := claudeImported.Targets()
	if len(claudeTargets) != 1 || claudeTargets[0] != target.TargetClaudeCode {
		t.Fatalf("claude targets = %#v, want claude-code", claudeTargets)
	}

	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	lockExitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", outputPath, "--dry-run"}, &lockStdout, &lockStderr)
	if lockExitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q, stdout = %q", lockExitCode, lockStderr.String(), lockStdout.String())
	}
}

func TestRunImportDryRunReportsSkillSkipsAndDuplicates(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, filepath.Join(homeDir, ".agents", "skills", "alpha"), "SKILL.md", "---\nname: alpha\ndescription: Alpha\n---\n")
	testkit.WriteFile(t, filepath.Join(homeDir, ".codex", "skills", "alpha"), "SKILL.md", "---\nname: alpha\ndescription: Duplicate\n---\n")
	testkit.WriteFile(t, filepath.Join(homeDir, ".codex", "skills", "missing-skill"), "README.md", "no skill\n")
	testkit.WriteFile(t, filepath.Join(homeDir, ".codex", "skills"), "not-a-dir", "file\n")
	testkit.WriteFile(t, filepath.Join(homeDir, ".codex", "skills", ".system"), "SKILL.md", "---\nname: system\ndescription: System\n---\n")
	cacheSkill := filepath.Join(homeDir, ".codex", "plugins", "cache", "bundle", "skills", "cache-skill")
	testkit.WriteFile(t, cacheSkill, "SKILL.md", "---\nname: cache-skill\ndescription: Cache\n---\n")
	if err := os.Symlink(cacheSkill, filepath.Join(homeDir, ".codex", "skills", "cache-skill")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"import: 1 resources",
		`scan resource="skill-root" target=codex scope=global live="` + filepath.Join(homeDir, ".agents", "skills") + `" status=scanned entries=1 imported=1 skipped=0`,
		`scan resource="skill-root" target=codex scope=global live="` + filepath.Join(homeDir, ".codex", "skills") + `" status=no_importable_entries entries=5 imported=0 skipped=5`,
		`resource="skill/alpha"`,
		"reason=conflicting_skill_name",
		`detail="conflicts_with=` + filepath.Join(homeDir, ".agents", "skills", "alpha") + `"`,
		"action_hint=resolve_conflict",
		"reason=missing_skill_md",
		"reason=skill_not_directory",
		"reason=supplied_skill_entry",
		"reason=supplied_plugin_cache_skill",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
}

func TestRunImportReportsEmptyAndNoImportableSkillRoots(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", "")
	testkit.WithWorkingDirectory(t, tempDir)
	if err := os.MkdirAll(filepath.Join(homeDir, ".agents", "skills"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	testkit.WriteFile(t, filepath.Join(homeDir, ".codex", "skills", "missing-skill"), "README.md", "no skill\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"nothing to import",
		filepath.Join(homeDir, ".agents", "skills") + ": empty entries=0 imported=0 skipped=0",
		filepath.Join(homeDir, ".codex", "skills") + ": no_importable_entries entries=1 imported=0 skipped=1",
		"missing_skill_md",
		"next: verify that the selected --target and --scope have live agent files to import",
		"next: try another selection, such as --scope global or a different --target",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportYesReportsNestedSkillSymlinkWithoutDestination(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	skillRoot := filepath.Join(homeDir, ".codex", "skills", "unsafe")
	testkit.WriteFile(t, skillRoot, "SKILL.md", "---\nname: unsafe\ndescription: Unsafe\n---\n")
	testkit.WriteFile(t, skillRoot, "payload.txt", "payload\n")
	if err := os.Symlink(filepath.Join(skillRoot, "payload.txt"), filepath.Join(skillRoot, "z-link")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, want failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), ": nested_symlink") {
		t.Fatalf("stderr = %q, want nested symlink skip diagnostic", stderr.String())
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d", "skills", "unsafe"))
}
