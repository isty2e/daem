package gitcli

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/source"
)

const detectGitSubcommandShell = `git_subcommand=
git_skip_config_value=0
for git_argument in "$@"; do
  if [ "$git_skip_config_value" = 1 ]; then
    git_skip_config_value=0
    continue
  fi
  case "$git_argument" in
    -c)
      git_skip_config_value=1
      ;;
    -C)
      git_skip_config_value=1
      ;;
    --no-replace-objects|--git-dir=*)
      ;;
    *)
      git_subcommand=$git_argument
      break
      ;;
  esac
done
`

func TestNewResolverRejectsSelectedCacheRootSymlink(t *testing.T) {
	tempDir := t.TempDir()
	external := filepath.Join(tempDir, "external-cache")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatalf("create external cache root: %v", err)
	}
	selected := filepath.Join(tempDir, "selected-cache")
	if err := os.Symlink(external, selected); err != nil {
		t.Fatalf("create selected cache-root symlink: %v", err)
	}

	if _, err := NewResolver(selected); err == nil || !strings.Contains(err.Error(), "symbolic-link") {
		t.Fatalf("NewResolver error = %v, want selected cache-root symlink rejection", err)
	}
}

func TestResolveRejectsSymlinkedRepositoryCacheBeforeFetchOrArchive(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	attackerOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "attacker"), "attacker")

	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	cachePath := resolver.repositoryPath(trustedOrigin)
	attackerCache := filepath.Join(tempDir, "attacker-cache")
	cloneGitRepository(t, attackerOrigin, attackerCache, false)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("create cache parent: %v", err)
	}
	if err := os.Symlink(attackerCache, cachePath); err != nil {
		t.Fatalf("preseed symlinked repository cache: %v", err)
	}

	logPath := installLoggingGitWrapper(t)
	_, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, trustedOrigin, "skills/demo", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "cache") {
		t.Fatalf("Resolve error = %v, want cache authority rejection", err)
	}
	assertGitSubcommandsAbsent(t, logPath, "fetch", "archive")
}

func TestResolveRejectsRepositoryCacheBelowSymlinkedAncestor(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	attackerOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "attacker"), "attacker")

	cacheRoot := filepath.Join(tempDir, "cache")
	resolver := mustGitResolver(t, cacheRoot)
	externalRepos := filepath.Join(tempDir, "external-repos")
	if err := os.MkdirAll(externalRepos, 0o700); err != nil {
		t.Fatalf("create external repository cache: %v", err)
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		t.Fatalf("create cache root: %v", err)
	}
	if err := os.Symlink(externalRepos, filepath.Join(cacheRoot, "repos")); err != nil {
		t.Fatalf("preseed symlinked cache ancestor: %v", err)
	}
	cloneGitRepository(t, attackerOrigin, filepath.Join(externalRepos, filepath.Base(resolver.repositoryPath(trustedOrigin))), false)

	logPath := installLoggingGitWrapper(t)
	_, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, trustedOrigin, "skills/demo", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "cache") {
		t.Fatalf("Resolve error = %v, want cache ancestor rejection", err)
	}
	assertGitSubcommandsAbsent(t, logPath, "fetch", "archive")
}

func TestResolveRejectsSymlinkedCacheNamespacesBeforeGitOrLockEffects(t *testing.T) {
	requireGit(t)
	for _, namespace := range []string{"artifacts", "locks"} {
		t.Run(namespace, func(t *testing.T) {
			tempDir := t.TempDir()
			trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
			cacheRoot := filepath.Join(tempDir, "cache")
			if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
				t.Fatalf("create cache root: %v", err)
			}
			external := filepath.Join(tempDir, "external-"+namespace)
			if err := os.MkdirAll(external, 0o700); err != nil {
				t.Fatalf("create external namespace: %v", err)
			}
			if err := os.Symlink(external, filepath.Join(cacheRoot, namespace)); err != nil {
				t.Fatalf("preseed symlinked %s namespace: %v", namespace, err)
			}
			resolver := mustGitResolver(t, cacheRoot)

			logPath := installLoggingGitWrapper(t)
			_, err := resolver.Resolve(
				context.Background(),
				mustGitSource(t, trustedOrigin, "skills/demo", "main"),
				noOperationOptions,
			)
			if err == nil || !strings.Contains(err.Error(), namespace) {
				t.Fatalf("Resolve error = %v, want %s namespace rejection", err, namespace)
			}
			assertGitSubcommandsAbsent(t, logPath, "fetch", "archive")
			_, lockErr := os.Lstat(filepath.Join(cacheRoot, "locks"))
			if namespace != "locks" && !os.IsNotExist(lockErr) {
				t.Fatalf("cache lock namespace was created after %s rejection: %v", namespace, lockErr)
			}
			entries, err := os.ReadDir(external)
			if err != nil {
				t.Fatalf("read external namespace: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("external namespace entries = %v, want none", entries)
			}
		})
	}
}

func TestResolveRejectsSymlinkedArtifactAncestorBeforeArchive(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	sourceSpec := mustGitSource(t, trustedOrigin, "skills/demo", "main")
	resolved, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatalf("prime Resolve returned error: %v", err)
	}
	commit := string(resolved.Identity().ResolvedRef())
	artifactAncestor := filepath.Dir(resolver.artifactEntryRoot(trustedOrigin, commit, "skills/demo"))
	if err := os.RemoveAll(filepath.Dir(artifactAncestor)); err != nil {
		t.Fatalf("remove primed artifact subtree: %v", err)
	}
	external := filepath.Join(tempDir, "external-artifacts")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatalf("create external artifact directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(artifactAncestor), 0o700); err != nil {
		t.Fatalf("recreate artifact parent: %v", err)
	}
	if err := os.Symlink(external, artifactAncestor); err != nil {
		t.Fatalf("preseed symlinked artifact ancestor: %v", err)
	}

	logPath := installLoggingGitWrapper(t)
	_, err = resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Resolve error = %v, want artifact ancestor rejection", err)
	}
	assertGitSubcommandsAbsent(t, logPath, "archive")
	entries, err := os.ReadDir(external)
	if err != nil {
		t.Fatalf("read external artifact directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("external artifact entries = %v, want none", entries)
	}
}

func TestResolveRejectsArtifactAncestorReplacementBeforeView(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	cacheRoot := filepath.Join(tempDir, "cache")
	resolver := mustGitResolver(t, cacheRoot)
	sourceSpec := mustGitSource(t, trustedOrigin, "skills/demo", "main")
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("prime Resolve returned error: %v", err)
	}

	artifactsRoot := filepath.Join(cacheRoot, "artifacts")
	movedArtifacts := artifactsRoot + ".moved"
	resolver.state.testAfterArtifactEnsure = func() {
		resolver.state.testAfterArtifactEnsure = nil
		if err := os.Rename(artifactsRoot, movedArtifacts); err != nil {
			t.Fatalf("move artifact cache in race hook: %v", err)
		}
		if err := os.Symlink(movedArtifacts, artifactsRoot); err != nil {
			t.Fatalf("replace artifact cache in race hook: %v", err)
		}
	}

	_, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("Resolve error = %v, want artifact ancestor replacement rejection", err)
	}
}

func TestResolveRejectsArtifactDirectoryReplacementBeforeIdentityUse(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	cacheRoot := filepath.Join(tempDir, "cache")
	resolver := mustGitResolver(t, cacheRoot)
	sourceSpec := mustGitSource(t, trustedOrigin, "skills/demo", "main")
	primed, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatalf("prime Resolve returned error: %v", err)
	}
	commit := string(primed.Identity().ResolvedRef())

	artifactsRoot := filepath.Join(cacheRoot, "artifacts")
	movedArtifacts := artifactsRoot + ".moved"
	resolver.state.testAfterArtifactEnsure = func() {
		resolver.state.testAfterArtifactEnsure = nil
		if err := os.Rename(artifactsRoot, movedArtifacts); err != nil {
			t.Fatalf("move artifact cache in race hook: %v", err)
		}
		replacementRoot := filepath.Join(
			resolver.artifactRoot(trustedOrigin, commit, "skills/demo"),
			"skills",
			"demo",
		)
		if err := os.MkdirAll(replacementRoot, 0o700); err != nil {
			t.Fatalf("create replacement artifact root: %v", err)
		}
		if err := os.WriteFile(
			filepath.Join(replacementRoot, "SKILL.md"),
			[]byte("---\nname: attacker\n---\n"),
			0o600,
		); err != nil {
			t.Fatalf("write replacement artifact content: %v", err)
		}
	}

	_, err = resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "does not match expected hash") {
		t.Fatalf("Resolve error = %v, want artifact identity mismatch", err)
	}
}

func TestResolveRejectsWrongRepositoryOriginBeforeFetchOrArchive(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	attackerOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "attacker"), "attacker")

	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	cloneGitRepository(t, attackerOrigin, resolver.repositoryPath(trustedOrigin), true)
	writeRepositoryAuthorityRecordForTest(t, resolver, trustedOrigin)

	logPath := installLoggingGitWrapper(t)
	_, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, trustedOrigin, "skills/demo", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("Resolve error = %v, want repository-origin rejection", err)
	}
	assertGitSubcommandsAbsent(t, logPath, "fetch", "archive")
}

func TestResolveRejectsReplacedGitDirectoryBeforeFetchOrArchive(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	attackerOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "attacker"), "attacker")

	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	cachePath := resolver.repositoryPath(trustedOrigin)
	if err := os.MkdirAll(cachePath, 0o700); err != nil {
		t.Fatalf("create cache path: %v", err)
	}
	if err := os.Symlink(filepath.Join(attackerOrigin, ".git"), filepath.Join(cachePath, ".git")); err != nil {
		t.Fatalf("preseed replaced .git entry: %v", err)
	}

	logPath := installLoggingGitWrapper(t)
	_, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, trustedOrigin, "skills/demo", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "cache") {
		t.Fatalf("Resolve error = %v, want repository form rejection", err)
	}
	assertGitSubcommandsAbsent(t, logPath, "fetch", "archive")
}

func TestResolveRejectsRepositoryReplacementBeforeGitLaunch(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	attackerOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "attacker"), "attacker")

	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	if _, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, trustedOrigin, "skills/demo", "main"),
		noOperationOptions,
	); err != nil {
		t.Fatalf("prime Resolve returned error: %v", err)
	}

	cachePath := resolver.repositoryPath(trustedOrigin)
	attackerCache := filepath.Join(tempDir, "attacker-cache")
	cloneGitRepository(t, attackerOrigin, attackerCache, true)
	resolver.state.testBeforeRepositoryCommand = func() {
		resolver.state.testBeforeRepositoryCommand = nil
		moved := cachePath + "-moved"
		if err := os.Rename(cachePath, moved); err != nil {
			t.Fatalf("move repository cache in race hook: %v", err)
		}
		if err := os.Symlink(attackerCache, cachePath); err != nil {
			t.Fatalf("replace repository cache in race hook: %v", err)
		}
	}

	logPath := installLoggingGitWrapper(t)
	_, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, trustedOrigin, "skills/demo", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("Resolve error = %v, want repository replacement rejection", err)
	}
	assertGitSubcommandsAbsent(t, logPath, "fetch", "archive")
}

func TestRepositoryOriginDiagnosticRedactsCredentialBearingObservedLocator(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	cachePath := resolver.repositoryPath(trustedOrigin)
	cloneGitRepository(t, trustedOrigin, cachePath, true)
	writeRepositoryAuthorityRecordForTest(t, resolver, trustedOrigin)

	secret := "synthetic-cache-origin-secret"
	runGitTestCommand(
		t,
		cachePath,
		"remote",
		"set-url",
		"origin",
		"https://user:"+secret+"@example.invalid/repository.git",
	)

	_, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, trustedOrigin, "skills/demo", "main"),
		noOperationOptions,
	)
	if err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("Resolve error = %v, want repository-origin rejection", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("repository-origin diagnostic disclosed credential: %v", err)
	}
}

func TestResolveAcceptsEquivalentNativeAndFileURLOrigins(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	cachePath := resolver.repositoryPath(trustedOrigin)
	cloneGitRepository(t, trustedOrigin, cachePath, true)
	writeRepositoryAuthorityRecordForTest(t, resolver, trustedOrigin)
	runGitTestCommand(
		t,
		cachePath,
		"config",
		"remote.origin.fetch",
		"+refs/heads/*:refs/remotes/origin/*",
	)
	runGitTestCommand(t, cachePath, "remote", "set-url", "origin", fileURLForTest(trustedOrigin))

	resolved, err := resolver.Resolve(
		context.Background(),
		mustGitSource(t, trustedOrigin, "skills/demo", "main"),
		noOperationOptions,
	)
	if err != nil {
		t.Fatalf("Resolve returned error for equivalent local origin: %v", err)
	}
	if got := string(mustReadGitResolutionFile(t, resolved, "SKILL.md")); !strings.Contains(got, "trusted") {
		t.Fatalf("resolved content = %q, want trusted source", got)
	}
}

func TestResolveRejectsUnownedLocalGitConfigurationBeforeFetch(t *testing.T) {
	requireGit(t)
	testCases := []struct {
		name  string
		key   string
		value string
	}{
		{name: "include", key: "include.path", value: "/tmp/daem-untrusted-git-config"},
		{name: "URL rewrite", key: "url.evil.insteadof", value: "https://example.invalid/"},
		{name: "SSH command", key: "core.sshcommand", value: "touch /tmp/daem-untrusted-ssh-command"},
		{name: "credential helper", key: "credential.helper", value: "!touch /tmp/daem-untrusted-credential-helper"},
		{name: "upload pack", key: "remote.origin.uploadpack", value: "touch /tmp/daem-untrusted-upload-pack"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tempDir := t.TempDir()
			trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
			resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
			sourceSpec := mustGitSource(t, trustedOrigin, "skills/demo", "main")
			if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
				t.Fatalf("prime Resolve returned error: %v", err)
			}
			runGitTestCommand(
				t,
				resolver.repositoryPath(trustedOrigin),
				"config",
				"--local",
				testCase.key,
				testCase.value,
			)

			logPath := installLoggingGitWrapper(t)
			_, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
			if err == nil || !strings.Contains(err.Error(), "configuration") {
				t.Fatalf("Resolve error = %v, want local-configuration rejection", err)
			}
			assertGitSubcommandsAbsent(t, logPath, "fetch", "archive")
		})
	}
}

func TestResolveRejectsAmbientOriginRewriteBeforeFetch(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	attackerOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "attacker"), "attacker")
	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	sourceSpec := mustGitSource(t, trustedOrigin, "skills/demo", "main")
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("prime Resolve returned error: %v", err)
	}

	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+fileURLForTest(attackerOrigin)+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", trustedOrigin)
	logPath := installLoggingGitWrapper(t)

	_, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "effective") {
		t.Fatalf("Resolve error = %v, want effective-origin rejection", err)
	}
	assertGitSubcommandsAbsent(t, logPath, "fetch", "archive")
}

func TestResolveDoesNotExecuteRepositoryCacheHooks(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	sourceSpec := mustGitSource(t, trustedOrigin, "skills/demo", "main")
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("prime Resolve returned error: %v", err)
	}

	hookMarker := filepath.Join(tempDir, "hook-ran")
	t.Setenv("DAEM_TEST_GIT_HOOK_MARKER", hookMarker)
	hookPath := filepath.Join(
		resolver.repositoryPath(trustedOrigin),
		"hooks",
		"reference-transaction",
	)
	hook := "#!/bin/sh\nprintf 'ran' > \"$DAEM_TEST_GIT_HOOK_MARKER\"\n"
	if err := os.WriteFile(hookPath, []byte(hook), 0o700); err != nil {
		t.Fatalf("write repository-cache hook: %v", err)
	}
	writeGitTestFile(
		t,
		trustedOrigin,
		"skills/demo/SKILL.md",
		"---\nname: trusted-updated\n---\n",
	)
	commitAll(t, trustedOrigin, "update trusted skill")

	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("Resolve returned error with inert cache hook: %v", err)
	}
	if _, err := os.Lstat(hookMarker); !os.IsNotExist(err) {
		t.Fatalf("repository-cache hook ran or marker stat failed: %v", err)
	}
}

func TestResolveIgnoresRepositorySelectingGitEnvironment(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	attackerOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "attacker"), "attacker")
	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	sourceSpec := mustGitSource(t, trustedOrigin, "skills/demo", "main")
	if _, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions); err != nil {
		t.Fatalf("prime Resolve returned error: %v", err)
	}

	attackerGitDir := filepath.Join(attackerOrigin, ".git")
	for name, value := range map[string]string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": filepath.Join(attackerGitDir, "objects"),
		"GIT_COMMON_DIR":                   attackerGitDir,
		"GIT_DIR":                          attackerGitDir,
		"GIT_IMPLICIT_WORK_TREE":           "1",
		"GIT_INDEX_FILE":                   filepath.Join(attackerGitDir, "index"),
		"GIT_NAMESPACE":                    "attacker",
		"GIT_OBJECT_DIRECTORY":             filepath.Join(attackerGitDir, "objects"),
		"GIT_QUARANTINE_PATH":              filepath.Join(attackerGitDir, "objects"),
		"GIT_REPLACE_REF_BASE":             "refs/replace/attacker",
		"GIT_SHALLOW_FILE":                 filepath.Join(attackerGitDir, "shallow"),
		"GIT_WORK_TREE":                    attackerOrigin,
	} {
		t.Setenv(name, value)
	}

	resolved, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error with repository-selecting Git environment: %v", err)
	}
	if got := string(mustReadGitResolutionFile(t, resolved, "SKILL.md")); !strings.Contains(got, "trusted") {
		t.Fatalf("resolved content = %q, want trusted source", got)
	}
}

func TestResolveIgnoresRepositoryCacheReplaceRefs(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()
	trustedOrigin := gitRepositoryWithSkill(t, filepath.Join(tempDir, "trusted"), "trusted")
	firstCommit := strings.TrimSpace(runGitTestCommand(t, trustedOrigin, "rev-parse", "HEAD"))
	resolver := mustGitResolver(t, filepath.Join(tempDir, "cache"))
	sourceSpec := mustGitSource(t, trustedOrigin, "skills/demo", "main")
	resolved, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatalf("prime Resolve returned error: %v", err)
	}

	writeGitTestFile(
		t,
		trustedOrigin,
		"skills/demo/SKILL.md",
		"---\nname: attacker\n---\n",
	)
	replacementCommit := commitAll(t, trustedOrigin, "replacement content")
	runGitTestCommand(t, trustedOrigin, "tag", "daem-replacement", replacementCommit)
	runGitTestCommand(t, trustedOrigin, "reset", "--hard", firstCommit)

	cachePath := resolver.repositoryPath(trustedOrigin)
	runGitTestCommand(
		t,
		cachePath,
		"fetch",
		"origin",
		"refs/tags/daem-replacement:refs/tags/daem-replacement",
	)
	runGitTestCommand(t, cachePath, "replace", firstCommit, replacementCommit)
	if err := os.RemoveAll(
		resolver.artifactEntryRoot(
			trustedOrigin,
			string(resolved.Identity().ResolvedRef()),
			"skills/demo",
		),
	); err != nil {
		t.Fatalf("remove primed artifact entry: %v", err)
	}

	resolved, err = resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error with cache replace ref: %v", err)
	}
	if got := string(mustReadGitResolutionFile(t, resolved, "SKILL.md")); !strings.Contains(got, "trusted") {
		t.Fatalf("resolved content = %q, want trusted source", got)
	}
}

func gitRepositoryWithSkill(t *testing.T, root string, name string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create repository fixture root: %v", err)
	}
	repository := initGitRepository(t, root)
	writeGitTestFile(t, repository, "skills/demo/SKILL.md", "---\nname: "+name+"\n---\n")
	commitAll(t, repository, "initial "+name+" skill")
	return repository
}

func mustGitResolver(t *testing.T, cacheRoot string) Resolver {
	t.Helper()
	resolver, err := NewResolver(cacheRoot)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	return resolver
}

func cloneGitRepository(t *testing.T, locator string, destination string, bare bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatalf("create clone parent: %v", err)
	}
	args := []string{"clone", "--no-checkout"}
	if bare {
		args = append(args, "--bare")
	}
	args = append(args, "--", locator, destination)
	command := exec.Command(gitExecutable, args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("clone repository fixture: %v\n%s", err, output)
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		t.Fatalf("set repository fixture mode: %v", err)
	}
}

func writeRepositoryAuthorityRecordForTest(t *testing.T, resolver Resolver, locator string) {
	t.Helper()
	parsed, err := source.ParseGitLocator(locator)
	if err != nil {
		t.Fatalf("parse repository authority locator: %v", err)
	}
	repository := repositoryForLocator(resolver, parsed)
	content, err := encodeRepositoryCacheRecord(newRepositoryCacheRecord(repository))
	if err != nil {
		t.Fatalf("encode repository cache fixture: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(repository.path, repositoryCacheRecordName),
		content,
		0o600,
	); err != nil {
		t.Fatalf("write repository cache fixture: %v", err)
	}
}

func installLoggingGitWrapper(t *testing.T) string {
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
	wrapper := "#!/bin/sh\n" + detectGitSubcommandShell +
		"printf '%s\\n' \"$git_subcommand\" >> \"$DAEM_GIT_SUBCOMMAND_LOG\"\nexec " +
		shellQuoteForTest(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binRoot, gitExecutable), []byte(wrapper), 0o700); err != nil {
		t.Fatalf("write logging git wrapper: %v", err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", binRoot+string(os.PathListSeparator)+originalPath)
	t.Setenv("DAEM_GIT_SUBCOMMAND_LOG", logPath)
	return logPath
}

func assertGitSubcommandsAbsent(t *testing.T, logPath string, forbidden ...string) {
	t.Helper()
	content, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read Git subcommand log: %v", err)
	}
	commands := strings.Fields(string(content))
	for _, command := range commands {
		for _, rejected := range forbidden {
			if command == rejected {
				t.Fatalf("Git subcommand %q ran after cache rejection: %v", rejected, commands)
			}
		}
	}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func fileURLForTest(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
