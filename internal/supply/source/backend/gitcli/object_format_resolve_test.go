package gitcli

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
)

func TestResolveGitSHA256CommitAndBranch(t *testing.T) {
	requireSHA256Git(t)
	tempDir := t.TempDir()
	repoPath := initSHA256GitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	if len(commit) != 64 {
		t.Fatalf("SHA-256 commit length = %d, want 64", len(commit))
	}

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	for _, ref := range []string{commit, "main"} {
		t.Run(ref, func(t *testing.T) {
			sourceSpec := mustGitSource(t, repoPath, "skills/demo", ref)
			resolution, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", ref, err)
			}
			wantSourceID, err := source.SourceIDFor(sourceSpec)
			if err != nil {
				t.Fatalf("SourceIDFor returned error: %v", err)
			}
			assertGitResolutionIdentity(
				t,
				resolution,
				wantSourceID,
				artifact.ResolvedRef(commit),
				artifact.ArtifactKindDirectory,
			)
			if _, err := os.Stat(resolver.repositoryCachePath(repoPath, gitObjectFormatSHA256)); err != nil {
				t.Fatalf("SHA-256 cache path: %v", err)
			}
			if _, err := os.Stat(resolver.repositoryPath(repoPath)); !os.IsNotExist(err) {
				t.Fatalf("SHA-1 locator-only cache path exists for SHA-256 origin: %v", err)
			}
		})
	}
}

func TestResolveRejectsEmptyNetworkSymbolicWithoutCreatingCache(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	realGit, err := exec.LookPath(gitExecutable)
	if err != nil {
		t.Fatalf("LookPath returned error: %v", err)
	}
	fakeGit := "#!/bin/sh\n" + detectGitSubcommandShell + `
if [ "$git_subcommand" = "ls-remote" ]; then
  exit 0
fi
exec ` + shellQuoteForTest(realGit) + ` "$@"
`
	if err := os.WriteFile(filepath.Join(binDir, gitExecutable), []byte(fakeGit), 0o700); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	locator := "https://example.invalid/acme/skills.git"
	_, err = resolver.Resolve(context.Background(), mustGitSource(t, locator, ".", "main"), noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "unobservable") {
		t.Fatalf("Resolve error = %v, want unobservable object format", err)
	}
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
}

func TestResolveDoesNotGuessSHA1WhenGITDefaultHashIsSHA256(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	t.Setenv("GIT_DEFAULT_HASH", "sha256")
	if _, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, repoPath, "skills/demo", "main"),
		noOperationOptions,
	); err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	cachePath := resolver.repositoryPath(repoPath)
	format := strings.TrimSpace(runGitTestCommand(t, cachePath, "rev-parse", "--show-object-format"))
	if format != string(gitObjectFormatSHA1) {
		t.Fatalf("cache object format = %q, want sha1 despite GIT_DEFAULT_HASH", format)
	}
}

func TestResolveRejectsCommitWidthDisagreeingWithLocalFormat(t *testing.T) {
	requireSHA256Git(t)
	tempDir := t.TempDir()
	repoPath := initSHA256GitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	_, err = resolver.Resolve(
		context.Background(),
		mustGitSource(t, repoPath, "skills/demo", strings.Repeat("a", 40)),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match the declared commit id") {
		t.Fatalf("Resolve error = %v, want commit/object-format disagreement", err)
	}
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
}

func TestResolveReusesLocatorOnlySHA1Cache(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	sourceSpec := mustGitSource(t, repoPath, "skills/demo", "main")
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	cachePath := resolver.repositoryPath(repoPath)
	first, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	second, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if !os.SameFile(first, second) {
		t.Fatal("SHA-1 cache path was recreated instead of reused")
	}
}

func requireSHA256Git(t *testing.T) {
	t.Helper()
	requireGit(t)

	command := exec.Command(gitExecutable, "init", "--bare", "--quiet", "--object-format=sha256")
	command.Dir = t.TempDir()
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("git SHA-256 repositories are unavailable: %v\n%s", err, output)
	}
}

func initSHA256GitRepository(t *testing.T, root string) string {
	t.Helper()

	repoPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	runGitTestCommand(t, repoPath, "init", "--object-format=sha256")
	runGitTestCommand(t, repoPath, "checkout", "-b", "main")
	runGitTestCommand(t, repoPath, "config", "user.email", "daem@example.invalid")
	runGitTestCommand(t, repoPath, "config", "user.name", "Agent Env Test")
	return repoPath
}

func assertNoRepositoryCaches(t *testing.T, reposRoot string) {
	t.Helper()

	entries, err := os.ReadDir(reposRoot)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("repository caches = %v, want none", names)
	}
}

func TestFileURLLocalSHA256UsesLocalFormatAuthority(t *testing.T) {
	requireSHA256Git(t)
	tempDir := t.TempDir()
	repoPath := initSHA256GitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	fileLocator := (&url.URL{Scheme: "file", Path: filepath.ToSlash(repoPath)}).String()

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolution, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, fileLocator, "skills/demo", "main"),
		noOperationOptions,
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolution.Identity().ResolvedRef() != artifact.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", resolution.Identity().ResolvedRef(), commit)
	}
}

func TestResolveRejectsAmbientURLRewriteBeforeNetworkFormatProbe(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	declared := "https://example.invalid/acme/skills.git"
	attacker := "https://attacker.invalid/acme/skills.git"
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+attacker+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", declared)

	logPath := installLoggingGitWrapperBlockingTransport(t)
	_, err = resolver.Resolve(
		context.Background(),
		mustGitSource(t, declared, ".", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "effective") {
		t.Fatalf("Resolve error = %v, want effective-origin rejection", err)
	}
	assertGitSubcommandsAbsent(t, logPath, "ls-remote", "fetch", "archive")
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
	assertNoObservationProbes(t, filepath.Join(tempDir, "cache", "probes"))
}

func TestResolveRejectsUnboundedRemoteRefAdvertisementWithoutCreatingCache(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolver.state.testRemoteRefAdvertisementBudget = &remoteRefAdvertisementBudget{
		maxBytes:     defaultRemoteRefAdvertisementBytes,
		maxRecords:   2,
		maxLineBytes: defaultRemoteRefAdvertisementLine,
	}

	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	realGit, err := exec.LookPath(gitExecutable)
	if err != nil {
		t.Fatalf("LookPath returned error: %v", err)
	}
	sha1 := strings.Repeat("a", 40)
	fakeGit := "#!/bin/sh\n" + detectGitSubcommandShell + `
if [ "$git_subcommand" = "ls-remote" ]; then
  printf '%s\trefs/heads/one\n' ` + shellQuoteForTest(sha1) + `
  printf '%s\trefs/heads/two\n' ` + shellQuoteForTest(sha1) + `
  printf '%s\trefs/heads/three\n' ` + shellQuoteForTest(sha1) + `
  exit 0
fi
exec ` + shellQuoteForTest(realGit) + ` "$@"
`
	if err := os.WriteFile(filepath.Join(binDir, gitExecutable), []byte(fakeGit), 0o700); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err = resolver.Resolve(
		context.Background(),
		mustGitSource(t, "https://example.invalid/acme/skills.git", ".", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds 2 records") {
		t.Fatalf("Resolve error = %v, want advertisement record ceiling", err)
	}
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
}

func TestResolveSHA1WhenGitLacksObjectFormatOption(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	installGitWrapperRejectingObjectFormat(t)

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	sourceSpec := mustGitSource(t, repoPath, "skills/demo", "main")
	resolution, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolution.Identity().ResolvedRef() != artifact.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", resolution.Identity().ResolvedRef(), commit)
	}
}

func TestResolveRejectsSHA256WhenGitLacksObjectFormatOption(t *testing.T) {
	requireSHA256Git(t)
	tempDir := t.TempDir()
	repoPath := initSHA256GitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commitAll(t, repoPath, "initial skill")
	installGitWrapperRejectingObjectFormat(t)

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	_, err = resolver.Resolve(
		context.Background(),
		mustGitSource(t, repoPath, "skills/demo", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "supports --object-format") {
		t.Fatalf("Resolve error = %v, want SHA-256 capability rejection", err)
	}
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
}

func TestResolveDoesNotFollowEnclosingRepositoryURLRewrite(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	project := filepath.Join(tempDir, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	runGitTestCommand(t, project, "init")

	attacker := gitRepositoryWithSkill(t, filepath.Join(tempDir, "attacker"), "attacker")
	declared := "https://127.0.0.1:1/acme/skills.git"
	runGitTestCommand(t, project, "config", "url."+fileURLForTest(attacker)+".insteadOf", declared)

	cacheRoot := filepath.Join(project, ".daem", "cache", "sources")
	resolver, err := NewResolver(cacheRoot)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	_, err = resolver.Resolve(
		context.Background(),
		mustGitSource(t, declared, ".", "main"),
		noOperationOptions,
	)
	if err == nil {
		t.Fatal("Resolve succeeded, want failure without rewritten origin contact")
	}
	assertNoRepositoryCaches(t, filepath.Join(cacheRoot, "repos"))
	assertNoObservationProbes(t, filepath.Join(cacheRoot, "probes"))
}

func TestResolveSHA1WhenGitLacksObjectFormatOptionAndHEADIsMissing(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	runGitTestCommand(t, repoPath, "symbolic-ref", "HEAD", "refs/heads/missing")
	installGitWrapperRejectingObjectFormat(t)

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolution, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, repoPath, "skills/demo", "main"),
		noOperationOptions,
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolution.Identity().ResolvedRef() != artifact.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", resolution.Identity().ResolvedRef(), commit)
	}
}

func TestResolveSHA1WhenGitLacksObjectFormatOptionAndGITDefaultHashIsSHA256(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	installGitWrapperRejectingObjectFormat(t)
	t.Setenv("GIT_DEFAULT_HASH", "sha256")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolution, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, repoPath, "skills/demo", "main"),
		noOperationOptions,
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolution.Identity().ResolvedRef() != artifact.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", resolution.Identity().ResolvedRef(), commit)
	}
	config, err := os.ReadFile(filepath.Join(resolver.repositoryPath(repoPath), "config"))
	if err != nil {
		t.Fatalf("read cache config: %v", err)
	}
	if strings.Contains(strings.ToLower(string(config)), "sha256") {
		t.Fatalf("legacy SHA-1 cache used sha256 despite GIT_DEFAULT_HASH: %s", config)
	}
}

func TestResolveSHA1WhenGitLacksObjectFormatOptionAndDefaultObjectFormatIsSHA256(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	repoPath := initGitRepository(t, tempDir)
	writeGitTestFile(t, repoPath, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")
	commit := commitAll(t, repoPath, "initial skill")
	installGitWrapperRejectingObjectFormat(t)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "init.defaultObjectFormat")
	t.Setenv("GIT_CONFIG_VALUE_0", "sha256")

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	resolution, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, repoPath, "skills/demo", "main"),
		noOperationOptions,
	)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolution.Identity().ResolvedRef() != artifact.ResolvedRef(commit) {
		t.Fatalf("ResolvedRef = %q, want %q", resolution.Identity().ResolvedRef(), commit)
	}
	config, err := os.ReadFile(filepath.Join(resolver.repositoryPath(repoPath), "config"))
	if err != nil {
		t.Fatalf("read cache config: %v", err)
	}
	if strings.Contains(strings.ToLower(string(config)), "sha256") {
		t.Fatalf("legacy SHA-1 cache used sha256 despite init.defaultObjectFormat: %s", config)
	}
}

func TestResolveRejectsLegacyLocalDirectoryWithoutRepositoryEvidence(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	notRepo := filepath.Join(tempDir, "not-repo")
	if err := os.MkdirAll(notRepo, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	installGitWrapperRejectingObjectFormat(t)

	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	_, err = resolver.Resolve(
		context.Background(),
		mustGitSource(t, notRepo, "skills/demo", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("Resolve error = %v, want local repository evidence rejection", err)
	}
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
}

func TestObservationProbeReplacementIsNotAdoptedOrRemoved(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	resolver, err := NewResolver(filepath.Join(tempDir, "cache"))
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}

	var replacement string
	resolver.state.testAfterObservationProbePublish = func(path string) {
		published := path + ".published"
		if err := os.Rename(path, published); err != nil {
			t.Fatalf("rename published probe: %v", err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("create replacement probe: %v", err)
		}
		replacement = path
		if err := os.WriteFile(filepath.Join(path, "canary"), []byte("keep\n"), 0o600); err != nil {
			t.Fatalf("write replacement canary: %v", err)
		}
	}

	_, err = resolver.Resolve(
		context.Background(),
		mustGitSource(t, "https://example.invalid/acme/skills.git", ".", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("Resolve error = %v, want observation replacement rejection", err)
	}
	if replacement == "" {
		t.Fatal("replacement probe was not published")
	}
	content, readErr := os.ReadFile(filepath.Join(replacement, "canary"))
	if readErr != nil {
		t.Fatalf("replacement canary was removed: %v", readErr)
	}
	if string(content) != "keep\n" {
		t.Fatalf("replacement canary = %q, want preserved", content)
	}
	assertNoRepositoryCaches(t, filepath.Join(tempDir, "cache", "repos"))
}

func assertNoObservationProbes(t *testing.T, probesRoot string) {
	t.Helper()

	entries, err := os.ReadDir(probesRoot)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("observation probes = %v, want none", names)
	}
}

func installLoggingGitWrapperBlockingTransport(t *testing.T) string {
	t.Helper()
	realGit, err := exec.LookPath(gitExecutable)
	if err != nil {
		t.Fatalf("resolve real git: %v", err)
	}
	binRoot := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binRoot, 0o700); err != nil {
		t.Fatalf("create logging wrapper directory: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "git-subcommands")
	wrapper := "#!/bin/sh\n" + detectGitSubcommandShell + `
printf '%s\n' "$git_subcommand" >> "$DAEM_GIT_SUBCOMMAND_LOG"
case "$git_subcommand" in
ls-remote|fetch|archive)
  exit 1
  ;;
esac
exec ` + shellQuoteForTest(realGit) + ` "$@"
`
	if err := os.WriteFile(filepath.Join(binRoot, gitExecutable), []byte(wrapper), 0o700); err != nil {
		t.Fatalf("write logging git wrapper: %v", err)
	}
	t.Setenv("PATH", binRoot+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DAEM_GIT_SUBCOMMAND_LOG", logPath)
	return logPath
}

func installGitWrapperRejectingObjectFormat(t *testing.T) {
	t.Helper()
	realGit, err := exec.LookPath(gitExecutable)
	if err != nil {
		t.Fatalf("resolve real git: %v", err)
	}
	binRoot := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binRoot, 0o700); err != nil {
		t.Fatalf("create object-format wrapper directory: %v", err)
	}
	wrapper := "#!/bin/sh\n" +
		"if [ \"$1\" = \"init\" ] && [ \"$2\" = \"-h\" ]; then\n" +
		"  printf '%s\\n' \"usage: git init\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"for git_argument in \"$@\"; do\n" +
		"  case \"$git_argument\" in\n" +
		"    --object-format=*|--show-object-format)\n" +
		"      printf '%s\\n' \"error: unknown option\" >&2\n" +
		"      exit 129\n" +
		"      ;;\n" +
		"  esac\n" +
		"done\nexec " + shellQuoteForTest(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binRoot, gitExecutable), []byte(wrapper), 0o700); err != nil {
		t.Fatalf("write object-format git wrapper: %v", err)
	}
	t.Setenv("PATH", binRoot+string(os.PathListSeparator)+os.Getenv("PATH"))
}
