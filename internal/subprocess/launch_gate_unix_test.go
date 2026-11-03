//go:build darwin || linux

package subprocess

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/subprocess/executioncontext"
)

type gateTestProcess struct {
	gate     *invocationGate
	root     string
	sentinel string
	waited   bool
}

func newGateTestProcess(t *testing.T, ctx context.Context) *gateTestProcess {
	t.Helper()
	root := t.TempDir()
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directory.Close() })
	invocation, err := executionidentity.NewInvocation(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "target-ran")
	gate, err := prepareGatedCommandInWorkingDirectory(ctx, "/bin/sh",
		[]string{"-c", `printf '%s' "$PWD" > "$1"`, "gate-test", sentinel},
		InheritedChildEnvironment().Entries(), directory, invocation)
	if err != nil {
		t.Fatal(err)
	}
	gate.command.Stdout, gate.command.Stderr = io.Discard, io.Discard
	gate.command.WaitDelay = InheritedOutputCloseWait
	fixture := &gateTestProcess{gate: gate, root: root, sentinel: sentinel}
	t.Cleanup(func() {
		gate.close()
		if gate.group != nil && !fixture.waited {
			_, _ = gate.group.Terminate()
			waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := gate.group.Await(waitCtx, InheritedOutputCloseWait); errors.Is(err, ErrProcessWaitAbandoned) {
				t.Error("gate test helper wait was abandoned")
			}
		}
	})
	return fixture
}

func (fixture *gateTestProcess) await(t *testing.T) error {
	t.Helper()
	if fixture.gate.group == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := fixture.gate.group.Await(ctx, InheritedOutputCloseWait)
	fixture.waited = !errors.Is(err, ErrProcessWaitAbandoned)
	if !fixture.waited {
		t.Fatal("gate test helper wait was abandoned")
	}
	_, cleanupErr := fixture.gate.group.ReapAfterLeaderExit()
	deadline := time.Now().Add(5 * time.Second)
	for {
		occupancy, probeErr := executioncontext.ObserveGroup(fixture.gate.command.Process.Pid)
		if probeErr != nil {
			t.Fatal("gate test group observation failed", probeErr)
		}
		if occupancy == executioncontext.GroupAbsent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("gate test group remained occupied after cleanup", cleanupErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return err
}

func (fixture *gateTestProcess) requireTargetAbsent(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(fixture.sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("target ran before a confirmed gate release", err)
	}
}

func TestInvocationGateWaitsForExactBindingBeforeTargetExec(t *testing.T) {
	fixture := newGateTestProcess(t, t.Context())
	confirmed := false
	err := fixture.gate.startAndRelease(func(ctx context.Context, binding executionidentity.Binding) error {
		fixture.requireTargetAbsent(t)
		if ctx.Err() != nil || binding.Validate() != nil || binding.Invocation() != fixture.gate.invocation ||
			binding.Group() != int64(fixture.gate.command.Process.Pid) || !binding.Context().HasVisibility() {
			t.Fatal("gate attempted to publish an unrelated or unqualified binding")
		}
		confirmed = true
		return nil
	})
	if err != nil || !confirmed || !fixture.gate.releaseAttempted {
		t.Fatal("confirmed invocation was not released", err)
	}
	if err := fixture.await(t); err != nil {
		t.Fatal("released target failed", err)
	}
	content, err := os.ReadFile(fixture.sentinel)
	if err != nil {
		t.Fatal("released target did not run", err)
	}
	actual, err := os.Stat(string(content))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(fixture.root)
	if err != nil || !os.SameFile(actual, expected) {
		t.Fatal("released target did not retain descriptor-backed working directory", err)
	}
	if fixture.gate.startAndRelease(func(context.Context, executionidentity.Binding) error { return nil }) == nil {
		t.Fatal("one gate was reused to launch another target")
	}
}

func TestInvocationGateNeverReleasesOnPublicationFailureOrCancellation(t *testing.T) {
	for _, mode := range []string{"failure", "indeterminate", "cancel", "missing publication", "wrong readiness", "missing target"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture := newGateTestProcess(t, ctx)
			called := false
			var confirm func(context.Context, executionidentity.Binding) error
			if mode != "missing publication" {
				confirm = func(context.Context, executionidentity.Binding) error {
					called = true
					fixture.requireTargetAbsent(t)
					if mode == "indeterminate" {
						if err := os.WriteFile(filepath.Join(fixture.root, "published-before-error"), []byte("bound"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "cancel" {
						cancel()
						return nil
					}
					return errors.New("publication was not confirmed")
				}
			}
			if mode == "wrong readiness" {
				fixture.gate.command.Args[2] = strings.Repeat("b", 32)
			}
			if mode == "missing target" {
				fixture.gate.command.Args = fixture.gate.command.Args[:3]
			}
			if err := fixture.gate.startAndRelease(confirm); err == nil || fixture.gate.releaseAttempted {
				t.Fatal("unconfirmed invocation reached release", err)
			}
			if (mode == "missing publication" || mode == "wrong readiness" || mode == "missing target") && called {
				t.Fatal("invalid helper reached durable binding publication")
			}
			_ = fixture.await(t)
			fixture.requireTargetAbsent(t)
			if mode == "indeterminate" {
				if _, err := os.Stat(filepath.Join(fixture.root, "published-before-error")); err != nil {
					t.Fatal("gate rollback hid an indeterminate publication prefix", err)
				}
			}
		})
	}
}

func TestInvocationGateHelperEOFBeforeBindingExecutesNoTarget(t *testing.T) {
	fixture := newGateTestProcess(t, t.Context())
	group, err := BindProcessGroup(fixture.gate.command)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.gate.command.Start(); err != nil {
		t.Fatal(err)
	}
	fixture.gate.group = group
	fixture.gate.closeChildEndpoints()
	if err := fixture.gate.release.Close(); err != nil {
		t.Fatal(err)
	}
	_ = fixture.await(t)
	fixture.requireTargetAbsent(t)
}
