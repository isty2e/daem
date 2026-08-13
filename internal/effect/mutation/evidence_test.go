package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRevisionSetMatchesCurrentAndDetectsChangedAliasTopology(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.Mkdir(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "value"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "value"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}

	set, err := CaptureRevisionSet(
		context.Background(),
		NewBoundedContentRevisionRequest(filepath.Join(alias, "value"), PathEffectReferent),
	)
	if err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchesCurrent(context.Background()); err != nil || !matches {
		t.Fatalf("MatchesCurrent() = %t, %v; want true", matches, err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchesCurrent(context.Background()); err != nil || matches {
		t.Fatalf("MatchesCurrent() = %t, %v; want false", matches, err)
	}
}

func TestRevisionSetRejectsInvalidStateAndCancellation(t *testing.T) {
	if _, err := CaptureRevisionSet(context.Background()); err == nil {
		t.Fatal("CaptureRevisionSet accepted an empty request set")
	}
	if _, err := (RevisionSet{}).MatchesCurrent(context.Background()); err == nil {
		t.Fatal("zero RevisionSet matched current state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CaptureRevisionSet(
		ctx,
		NewBoundedContentRevisionRequest(t.TempDir(), PathEffectReferent),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("CaptureRevisionSet error = %v, want context cancellation", err)
	}
}

func TestRevisionSetSubsetReusesCapturedEvidence(t *testing.T) {
	root := t.TempDir()
	stablePath := filepath.Join(root, "stable")
	changingPath := filepath.Join(root, "changing")
	if err := os.WriteFile(stablePath, []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changingPath, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	stableRequest := NewBoundedContentRevisionRequest(stablePath, PathEffectReferent)
	changingRequest := NewBoundedContentRevisionRequest(changingPath, PathEffectReferent)
	set, err := CaptureRevisionSet(t.Context(), stableRequest, changingRequest)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := set.Subset(stableRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changingPath, []byte("after!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchesCurrent(t.Context()); err != nil || matches {
		t.Fatalf("complete set match = %t, %v; want false", matches, err)
	}
	if matches, err := stable.MatchesCurrent(t.Context()); err != nil || !matches {
		t.Fatalf("stable subset match = %t, %v; want true", matches, err)
	}
	missing := NewBoundedContentRevisionRequest(filepath.Join(root, "missing"), PathEffectReferent)
	if _, err := set.Subset(missing); err == nil {
		t.Fatal("revision subset accepted an uncaptured request")
	}
}

func TestBoundedFileRevisionSetTracksRegularFilesAliasesAndAbsence(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	alias := filepath.Join(root, "selected")
	missing := filepath.Join(root, "missing")
	if err := os.WriteFile(first, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	set, err := CaptureBoundedFileRevisionSet(
		t.Context(),
		4,
		alias,
		missing,
	)
	if err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchesCurrent(t.Context()); err != nil || !matches {
		t.Fatalf("MatchesCurrent() = %t, %v; want true", matches, err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchesCurrent(t.Context()); err != nil || matches {
		t.Fatalf("alias replacement match = %t, %v; want false", matches, err)
	}
}

func TestBoundedFileRevisionSetRejectsDirectoriesAndOversizedFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := CaptureBoundedFileRevisionSet(t.Context(), 4, root); err == nil {
		t.Fatal("bounded file revision accepted a directory")
	}

	path := filepath.Join(root, "oversized")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureBoundedFileRevisionSet(t.Context(), 4, path); err == nil {
		t.Fatal("bounded file revision accepted an oversized file")
	}
}

func TestBoundedFileRevisionSetDetectsContentChangeAndMissingFileCreation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "selected")
	missing := filepath.Join(root, "missing")
	if err := os.WriteFile(path, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := CaptureBoundedFileRevisionSet(t.Context(), 4, path, missing)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("diff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchesCurrent(t.Context()); err != nil || matches {
		t.Fatalf("content-change match = %t, %v; want false", matches, err)
	}

	if err := os.WriteFile(path, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missing, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchesCurrent(t.Context()); err != nil || matches {
		t.Fatalf("missing-file creation match = %t, %v; want false", matches, err)
	}
}

func TestBoundedFileRevisionSetRejectsInvalidInput(t *testing.T) {
	if _, err := CaptureBoundedFileRevisionSet(t.Context(), 0, "value"); err == nil {
		t.Fatal("bounded file revision accepted a zero byte limit")
	}
	if _, err := CaptureBoundedFileRevisionSet(t.Context(), 1); err == nil {
		t.Fatal("bounded file revision accepted an empty path set")
	}
}

func TestStaleSnapshotErrorHasStableTypeAndSafeMessage(t *testing.T) {
	err := error(StaleSnapshotError{})
	var stale StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("errors.As(%T) = false", err)
	}
	if got := err.Error(); got != "stale_snapshot: authoritative inputs changed; rerun the command from current state" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestStalePlanErrorHasStableTypeAndSafeMessage(t *testing.T) {
	err := error(StalePlanError{})
	var stale StalePlanError
	if !errors.As(err, &stale) {
		t.Fatalf("errors.As(%T) = false", err)
	}
	if got := err.Error(); got != "stale_plan: the disclosed operation changed; review and confirm a new plan" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestMutationErrorsHaveStableReasonCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ReasonCode
	}{
		{name: "stale snapshot", err: StaleSnapshotError{}, want: ReasonStaleSnapshot},
		{name: "stale plan", err: StalePlanError{}, want: ReasonStalePlan},
		{name: "contention", err: ContentionError{}, want: ReasonContention},
		{name: "canceled", err: CancellationError{cause: context.Canceled}, want: ReasonCanceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wrapped := errors.Join(errors.New("operation failed"), test.err)
			if got, ok := ReasonCodeOf(wrapped); !ok || got != test.want {
				t.Fatalf("ReasonCodeOf(%v) = %q, %t; want %q, true", wrapped, got, ok, test.want)
			}
		})
	}
	if got, ok := ReasonCodeOf(errors.New("unclassified")); ok || got != "" {
		t.Fatalf("unclassified reason = %q, %t; want empty, false", got, ok)
	}
}
