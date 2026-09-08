//go:build darwin || linux

package gitcli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
	sourcearchive "github.com/isty2e/daem/internal/supply/source/archive"
)

func TestGitConsumerCannotSuppressIncompleteOutput(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := gitProcessHelperCommand(t, ctx, "setsid-stdout-parent", pidFile)
	result := make(chan error, 1)
	go func() {
		result <- runGitReader(ctx, command, func(reader io.Reader) error {
			_, _ = io.Copy(io.Discard, reader)
			return nil
		})
	}()
	pids := waitForGitHelperPIDs(t, pidFile, 2)
	t.Cleanup(func() { _ = syscall.Kill(pids[1], syscall.SIGKILL) })
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "git output was incomplete after pipe drain bound") {
			t.Fatalf("suppressed read error = %v, want incomplete-output refusal", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("inherited stdout writer prevented consumer completion")
	}
	assertGitHelperProcessesGone(t, pids[:1])
	assertGitHelperProcessAlive(t, pids[1])
}

func TestGitStdoutCompletionDoesNotHideInheritedStderr(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := gitProcessHelperCommand(t, ctx, "setsid-stderr-parent", pidFile)
	result := make(chan error, 1)
	go func() {
		_, err := runGitOutput(ctx, command)
		result <- err
	}()
	pids := waitForGitHelperPIDs(t, pidFile, 2)
	t.Cleanup(func() { _ = syscall.Kill(pids[1], syscall.SIGKILL) })
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "read git stderr") ||
			!strings.Contains(err.Error(), "git output was incomplete after pipe drain bound") {
			t.Fatalf("inherited stderr error = %v, want incomplete stderr refusal", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("inherited stderr writer prevented completion")
	}
	assertGitHelperProcessesGone(t, pids[:1])
	assertGitHelperProcessAlive(t, pids[1])
}

func TestGitArchiveConsumerWorkDoesNotExpireOutput(t *testing.T) {
	for _, phase := range []string{"before-read", "between-reads", "after-eof"} {
		t.Run(phase, func(t *testing.T) {
			entered, release := holdGitLeaderWait(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := gitProcessHelperCommand(t, ctx, "archive-only", filepath.Join(t.TempDir(), "pid"))
			outputRoot := filepath.Join(t.TempDir(), "output")

			err := runGitReader(ctx, command, func(input io.Reader) error {
				defer release()
				var prefix []byte
				switch phase {
				case "between-reads":
					prefix = make([]byte, 512)
					if _, err := io.ReadFull(input, prefix); err != nil {
						return err
					}
				case "after-eof":
					var err error
					prefix, err = io.ReadAll(input)
					if err != nil {
						return err
					}
				}
				select {
				case <-entered:
				case <-ctx.Done():
					return ctx.Err()
				}
				release()

				// Cross the drain bound while doing consumer work, not waiting for input.
				time.Sleep(3 * subprocess.InheritedOutputCloseWait)
				return sourcearchive.ExtractTar(ctx, io.MultiReader(bytes.NewReader(prefix), input), outputRoot)
			})
			if err != nil {
				t.Fatalf("finite archive consumer failed: %v", err)
			}
			payload, err := os.ReadFile(filepath.Join(outputRoot, "payload.txt"))
			if err != nil || string(payload) != "payload" {
				t.Fatalf("extracted payload = %q, %v; want payload", payload, err)
			}
		})
	}
}
