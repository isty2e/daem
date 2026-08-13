package instructions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	"github.com/isty2e/daem/internal/target"
)

func TestCandidatesImportsCodexProjectInstructionBytes(t *testing.T) {
	tempDir := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	content := []byte("# Agents\n\nKeep this exact.\n")
	if err := os.WriteFile("AGENTS.md", content, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSkipped(skipped, "AGENTS.override.md", "missing") {
		t.Fatalf("skipped = %#v, want missing Codex override", skipped)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want one", sources)
	}
	source := sources[0]
	if source.ResourceName != "codex_project" || source.LivePath != "AGENTS.md" {
		t.Fatalf("source identity = %#v", source)
	}
	if string(source.Content) != string(content) {
		t.Fatalf("source content = %q, want exact %q", source.Content, content)
	}
	if source.SourcePath != filepath.Join(sourceDirectory.Root(), "instructions", "codex-project.md") {
		t.Fatalf("source path = %q", source.SourcePath)
	}
}

func TestCandidatesImportsOpenCodeProjectInstructionFromSurfaceRows(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	content := []byte("OpenCode project guidance.\n")
	if err := os.WriteFile("AGENTS.md", content, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetOpenCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if hasSkipped(skipped, "AGENTS.md", "missing") {
		t.Fatalf("skipped = %#v, did not want AGENTS.md missing", skipped)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want one", sources)
	}
	source := sources[0]
	if source.ResourceName != "opencode_project" || source.LivePath != "AGENTS.md" || source.RenderTo != "" {
		t.Fatalf("source = %#v, want opencode project default", source)
	}
	if string(source.Content) != string(content) {
		t.Fatalf("source content = %q, want exact %q", source.Content, content)
	}
	if source.SourcePath != filepath.Join(sourceDirectory.Root(), "instructions", "opencode-project.md") {
		t.Fatalf("source path = %q", source.SourcePath)
	}
}

func TestCandidatesFallsBackToDiscoveryInstructionWithoutRenderDestination(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile("CLAUDE.md", []byte("fallback guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetOpenCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSkipped(skipped, "AGENTS.md", "missing") {
		t.Fatalf("skipped = %#v, want missing default AGENTS.md", skipped)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want one", sources)
	}
	source := sources[0]
	if source.ResourceName != "opencode_project" || source.LivePath != "CLAUDE.md" || source.RenderTo != "" {
		t.Fatalf("source = %#v, want discovery fallback imported as default source evidence", source)
	}
}

func TestCandidatesImportsAntigravityAlternatePlacementWithRenderTo(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile("GEMINI.md", []byte("gemini guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetAntigravityCLI, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSkipped(skipped, "AGENTS.md", "missing") {
		t.Fatalf("skipped = %#v, want missing default AGENTS.md", skipped)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want one alternate placement source", sources)
	}
	source := sources[0]
	if source.ResourceName != "antigravity_cli_project_gemini" ||
		source.LivePath != "GEMINI.md" ||
		source.RenderTo != "GEMINI.md" {
		t.Fatalf("source = %#v, want GEMINI.md render_to-preserving source", source)
	}
	if source.SourcePath != filepath.Join(sourceDirectory.Root(), "instructions", "antigravity-cli-project-gemini.md") {
		t.Fatalf("source path = %q", source.SourcePath)
	}
}

func TestCandidatesImportsAntigravityDefaultAndAlternatePlacements(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile("AGENTS.md", []byte("agents guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("GEMINI.md", []byte("gemini guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetAntigravityCLI, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", skipped)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v, want default and alternate placement", sources)
	}
	if sources[0].ResourceName != "antigravity_cli_project" || sources[0].RenderTo != "" {
		t.Fatalf("default source = %#v", sources[0])
	}
	if sources[1].ResourceName != "antigravity_cli_project_gemini" || sources[1].RenderTo != "GEMINI.md" {
		t.Fatalf("alternate source = %#v", sources[1])
	}
}

func TestCandidatesReportsClassifyOnlyRuntimeInstructionWhenPresent(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile("CLAUDE.local.md", []byte("local runtime guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetClaudeCode, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 0 {
		t.Fatalf("sources = %#v, want no imports from runtime row", sources)
	}
	if !hasSkipped(skipped, "CLAUDE.local.md", "instruction_classify_only") {
		t.Fatalf("skipped = %#v, want classify-only runtime skip", skipped)
	}
}

func TestCandidatesUsesCodexOverridePrecedenceFromSurfacePriority(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile("AGENTS.md", []byte("default guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("AGENTS.override.md", []byte("override guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", skipped)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want one", sources)
	}
	source := sources[0]
	if source.ResourceName != "codex_project" || source.LivePath != "AGENTS.override.md" || string(source.Content) != "override guidance\n" {
		t.Fatalf("source = %#v, want override source selected by priority", source)
	}
}

func TestCandidatesSkipsEmptyCodexOverrideAndFallsBackToDefault(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDirectory(t, tempDir)
	if err := os.WriteFile("AGENTS.override.md", []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("AGENTS.md", []byte("default guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSkipped(skipped, "AGENTS.override.md", "empty_instruction_file") {
		t.Fatalf("skipped = %#v, want empty override skip", skipped)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want default fallback", sources)
	}
	if sources[0].LivePath != "AGENTS.md" || string(sources[0].Content) != "default guidance\n" {
		t.Fatalf("source = %#v, want default fallback content", sources[0])
	}
}

func TestCandidatesUsesCodexHomeForGlobalInstructionRows(t *testing.T) {
	tempDir := t.TempDir()
	codexHome := filepath.Join(tempDir, "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	withWorkingDirectory(t, tempDir)
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(codexHome, "AGENTS.md")
	if err := os.WriteFile(livePath, []byte("global guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetCodex, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSkipped(skipped, filepath.Join(codexHome, "AGENTS.override.md"), "missing") {
		t.Fatalf("skipped = %#v, want missing CODEX_HOME override", skipped)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want one", sources)
	}
	if sources[0].ResourceName != "codex_global" || sources[0].LivePath != livePath {
		t.Fatalf("source = %#v, want CODEX_HOME global source", sources[0])
	}
}

func TestCandidatesImportsAntigravityGlobalDefaultPlacement(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	livePath := filepath.Join(homeDir, ".gemini", "GEMINI.md")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, []byte("global antigravity guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetAntigravityCLI, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", skipped)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v, want one global placement source", sources)
	}
	source := sources[0]
	if source.ResourceName != "antigravity_cli_global" ||
		source.LivePath != livePath ||
		source.RenderTo != "" {
		t.Fatalf("source = %#v, want default global source without render_to", source)
	}
	if source.SourcePath != filepath.Join(sourceDirectory.Root(), "instructions", "antigravity-cli-global.md") {
		t.Fatalf("source path = %q", source.SourcePath)
	}
}

func TestCandidatesIgnoresAntigravityConfigInstructionPaths(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(filepath.Join(homeDir, ".gemini", "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "GEMINI.md"} {
		if err := os.WriteFile(filepath.Join(homeDir, ".gemini", "config", name), []byte("config guidance\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sourceDirectory := testSourceDirectory(t, filepath.Join(tempDir, "daem.d"))

	sources, skipped, err := Candidates(context.Background(), sourceDirectory, target.TargetAntigravityCLI, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 0 {
		t.Fatalf("sources = %#v, want none from non-admitted config paths", sources)
	}
	if !hasSkipped(skipped, filepath.Join(homeDir, ".gemini", "GEMINI.md"), "missing") {
		t.Fatalf("skipped = %#v, want missing admitted global path only", skipped)
	}
}

func TestInstructionLocationPathPreservesProfilePathDomains(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	homePath, err := instructionLocationPath("~/agent/AGENTS.md")
	if err != nil {
		t.Fatalf("instructionLocationPath(home) error = %v", err)
	}
	if want := filepath.Join(home, "agent", "AGENTS.md"); homePath != want {
		t.Fatalf("instructionLocationPath(home) = %q, want %q", homePath, want)
	}

	relativePath, err := instructionLocationPath(".agents/AGENTS.md")
	if err != nil {
		t.Fatalf("instructionLocationPath(relative) error = %v", err)
	}
	if want := filepath.FromSlash(".agents/AGENTS.md"); relativePath != want {
		t.Fatalf("instructionLocationPath(relative) = %q, want %q", relativePath, want)
	}

	absoluteInput := filepath.Join(string(os.PathSeparator), "tmp", "nested", "..", "AGENTS.md")
	absolutePath, err := instructionLocationPath(absoluteInput)
	if err != nil {
		t.Fatalf("instructionLocationPath(absolute) error = %v", err)
	}
	if want := filepath.Clean(absoluteInput); absolutePath != want {
		t.Fatalf("instructionLocationPath(absolute) = %q, want %q", absolutePath, want)
	}
}

func TestInstructionLocationPathReportsUnavailableHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	if _, err := instructionLocationPath("~/AGENTS.md"); err == nil {
		t.Fatal("instructionLocationPath() succeeded without a home directory")
	}
}

func TestClassifyOnlyInstructionSkipTreatsDanglingSymlinkAsPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	root := t.TempDir()
	link := filepath.Join(root, "AGENTS.md")
	if err := os.Symlink(filepath.Join(root, "missing"), link); err != nil {
		t.Fatal(err)
	}

	skip, ok, err := classifyOnlyInstructionSkip(link)
	if err != nil {
		t.Fatalf("classifyOnlyInstructionSkip() error = %v", err)
	}
	if !ok || skip.LivePath != link || skip.Reason != importInstructionSkipClassifyOnly {
		t.Fatalf("classifyOnlyInstructionSkip() = (%#v, %v), want dangling link classified", skip, ok)
	}
}

func TestReadInstructionImportContentRejectsUnsafeFileShapes(t *testing.T) {
	root := t.TempDir()

	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, skip, err := readInstructionImportContent(context.Background(), directory); err != nil || skip.Reason != importInstructionSkipNotRegular {
		t.Fatalf("directory read = (%#v, %v), want %q skip", skip, err, importInstructionSkipNotRegular)
	}

	targetPath := filepath.Join(root, "actual.md")
	if err := os.WriteFile(targetPath, []byte("guidance\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "link.md")
	if err := os.Symlink(targetPath, symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, skip, err := readInstructionImportContent(context.Background(), symlink); err != nil || skip.Reason != importInstructionSkipSymlink {
		t.Fatalf("symlink read = (%#v, %v), want %q skip", skip, err, importInstructionSkipSymlink)
	}

	oversized := filepath.Join(root, "oversized.md")
	file, err := os.OpenFile(oversized, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(directfile.MaximumBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, skip, err := readInstructionImportContent(context.Background(), oversized); err != nil || skip.Reason != importInstructionSkipTooLarge {
		t.Fatalf("oversized read = (%#v, %v), want %q skip", skip, err, importInstructionSkipTooLarge)
	}

	changed, ok := instructionSnapshotSkip(targetPath, filesnapshot.ErrChanged)
	if !ok || changed.Reason != importInstructionSkipChanged {
		t.Fatalf("changed mapping = (%#v, %t), want %q", changed, ok, importInstructionSkipChanged)
	}
}

func TestCandidatesStopsWhenInstructionImportContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sources, skipped, err := Candidates(
		ctx,
		testSourceDirectory(t, filepath.Join(t.TempDir(), "daem.d")),
		target.TargetCodex,
		target.ScopeProject,
	)
	if !errors.Is(err, context.Canceled) || sources != nil || skipped != nil {
		t.Fatalf("Candidates = (%#v, %#v, %v), want context cancellation", sources, skipped, err)
	}
}

func withWorkingDirectory(t *testing.T, directory string) {
	t.Helper()

	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}

func hasSkipped(skipped []adopt.Skipped, livePath string, reason string) bool {
	for _, item := range skipped {
		if item.LivePath == livePath && item.Reason == reason {
			return true
		}
	}
	return false
}

func testSourceDirectory(t *testing.T, root string) adopt.SourceDirectory {
	t.Helper()
	directory, err := adopt.NewSourceDirectory(filepath.Join(filepath.Dir(root), "daem.toml"), root)
	if err != nil {
		t.Fatalf("NewSourceDirectory returned error: %v", err)
	}
	return directory
}
