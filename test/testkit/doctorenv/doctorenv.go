package doctorenv

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	fakeGitModeEnv    = "DAEM_TEST_DOCTOR_GIT_MODE"
	fakeGitVersionEnv = "DAEM_TEST_DOCTOR_GIT_VERSION"
	fakeGitScript     = "#!/bin/sh\n" +
		"if [ \"$#\" -eq 1 ] && [ \"$1\" = \"--version\" ]; then\n" +
		"  printf '%s\\n' \"$DAEM_TEST_DOCTOR_GIT_VERSION\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$#\" -eq 2 ] && [ \"$1\" = \"init\" ] && [ \"$2\" = \"-h\" ]; then\n" +
		"  printf '%s\\n' \"usage: git init\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 2\n"
)

func init() {
	if os.Getenv(fakeGitModeEnv) != "success" {
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Fprintln(os.Stdout, os.Getenv(fakeGitVersionEnv))
		os.Exit(0)
	}
	if len(os.Args) == 3 && os.Args[1] == "init" && os.Args[2] == "-h" {
		fmt.Fprintln(os.Stdout, "usage: git init")
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "fake doctor git accepts only --version and init -h")
	os.Exit(2)
}

// WithFakeGit exposes a deterministic git --version command through PATH.
func WithFakeGit(t *testing.T, version string) string {
	t.Helper()

	binDirectory := t.TempDir()
	gitName := "git"
	if runtime.GOOS == "windows" {
		gitName += ".exe"
	}
	gitPath := filepath.Join(binDirectory, gitName)
	if runtime.GOOS == "windows" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatalf("resolve test executable: %v", err)
		}
		if err := os.Link(executable, gitPath); err != nil {
			if err := copyTestExecutable(executable, gitPath); err != nil {
				t.Fatalf("install fake doctor git: %v", err)
			}
		}
		t.Setenv(fakeGitModeEnv, "success")
	} else {
		if err := os.WriteFile(gitPath, []byte(fakeGitScript), 0o755); err != nil {
			t.Fatalf("install fake doctor git: %v", err)
		}
	}

	t.Setenv(fakeGitVersionEnv, version)
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	return binDirectory
}

// WithoutGit replaces PATH with an empty directory.
func WithoutGit(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// WithoutEnvironmentVariable makes one environment variable absent for a test.
func WithoutEnvironmentVariable(t *testing.T, name string) {
	t.Helper()

	value, present := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, value)
			return
		}
		_ = os.Unsetenv(name)
	})
}

func copyTestExecutable(source string, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
