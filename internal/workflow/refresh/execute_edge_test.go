package refresh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestRefreshEdgeRound2AuthorizationAndAuthorityDrift(t *testing.T) {
	t.Run("timeout and workdir authority remain independent", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		root := filepath.Dir(manifestPath)
		moved := root + "-moved"
		t.Cleanup(func() {
			_ = os.RemoveAll(root)
			_ = os.RemoveAll(moved)
		})
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		if err != nil {
			t.Fatalf("PlanWrite returned error: %v", err)
		}

		result, err := Execute(context.Background(), prepared, ExecuteOptions{
			CommandOptions: subprocess.CommandOptions{
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					if renameErr := os.Rename(root, moved); renameErr != nil {
						t.Fatalf("move selected root: %v", renameErr)
					}
					if mkdirErr := os.Mkdir(root, 0o700); mkdirErr != nil {
						t.Fatalf("create replacement root: %v", mkdirErr)
					}
					return subprocess.CommandResult{
						Started:  true,
						TimedOut: true,
						Err:      context.DeadlineExceeded,
					}
				},
			},
		})
		if err == nil ||
			result.ResultClass != ResultPartial ||
			result.ProcessOutcome == nil ||
			result.ProcessOutcome.Reason != subprocess.CommandReasonTimeout ||
			!result.ProcessOutcome.TimedOut ||
			result.AuthorityOutcome == nil ||
			!result.AuthorityOutcome.WorkDirFailed {
			t.Fatalf("result = %#v, error = %v", result, err)
		}
	})

	t.Run("presentation snapshot cannot mutate the executable plan", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		if err != nil {
			t.Fatalf("PlanWrite returned error: %v", err)
		}
		t.Cleanup(func() { _ = prepared.Close() })
		disclosure := prepared.Disclosure()
		disclosure.Disclosure.Invocation.Args[0] = "attacker-controlled"
		disclosure.Disclosure.NonClaims[0] = "claim_rollback"

		calls := 0
		result, err := Execute(context.Background(), prepared, ExecuteOptions{
			CommandOptions: subprocess.CommandOptions{
				Runner: func(
					_ context.Context,
					request subprocess.CommandRequest,
				) subprocess.CommandResult {
					calls++
					if !slices.Equal(
						request.Args,
						[]string{"refresh", "formatter"},
					) {
						t.Fatalf("request args = %v", request.Args)
					}
					return successfulRefreshCommandResult()
				},
			},
		})
		if err != nil || result.ResultClass != ResultAttemptedUnverified {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		if calls != 1 {
			t.Fatalf("runner calls = %d, want 1", calls)
		}
	})

	t.Run("cancel through a value copy invalidates every alias", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		if err != nil {
			t.Fatalf("PlanWrite returned error: %v", err)
		}
		alias := *prepared
		cancelled, err := Cancel(&alias)
		if err != nil {
			t.Fatalf("Cancel returned error: %v", err)
		}
		if cancelled.ResultClass != ResultCancelled || cancelled.Attempted {
			t.Fatalf("cancelled result = %#v", cancelled)
		}
		if _, err := Execute(
			context.Background(),
			prepared,
			ExecuteOptions{},
		); !errors.Is(err, ErrPreparedCommandClosed) {
			t.Fatalf("Execute error = %v, want ErrPreparedCommandClosed", err)
		}
	})

	t.Run("context cancellation after disclosure prevents launch", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		if err != nil {
			t.Fatalf("PlanWrite returned error: %v", err)
		}
		t.Cleanup(func() { _ = prepared.Close() })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		result, err := Execute(ctx, prepared, ExecuteOptions{
			CommandOptions: subprocess.CommandOptions{
				Runner: func(
					context.Context,
					subprocess.CommandRequest,
				) subprocess.CommandResult {
					calls++
					return successfulRefreshCommandResult()
				},
			},
		})
		if !errors.Is(err, context.Canceled) ||
			result.ResultClass != ResultCancelled ||
			result.Attempted ||
			calls != 0 {
			t.Fatalf("result=%#v calls=%d err=%v", result, calls, err)
		}
	})

	t.Run("semantic-neutral lock byte drift invalidates disclosure", func(t *testing.T) {
		manifestPath := writeNoObserverRefreshFixture(t)
		paths, err := daempaths.Resolve(manifestPath)
		if err != nil {
			t.Fatalf("Resolve returned error: %v", err)
		}
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		if err != nil {
			t.Fatalf("PlanWrite returned error: %v", err)
		}
		t.Cleanup(func() { _ = prepared.Close() })
		lockBytes, err := os.ReadFile(paths.LockfilePath)
		if err != nil {
			t.Fatalf("ReadFile lock: %v", err)
		}
		if err := os.WriteFile(
			paths.LockfilePath,
			append(lockBytes, '\n'),
			0o600,
		); err != nil {
			t.Fatalf("WriteFile lock: %v", err)
		}
		calls := 0
		result, err := Execute(context.Background(), prepared, ExecuteOptions{
			CommandOptions: subprocess.CommandOptions{
				Runner: func(
					context.Context,
					subprocess.CommandRequest,
				) subprocess.CommandResult {
					calls++
					return successfulRefreshCommandResult()
				},
			},
		})
		if err == nil ||
			result.ResultClass != ResultRefused ||
			result.ReasonCode != ReasonStalePlan ||
			calls != 0 {
			t.Fatalf("result=%#v calls=%d err=%v", result, calls, err)
		}
	})

	t.Run("observer path order and duplicates canonicalize stably", func(t *testing.T) {
		manifestPath, inventoryPath := writeObservedRefreshFixture(t)
		secondPath := writeObserverAuthorityFile(t, filepath.Dir(inventoryPath), "second.json")
		observer := observerWithAuthoritySchedule(
			t,
			inventoryPath,
			func(call int) []string {
				if call%2 == 0 {
					return []string{secondPath, inventoryPath, secondPath}
				}
				return []string{inventoryPath, secondPath}
			},
		)
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "context7",
		}, PlanOptions{
			CommandBuilder: syntheticRefreshCommandBuilder(t),
			Observer:       observer,
		})
		if err != nil {
			t.Fatalf("PlanWrite returned error: %v", err)
		}
		t.Cleanup(func() { _ = prepared.Close() })
		result, err := Execute(context.Background(), prepared, ExecuteOptions{
			CommandOptions: subprocess.CommandOptions{
				Runner: func(
					context.Context,
					subprocess.CommandRequest,
				) subprocess.CommandResult {
					return successfulRefreshCommandResult()
				},
			},
		})
		if err != nil || result.ResultClass != ResultObservedRelation {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})

	t.Run("post-observer authority substitution is partial", func(t *testing.T) {
		manifestPath, inventoryPath := writeObservedRefreshFixture(t)
		replacementPath := writeObserverAuthorityFile(
			t,
			filepath.Dir(inventoryPath),
			"replacement.json",
		)
		observer := observerWithAuthoritySchedule(
			t,
			inventoryPath,
			func(call int) []string {
				if call == 3 {
					return []string{replacementPath}
				}
				return []string{inventoryPath}
			},
		)
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "context7",
		}, PlanOptions{
			CommandBuilder: syntheticRefreshCommandBuilder(t),
			Observer:       observer,
		})
		if err != nil {
			t.Fatalf("PlanWrite returned error: %v", err)
		}
		t.Cleanup(func() { _ = prepared.Close() })
		result, err := Execute(context.Background(), prepared, ExecuteOptions{
			CommandOptions: subprocess.CommandOptions{
				Runner: func(
					context.Context,
					subprocess.CommandRequest,
				) subprocess.CommandResult {
					return successfulRefreshCommandResult()
				},
			},
		})
		if err == nil ||
			result.ResultClass != ResultPartial ||
			result.ReasonCode != ReasonPostObservationFailed ||
			!result.Attempted ||
			!result.AttemptHistory.Persisted {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
}

func successfulRefreshCommandResult() subprocess.CommandResult {
	return subprocess.CommandResult{
		Started:     true,
		ExitCode:    0,
		HasExitCode: true,
	}
}

func writeObserverAuthorityFile(
	t *testing.T,
	root string,
	name string,
) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile observer authority: %v", err)
	}
	return path
}

func observerWithAuthoritySchedule(
	t *testing.T,
	inventoryPath string,
	pathsForCall func(int) []string,
) RelationObserver {
	t.Helper()
	base := syntheticObserver(
		t,
		inventoryPath,
		observerelation.StateExactCorrelation,
		observerelation.StateExactCorrelation,
		observerelation.StateExactCorrelation,
		observerelation.StateExactCorrelation,
	)
	call := 0
	return func(
		ctx context.Context,
		input ObservationRequest,
	) (RelationObservation, error) {
		observation, err := base(ctx, input)
		if err != nil {
			return RelationObservation{}, err
		}
		var paths []observerelation.AuthorityPath
		for _, path := range pathsForCall(call) {
			authorityPath, err := observerelation.NewAuthorityPath(
				path,
				input.Target,
				input.Scope,
			)
			if err != nil {
				return RelationObservation{}, err
			}
			paths = append(paths, authorityPath)
		}
		call++
		observation.AuthorityPaths = paths
		return observation, nil
	}
}
