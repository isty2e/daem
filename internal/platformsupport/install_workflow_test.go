package platformsupport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func installerWorkflowStep(t *testing.T, name string) string {
	t.Helper()
	var workflow struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(readRepositoryFile(t, ".github/workflows/release-artifact.yml")), &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["artifact"].Steps {
		if step.Name == name {
			return step.Run
		}
	}
	t.Fatalf("release workflow step %q unavailable", name)
	return ""
}

func runInstallerWorkflowStep(t *testing.T, directory, script string, extra ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/bash", "-c", script)
	command.Dir = directory
	command.Env = append([]string{
		"PATH=" + filepath.Join(directory, "bin") + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + directory,
		"XDG_CONFIG_HOME=" + filepath.Join(directory, "config"),
		"XDG_DATA_HOME=" + filepath.Join(directory, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(directory, "cache"),
		"XDG_STATE_HOME=" + filepath.Join(directory, "state"),
		"TMPDIR=" + directory, "TMP=" + directory, "TEMP=" + directory,
		"LANG=C", "LC_ALL=C",
		"RUNNER_TEMP=" + directory,
		"GITHUB_ENV=" + filepath.Join(directory, "github-env"),
		"RELEASE_TAG=v1.2.3", "RELEASE_REVISION=" + strings.Repeat("a", 40),
		"RELEASE_COMMIT=" + strings.Repeat("a", 40),
		"RELEASE_REVISION_TIME=2026-07-01T02:03:04Z",
		"RELEASE_GO_VERSION=go1.26.5", "RELEASE_GOOS=" + runtime.GOOS, "RELEASE_GOARCH=" + runtime.GOARCH,
	}, extra...)
	return command.CombinedOutput()
}

func writeWorkflowFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseWorkflowExecutesTaggedInstallerWithoutLegacyFallback(t *testing.T) {
	step := installerWorkflowStep(t, "Exercise documented installer flow against local artifacts")
	for _, exitCode := range []int{0, 17} {
		t.Run(fmt.Sprint(exitCode), func(t *testing.T) {
			root := t.TempDir()
			writeWorkflowFixture(t, root, "install.sh", "#!/bin/sh\nexit 99\n")
			writeWorkflowFixture(t, root, "bin/git", "#!/bin/sh\n[ \"$1\" = status ] || exit 91\n")
			writeWorkflowFixture(t, root, "bin/awk", "#!/bin/sh\nprintf attempted > \"$RUNNER_TEMP/legacy-attempt\"\nexit 92\n")
			writeWorkflowFixture(t, root, "tools/test-go.sh", `#!/bin/sh
set -eu
test "$DAEM_TEST_RELEASE_DIR" = "$RUNNER_TEMP/artifact-1"
test "$DAEM_TEST_RELEASE_VERSION" = "$RELEASE_TAG"
test "$DAEM_TEST_RELEASE_REVISION" = "$RELEASE_REVISION"
test "$DAEM_TEST_RELEASE_TIME" = "$RELEASE_REVISION_TIME"
test "$DAEM_TEST_RELEASE_TOOLCHAIN" = "$RELEASE_GO_VERSION"
test "$DAEM_TEST_RELEASE_TARGET" = "${RELEASE_GOOS}_${RELEASE_GOARCH}"
test "$DAEM_TEST_DEVELOPMENT_BINARY" = "$RUNNER_TEMP/development-daem"
printf '%s\n' "$@" > "$RUNNER_TEMP/installer-invocation"
exit "$DAEM_TEST_EXIT"
`)
			output, err := runInstallerWorkflowStep(t, root, step, fmt.Sprintf("DAEM_TEST_EXIT=%d", exitCode))
			if (err == nil) != (exitCode == 0) {
				t.Fatalf("workflow exit=%v, want code %d:\n%s", err, exitCode, output)
			}
			invocation, readErr := os.ReadFile(filepath.Join(root, "installer-invocation"))
			if readErr != nil || string(invocation) != "-mod=readonly\n-count=1\n-v\n-run\n^TestInstallerNativeRelease$\n./test/install\n" {
				t.Fatalf("native fixture invocation = %q, error=%v", invocation, readErr)
			}
			if _, err := os.Stat(filepath.Join(root, "legacy-attempt")); !os.IsNotExist(err) {
				t.Fatalf("standalone installer execution fell back to legacy recipe: %v", err)
			}
		})
	}
}

func TestReleaseWorkflowBindsRevisionTimeToCommit(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("release workflow uses native Unix date")
	}
	step := installerWorkflowStep(t, "Verify embedded version contract")
	const expectedTime = "2026-07-01T02:03:04Z"
	commitTime, err := time.Parse(time.RFC3339, expectedTime)
	if err != nil {
		t.Fatal(err)
	}
	for _, timestamp := range []string{expectedTime, "2026-07-01T02:03:05Z"} {
		t.Run(timestamp, func(t *testing.T) {
			root := t.TempDir()
			writeWorkflowFixture(t, root, "install.sh", "#!/bin/sh\nexit 99\n")
			writeWorkflowFixture(t, root, "bin/git", "#!/bin/sh\n[ \"$*\" = \"show -s --format=%ct $RELEASE_COMMIT\" ] || exit 91\nprintf '%s' \"$DAEM_TEST_COMMIT_SECONDS\"\n")
			writeWorkflowFixture(t, root, "build-1/daem", "#!/bin/sh\n[ \"$*\" = 'version --json' ] || exit 92\nprintf '%s\\n' \"$DAEM_TEST_VERSION_JSON\"\n")
			identity := fmt.Sprintf(`{
  "schema_version": 1,
  "version": "v1.2.3",
  "revision": %q,
  "revision_time": %q,
  "source_state": "clean",
  "vcs": "git",
  "go_version": "go1.26.5",
  "goos": %q,
  "goarch": %q
}`, strings.Repeat("a", 40), timestamp, runtime.GOOS, runtime.GOARCH)
			output, err := runInstallerWorkflowStep(t, root, step, "DAEM_TEST_VERSION_JSON="+identity, fmt.Sprintf("DAEM_TEST_COMMIT_SECONDS=%d", commitTime.Unix()))
			if (err == nil) != (timestamp == expectedTime) {
				t.Fatalf("revision time %s workflow error=%v:\n%s", timestamp, err, output)
			}
			if err == nil {
				exported, err := os.ReadFile(filepath.Join(root, "github-env"))
				if err != nil || string(exported) != "RELEASE_REVISION_TIME="+expectedTime+"\n" {
					t.Fatalf("exported time = %q, error=%v", exported, err)
				}
			}
		})
	}
}
