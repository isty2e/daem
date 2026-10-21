//go:build unix

package gitcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestExplicitObjectFormatCaptureDiagnostics(t *testing.T) {
	cases := []struct {
		name              string
		wantSupported     bool
		wantClass         string
		wantExit          int
		wantStdoutCut     bool
		wantStderrCut     bool
		wantEmptyEvidence bool
	}{
		{name: "positive", wantSupported: true},
		{name: "legacy"},
		{name: "positive-held", wantSupported: true},
		{name: "negative-held", wantClass: "help output was truncated"},
		{name: "empty-zero", wantClass: "returned empty output", wantEmptyEvidence: true},
		{name: "empty-usage", wantClass: "returned empty output", wantExit: 129, wantEmptyEvidence: true},
		{name: "stdout-held", wantClass: "capture was truncated", wantExit: 129, wantStdoutCut: true, wantEmptyEvidence: true},
		{name: "stderr-held", wantClass: "capture was truncated", wantExit: 129, wantStderrCut: true, wantEmptyEvidence: true},
		{name: "both-held", wantClass: "capture was truncated", wantExit: 129, wantStdoutCut: true, wantStderrCut: true, wantEmptyEvidence: true},
		{name: "blank-overflow", wantClass: "capture was truncated", wantExit: 129, wantStdoutCut: true, wantEmptyEvidence: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			countPath := installGitHelpCaptureFixture(t, tc.name)
			resolver, err := NewResolver(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				supported, err := resolver.explicitObjectFormatSupported(context.Background())
				if tc.wantClass == "" {
					if err != nil || supported != tc.wantSupported {
						t.Fatalf("capability = %t, %v; want %t without error", supported, err, tc.wantSupported)
					}
					continue
				}
				if err == nil || supported || !strings.Contains(err.Error(), tc.wantClass) {
					t.Fatalf("capability = %t, %v; want refusal class %q", supported, err, tc.wantClass)
				}
				if tc.wantEmptyEvidence {
					reason := ""
					if tc.wantExit != 0 {
						reason = "nonzero_exit"
					}
					for _, evidence := range []string{
						fmt.Sprintf("exit_code=%d", tc.wantExit),
						"reason=" + strconv.Quote(reason),
						fmt.Sprintf("stdout_truncated=%t", tc.wantStdoutCut),
						fmt.Sprintf("stderr_truncated=%t", tc.wantStderrCut),
					} {
						if !strings.Contains(err.Error(), evidence) {
							t.Errorf("refusal %q omitted %q", err, evidence)
						}
					}
				}
			}
			wantProbes := 1
			if tc.wantClass != "" {
				wantProbes = 2
			}
			if got := countGitHelpProbes(t, countPath); got != wantProbes {
				t.Fatalf("probes = %d, want %d; error generations must not be cached", got, wantProbes)
			}
		})
	}
}

func installGitHelpCaptureFixture(t *testing.T, mode string) string {
	t.Helper()
	root := t.TempDir()
	fifo := filepath.Join(root, "held-input")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		fd, err := unix.Open(fifo, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			_, _ = unix.Write(fd, []byte("\n"))
			_ = unix.Close(fd)
		}
	})
	countPath := filepath.Join(root, "probes")
	t.Setenv("DAEM_GIT_HELP_COUNT", countPath)
	t.Setenv("DAEM_GIT_HELP_FIFO", fifo)
	t.Setenv("DAEM_GIT_HELP_CASE", mode)
	t.Setenv("DAEM_GIT_HELP_WIDTH", strconv.Itoa(objectFormatHelpOutputLimit+1))
	script := `#!/bin/sh
[ "$1" = init ] && [ "$2" = -h ] || exit 87
printf '.\n' >> "$DAEM_GIT_HELP_COUNT"
case "$DAEM_GIT_HELP_CASE" in
  positive|positive-held) printf 'usage: git init --object-format=<format>\n' >&2 ;;
  legacy|negative-held) printf 'usage: git init\n' >&2 ;;
  empty-zero) exit 0 ;;
  blank-overflow) printf "%${DAEM_GIT_HELP_WIDTH}s" "" ;;
esac
case "$DAEM_GIT_HELP_CASE" in
  stdout-held) /bin/sh -c 'read value < "$DAEM_GIT_HELP_FIFO"' 2>/dev/null & ;;
  stderr-held) /bin/sh -c 'read value < "$DAEM_GIT_HELP_FIFO"' >/dev/null & ;;
  both-held|positive-held|negative-held) /bin/sh -c 'read value < "$DAEM_GIT_HELP_FIFO"' & ;;
esac
exit 129
`
	if err := os.WriteFile(filepath.Join(root, gitExecutable), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	return countPath
}
