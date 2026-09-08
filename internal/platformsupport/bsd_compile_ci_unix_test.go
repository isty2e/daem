//go:build darwin || linux

package platformsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBSDCompileCIExecutesEveryTargetAndPreservesFailures(t *testing.T) {
	var workflow struct {
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(readRepositoryFile(t, ".github/workflows/ci.yml")), &workflow); err != nil {
		t.Fatal(err)
	}
	job := workflow.Jobs["bsd_observation_compile"]
	if job.RunsOn != "ubuntu-24.04" || job.TimeoutMinutes <= 0 || len(job.Strategy.Matrix.Include) != 0 {
		t.Fatalf("BSD compile job shape = runner %q, timeout %d, matrix rows %d; want one bounded Linux job",
			job.RunsOn, job.TimeoutMinutes, len(job.Strategy.Matrix.Include))
	}
	if job.If != "" || job.ContinueOnError != nil {
		t.Fatal("BSD compile job must run unconditionally and propagate failure")
	}
	var compile releaseStep
	for _, step := range job.Steps {
		if step.Run != "" {
			if compile.Run != "" {
				t.Fatal("BSD compile execution is split across multiple scripts")
			}
			compile = step
		}
	}
	if compile.Run == "" || compile.ContinueOnError != nil || compile.If != "" ||
		(compile.Shell != "" && compile.Shell != "bash") {
		t.Fatal("BSD compile script must run unconditionally and propagate failure")
	}

	for _, scenario := range []struct {
		name    string
		failure string
		version string
	}{
		{name: "success", version: "go1.26.6"},
		{name: "first failure", version: "go1.26.6", failure: "freebsd/amd64:./internal/filesnapshot"},
		{name: "middle failure", version: "go1.26.6", failure: "netbsd/amd64:./internal/assurance/observe/codexplugin"},
		{name: "last failure", version: "go1.26.6", failure: "openbsd/amd64:./internal/assurance/observe/codexplugin"},
		{name: "wrong toolchain", version: "go1.25.12"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := t.TempDir()
			calls := filepath.Join(directory, "calls")
			if err := os.WriteFile(calls, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			stub := `#!/bin/sh
if [ "$*" = 'env GOVERSION' ]; then
  printf '%s\n' "$PROBE_GO_VERSION"
  exit 0
fi
printf '%s\n' "$GOOS/$GOARCH" "$CGO_ENABLED" "$#" "$@" >> "$PROBE_CALLS"
if [ "$GOOS/$GOARCH:$5" = "$PROBE_FAILURE" ]; then
  printf 'injected compile failure: %s\n' "$PROBE_FAILURE" >&2
  exit 19
fi
`
			if err := os.WriteFile(filepath.Join(directory, "go"), []byte(stub), 0o700); err != nil {
				t.Fatal(err)
			}
			outputRoot := filepath.Join(directory, "compile outputs")
			command := exec.CommandContext(t.Context(), "bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", compile.Run)
			command.Dir = directory
			command.Env = append(
				os.Environ(),
				"PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"),
				"RUNNER_TEMP="+outputRoot,
				"PROBE_CALLS="+calls,
				"PROBE_FAILURE="+scenario.failure,
				"PROBE_GO_VERSION="+scenario.version,
			)
			for key, value := range compile.Env {
				command.Env = append(command.Env, key+"="+value)
			}
			output, err := command.CombinedOutput()
			wantSuccess := scenario.failure == "" && scenario.version == "go1.26.6"
			if (err == nil) != wantSuccess {
				t.Fatalf("compile result = %v, want success=%t\n%s", err, wantSuccess, output)
			}
			if scenario.failure != "" && !strings.Contains(string(output), "injected compile failure: "+scenario.failure) {
				t.Fatalf("compiler failure diagnostic was lost: %s", output)
			}

			var expected []string
			if scenario.version == "go1.26.6" {
				for _, target := range []string{"freebsd/amd64", "freebsd/386", "freebsd/arm", "netbsd/amd64", "netbsd/386", "netbsd/arm", "openbsd/amd64"} {
					for _, packagePath := range []string{"./internal/filesnapshot", "./internal/assurance/observe/codexplugin"} {
						expected = append(expected, target, "0", "5", "test", "-c", "-o",
							filepath.Join(outputRoot, filepath.Base(packagePath)+"-"+strings.ReplaceAll(target, "/", "-")+".test"), packagePath)
					}
				}
			}
			actual, readErr := os.ReadFile(calls)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := ""
			if len(expected) != 0 {
				want = strings.Join(expected, "\n") + "\n"
			}
			if string(actual) != want {
				t.Fatalf("compiler calls =\n%s\nwant:\n%s", actual, want)
			}
		})
	}
}
