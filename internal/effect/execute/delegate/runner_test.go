package delegate

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
)

func TestDefaultCommandRunnerExecutesPlainCommand(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go executable is unavailable")
	}
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     "go",
		args:        []string{"env", "GOVERSION"},
	})
	executor := NewExecutor(Options{Timeout: 5 * time.Second, OutputLimit: 128})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if record.Status() != AttemptSucceeded || record.Reason() != ReasonNone {
		t.Fatalf("record = %#v, want successful go env execution", record)
	}
	if !strings.HasPrefix(strings.TrimSpace(record.Stdout()), "go") {
		t.Fatalf("stdout = %q, want GOVERSION", record.Stdout())
	}
}

func TestDefaultCommandRunnerExecutesExactAbsolutePath(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go executable is unavailable")
	}
	goPath, err = filepath.Abs(goPath)
	if err != nil {
		t.Fatalf("Abs(go executable) returned error: %v", err)
	}
	action := testAction(t, testActionInput{
		disposition: reconcile.DelegateScheduled,
		mode:        delegatepolicy.ModeApply,
		command:     goPath,
		args:        []string{"env", "GOVERSION"},
	})
	executor := NewExecutor(Options{Timeout: 5 * time.Second, OutputLimit: 128})

	record := executor.Execute(context.Background(), action, testWorkingDirectoryBinder(t))

	if record.Status() != AttemptSucceeded || record.Reason() != ReasonNone {
		t.Fatalf("record = %#v, want successful exact-path execution", record)
	}
	if !strings.HasPrefix(strings.TrimSpace(record.Stdout()), "go") {
		t.Fatalf("stdout = %q, want GOVERSION", record.Stdout())
	}
}
