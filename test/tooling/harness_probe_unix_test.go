//go:build unix

package tooling

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"testing"
)

const packageWrapperProbeArgument = "--daem-package-wrapper-probe"

type packageWrapperProbe struct {
	PID       int
	ParentPID int
	PGID      int
	Home      string
	StateHome string
}

func TestMain(tests *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == packageWrapperProbeArgument {
		probe := packageWrapperProbe{
			PID:       os.Getpid(),
			ParentPID: os.Getppid(),
			PGID:      syscall.Getpgrp(),
			Home:      os.Getenv("HOME"),
			StateHome: os.Getenv("XDG_STATE_HOME"),
		}
		if err := json.NewEncoder(os.Stdout).Encode(probe); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(tests.Run())
}

func (probe packageWrapperProbe) validateProcess(wrapper string, wrapperPID int, platform, stderr string) error {
	if wrapperPID <= 0 || probe.PID <= 0 || probe.PID == wrapperPID || probe.ParentPID != wrapperPID || probe.PGID != probe.PID {
		return fmt.Errorf("pid=%d ppid=%d pgid=%d, want a child of %d in its own process group", probe.PID, probe.ParentPID, probe.PGID, wrapperPID)
	}
	if stderr == "" {
		return nil
	}

	// Darwin can reject the child's redundant setpgid after the parent created
	// the group. Admit only that diagnostic with the actual group verified above.
	// This completed-probe check is not a runtime pre-exec admission guard.
	expected := fmt.Sprintf("%s: child setpgid (%d to %d): Operation not permitted\n", wrapper, probe.PID, probe.PID)
	if platform == "darwin" && stderr == expected {
		return nil
	}
	return fmt.Errorf("unexpected package wrapper stderr: %q", stderr)
}

func TestPackageWrapperProbeValidatesProcessAndDiagnostics(t *testing.T) {
	const wrapper = "/repo/tools/test-exec.sh"
	valid := packageWrapperProbe{PID: 102, ParentPID: 101, PGID: 102}
	warning := wrapper + ": child setpgid (102 to 102): Operation not permitted\n"

	for _, test := range []struct {
		name       string
		probe      packageWrapperProbe
		wrapperPID int
		platform   string
		stderr     string
		wantError  bool
	}{
		{name: "quiet-darwin", probe: valid, wrapperPID: 101, platform: "darwin"},
		{name: "quiet-linux", probe: valid, wrapperPID: 101, platform: "linux"},
		{name: "verified-darwin-diagnostic", probe: valid, wrapperPID: 101, platform: "darwin", stderr: warning},
		{name: "linux-diagnostic", probe: valid, wrapperPID: 101, platform: "linux", stderr: warning, wantError: true},
		{name: "foreign-wrapper", probe: valid, wrapperPID: 101, platform: "darwin", stderr: "/other" + warning, wantError: true},
		{name: "foreign-child", probe: valid, wrapperPID: 101, platform: "darwin", stderr: wrapper + ": child setpgid (103 to 103): Operation not permitted\n", wantError: true},
		{name: "foreign-group", probe: valid, wrapperPID: 101, platform: "darwin", stderr: wrapper + ": child setpgid (102 to 103): Operation not permitted\n", wantError: true},
		{name: "different-error", probe: valid, wrapperPID: 101, platform: "darwin", stderr: wrapper + ": child setpgid (102 to 102): No such process\n", wantError: true},
		{name: "unrelated-stderr", probe: valid, wrapperPID: 101, platform: "darwin", stderr: "unexpected diagnostic\n", wantError: true},
		{name: "additional-stderr", probe: valid, wrapperPID: 101, platform: "darwin", stderr: warning + "unexpected diagnostic\n", wantError: true},
		{name: "repeated-diagnostic", probe: valid, wrapperPID: 101, platform: "darwin", stderr: warning + warning, wantError: true},
		{name: "missing-newline", probe: valid, wrapperPID: 101, platform: "darwin", stderr: warning[:len(warning)-1], wantError: true},
		{name: "ungrouped-quiet", probe: packageWrapperProbe{PID: 102, ParentPID: 101, PGID: 100}, wrapperPID: 101, platform: "darwin", wantError: true},
		{name: "ungrouped-diagnostic", probe: packageWrapperProbe{PID: 102, ParentPID: 101, PGID: 100}, wrapperPID: 101, platform: "darwin", stderr: warning, wantError: true},
		{name: "foreign-parent", probe: packageWrapperProbe{PID: 102, ParentPID: 100, PGID: 102}, wrapperPID: 101, platform: "darwin", stderr: warning, wantError: true},
		{name: "missing-process", probe: packageWrapperProbe{ParentPID: 101}, wrapperPID: 101, platform: "darwin", wantError: true},
		{name: "negative-process", probe: packageWrapperProbe{PID: -1, ParentPID: 101, PGID: -1}, wrapperPID: 101, platform: "darwin", wantError: true},
		{name: "missing-wrapper", probe: valid, platform: "darwin", wantError: true},
		{name: "same-process", probe: packageWrapperProbe{PID: 101, ParentPID: 101, PGID: 101}, wrapperPID: 101, platform: "darwin", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.probe.validateProcess(wrapper, test.wrapperPID, test.platform, test.stderr)
			if (err != nil) != test.wantError {
				t.Fatalf("validate process = %v, want error=%t", err, test.wantError)
			}
		})
	}
}
