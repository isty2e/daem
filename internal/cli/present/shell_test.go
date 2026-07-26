package clipresent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestShellCommandRoundTripsArgumentsWithoutExpansion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell quoting contract")
	}

	canary := filepath.Join(t.TempDir(), "must-not-exist")
	values := []string{
		"",
		"plain",
		"with space",
		"single'quote",
		`double"quote`,
		"$(touch " + canary + ")",
		"semi;colon",
		"star*glob",
		"back\\slash",
		"line\nbreak",
		"trailing newline\n",
		"escape\x1b[2J",
		string([]byte{0x01, 0x7f, 0x80}),
		"bidirectional\u202econtrol",
		string([]byte{'i', 'n', 'v', 'a', 'l', 'i', 'd', '-', 0xff}),
		"~/.config/daem",
		"#comment",
		"한글 경로",
	}
	for _, value := range values {
		command, err := ShellCommand(
			"/bin/sh",
			"-c",
			`printf '%s\000' "$#"; printf '%s\000' "$@"`,
			"daem-shell-probe",
			value,
		)
		if err != nil {
			t.Fatalf("ShellCommand(%q): %v", value, err)
		}
		output, err := exec.Command("/bin/sh", "-c", command).Output()
		if err != nil {
			t.Fatalf("execute %q: %v", command, err)
		}
		want := "1\x00" + value + "\x00"
		if string(output) != want {
			t.Fatalf("command %q output = %q, want one exact argument %q", command, output, want)
		}
	}
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatalf("command substitution canary stat error = %v, want not exist", err)
	}
}

func TestShellCommandUsesStableMinimalQuoting(t *testing.T) {
	got, err := ShellCommand("daem", "lock", "--manifest", "/tmp/plain/daem.toml")
	if err != nil {
		t.Fatalf("ShellCommand returned error: %v", err)
	}
	if want := "daem lock --manifest /tmp/plain/daem.toml"; got != want {
		t.Fatalf("ShellCommand = %q, want %q", got, want)
	}
	got, err = ShellCommand("daem", "lock", "--manifest", "/tmp/a'b/daem.toml")
	if err != nil {
		t.Fatalf("ShellCommand returned error: %v", err)
	}
	if want := `daem lock --manifest '/tmp/a'"'"'b/daem.toml'`; got != want {
		t.Fatalf("ShellCommand = %q, want %q", got, want)
	}
}

func TestShellCommandRejectsNULArgument(t *testing.T) {
	if _, err := ShellCommand("daem", "lock", "--manifest", "bad\x00path"); !errors.Is(err, errShellArgumentContainsNUL) {
		t.Fatalf("ShellCommand error = %v, want NUL rejection", err)
	}
}

func mustShellCommand(t testing.TB, argv ...string) string {
	t.Helper()
	command, err := ShellCommand(argv...)
	if err != nil {
		t.Fatalf("ShellCommand(%q): %v", argv, err)
	}
	return command
}
