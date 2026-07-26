package refresh

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestRefreshEdgeRound1SelectionAndLockRefusals(t *testing.T) {
	t.Run("whitespace is not normalized into an exact id", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		result, err := PlanDryRun(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  " formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		assertRefreshRefusal(t, result, err, ReasonInvalidSelection)
	})

	t.Run("target selector cannot retarget the relation", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		result, err := PlanDryRun(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
			TargetValue:  "claude-code",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		assertRefreshRefusal(t, result, err, ReasonInvalidSelection)
	})

	t.Run("scope selector cannot rescope the relation", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		result, err := PlanDryRun(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
			ScopeValue:   "global",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		assertRefreshRefusal(t, result, err, ReasonInvalidSelection)
	})

	t.Run("missing lock never falls through to route construction", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		paths, err := daempaths.Resolve(manifestPath)
		if err != nil {
			t.Fatalf("Resolve returned error: %v", err)
		}
		if err := os.Remove(paths.LockfilePath); err != nil {
			t.Fatalf("Remove lockfile: %v", err)
		}
		result, err := PlanDryRun(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: failIfRefreshBuilderCalled(t)})
		assertRefreshRefusal(t, result, err, ReasonLockUnavailable)
	})

	t.Run("changed selected source makes the lock stale", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("ReadFile manifest: %v", err)
		}
		changed := []byte(string(content))
		oldSource := []byte(`host_source = "@acme/formatter"`)
		newSource := []byte(`host_source = "@acme/formatter-next"`)
		index := bytes.Index(changed, oldSource)
		if index < 0 {
			t.Fatal("fixture source was not found")
		}
		changed = bytes.Replace(changed, oldSource, newSource, 1)
		if err := os.WriteFile(manifestPath, changed, 0o600); err != nil {
			t.Fatalf("WriteFile manifest: %v", err)
		}
		result, err := PlanDryRun(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: failIfRefreshBuilderCalled(t)})
		assertRefreshRefusal(t, result, err, ReasonLockMismatch)
	})

	t.Run("nil context is a typed cancellation", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		result, err := PlanDryRun(nil, CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: failIfRefreshBuilderCalled(t)})
		assertRefreshRefusal(t, result, err, ReasonCancelled)
	})
}

func assertRefreshRefusal(
	t *testing.T,
	result CommandResult,
	err error,
	wantCode ReasonCode,
) {
	t.Helper()
	if err == nil {
		t.Fatal("refresh planning returned nil error")
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %T %v, want RefusalError", err, err)
	}
	if refusal.Code() != wantCode ||
		result.ReasonCode != wantCode ||
		!result.HasErrors() {
		t.Fatalf("result=%#v refusal=%v, want code %q", result, refusal, wantCode)
	}
}

func failIfRefreshBuilderCalled(t *testing.T) CommandBuilder {
	t.Helper()
	return func(CommandBuildInput) (CommandSpec, error) {
		t.Fatal("refresh command builder was called after an earlier refusal")
		return CommandSpec{}, nil
	}
}
