package mutation

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestContentRevisionRequiresExplicitCaptureMode(t *testing.T) {
	_, err := NewRevisionObservationPass().Capture(t.Context(), RevisionRequest{
		Path:   t.TempDir(),
		Effect: PathEffectReferent,
	})
	if err == nil || err.Error() != "mutation revision request capture mode is required" {
		t.Fatalf("implicit content revision error = %v", err)
	}
}

func TestContentRevisionTreeEntryLimitAcceptsExactAndRejectsOverflow(t *testing.T) {
	root := t.TempDir()
	writeMutationTestFile(t, filepath.Join(root, "one"), "x", 0o600)
	limits := mustRevisionCaptureLimits(t, 1, 1, math.MaxInt64, 2, 2)
	request := NewBoundedContentRevisionRequest(root, PathEffectReferent)

	if _, err := captureRevisionSetWithLimits(t.Context(), limits, request); err != nil {
		t.Fatalf("exact tree entry limit returned error: %v", err)
	}
	writeMutationTestFile(t, filepath.Join(root, "two"), "", 0o600)
	_, err := captureRevisionSetWithLimits(t.Context(), limits, request)
	assertRevisionLimitError(
		t,
		err,
		RevisionLimitTreeEntries,
		1,
		2,
	)
}

func TestContentRevisionTreeDepthLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "one", "two"), 0o700); err != nil {
		t.Fatal(err)
	}
	limits := mustRevisionCaptureLimits(t, 4, 1, math.MaxInt64, 4, 1)
	_, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(root, PathEffectReferent),
	)
	assertRevisionLimitError(
		t,
		err,
		RevisionLimitTreeDepth,
		1,
		2,
	)
}

func TestContentRevisionTreeDepthLimitAcceptsExactBoundary(t *testing.T) {
	root := t.TempDir()
	current := root
	for _, name := range []string{"one", "two"} {
		current = filepath.Join(current, name)
		if err := os.Mkdir(current, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	limits := mustRevisionCaptureLimits(t, 2, 2, math.MaxInt64, 2, 0)
	if _, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(root, PathEffectReferent),
	); err != nil {
		t.Fatalf("exact tree depth limit returned error: %v", err)
	}
}

func TestContentRevisionOperationEntryLimitSpansTrees(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeMutationTestFile(t, filepath.Join(first, "entry"), "", 0o600)
	writeMutationTestFile(t, filepath.Join(second, "entry"), "", 0o600)
	limits := mustRevisionCaptureLimits(t, 2, 1, math.MaxInt64, 1, 1)
	_, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(first, PathEffectReferent),
		NewBoundedContentRevisionRequest(second, PathEffectReferent),
	)
	assertRevisionLimitError(
		t,
		err,
		RevisionLimitOperationEntries,
		1,
		2,
	)
}

func TestDirectoryListingRevisionBoundsImmediateEntriesWithoutTraversingChildren(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "skill")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := child
	for depth := 1; depth <= 65; depth++ {
		nested = filepath.Join(nested, "nested")
		if err := os.Mkdir(nested, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	limits := mustRevisionCaptureLimits(t, 1, 1, math.MaxInt64, 1, 0)
	request := NewBoundedDirectoryListingRevisionRequest(root)
	set, err := captureRevisionSetWithLimits(t.Context(), limits, request)
	if err != nil {
		t.Fatalf("immediate directory listing traversed child content: %v", err)
	}
	writeMutationTestFile(t, filepath.Join(nested, "content"), "changed", 0o600)
	if matches, err := set.MatchesCurrent(t.Context()); err != nil || !matches {
		t.Fatalf("descendant-only listing change = (matches=%t, error=%v), want match", matches, err)
	}
	if err := os.Mkdir(filepath.Join(root, "added"), 0o700); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchesCurrent(t.Context()); err == nil || matches {
		t.Fatalf("changed directory listing = (matches=%t, error=%v), want entry exhaustion", matches, err)
	} else {
		assertRevisionLimitError(t, err, RevisionLimitTreeEntries, 1, 2)
	}
}

func TestContentRevisionOperationByteLimitSpansFiles(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeMutationTestFile(t, first, "123", 0o600)
	writeMutationTestFile(t, second, "456", 0o600)
	limits := mustRevisionCaptureLimits(t, 0, 0, math.MaxInt64, 0, 5)
	_, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(first, PathEffectReferent),
		NewBoundedContentRevisionRequest(second, PathEffectReferent),
	)
	assertRevisionLimitError(
		t,
		err,
		RevisionLimitOperationBytes,
		5,
		6,
	)
}

func TestRevisionObservationPassSharesBudgetAcrossIncrementalCaptures(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeMutationTestFile(t, first, "123", 0o600)
	writeMutationTestFile(t, second, "456", 0o600)
	limits := mustRevisionCaptureLimits(t, 0, 0, math.MaxInt64, 0, 5)
	pass, err := newRevisionObservationPass(limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pass.Capture(
		t.Context(),
		NewBoundedContentRevisionRequest(first, PathEffectReferent),
	); err != nil {
		t.Fatalf("first incremental capture returned error: %v", err)
	}
	_, err = pass.Capture(
		t.Context(),
		NewBoundedContentRevisionRequest(second, PathEffectReferent),
	)
	assertRevisionLimitError(
		t,
		err,
		RevisionLimitOperationBytes,
		5,
		6,
	)
}

func TestRevisionByteBudgetRejectsOverflow(t *testing.T) {
	limits := mustRevisionCaptureLimits(t, 0, 0, math.MaxInt64, 0, math.MaxInt64-1)
	operation, err := newRevisionCaptureBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	operation.bytes = math.MaxInt64 - 2
	tree, err := operation.beginTree()
	if err != nil {
		t.Fatal(err)
	}

	err = tree.admitBytes(math.MaxInt64)
	assertRevisionLimitError(
		t,
		err,
		RevisionLimitOperationBytes,
		math.MaxInt64-1,
		math.MaxInt64,
	)
}

func TestContentRevisionRevalidationReportsGrowthAsExhaustion(t *testing.T) {
	root := t.TempDir()
	writeMutationTestFile(t, filepath.Join(root, "one"), "", 0o600)
	limits := mustRevisionCaptureLimits(t, 1, 1, math.MaxInt64, 1, 1)
	set, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(root, PathEffectReferent),
	)
	if err != nil {
		t.Fatal(err)
	}
	writeMutationTestFile(t, filepath.Join(root, "two"), "", 0o600)
	matches, err := set.MatchesCurrent(t.Context())
	if matches {
		t.Fatal("over-limit revalidation matched the captured revision")
	}
	assertRevisionLimitError(
		t,
		err,
		RevisionLimitTreeEntries,
		1,
		2,
	)
}

func TestContentRevisionChecksCancellationInsideDirectoryTraversal(t *testing.T) {
	root := t.TempDir()
	writeMutationTestFile(t, filepath.Join(root, "entry"), "value", 0o600)
	ctx := &cancelAfterErrChecks{cancelAt: 5}
	_, err := CaptureRevisionSet(
		ctx,
		NewBoundedContentRevisionRequest(root, PathEffectReferent),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("traversal cancellation error = %v", err)
	}
}

func TestContentRevisionChecksCancellationWhileStreamingFile(t *testing.T) {
	for _, content := range []string{"content", ""} {
		path := filepath.Join(t.TempDir(), "content")
		writeMutationTestFile(t, path, content, 0o600)
		ctx := &cancelAfterErrChecks{cancelAt: 3}
		_, err := CaptureRevisionSet(
			ctx,
			NewBoundedContentRevisionRequest(path, PathEffectReferent),
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("file streaming cancellation for %d bytes = %v", len(content), err)
		}
	}
}

func TestContentRevisionTreeByteLimitAcceptsExactAndRejectsOverflow(t *testing.T) {
	root := t.TempDir()
	writeMutationTestFile(t, filepath.Join(root, "one"), "1234", 0o600)
	writeMutationTestFile(t, filepath.Join(root, "two"), "56", 0o600)
	request := NewBoundedContentRevisionRequest(root, PathEffectReferent)

	exact := mustRevisionCaptureLimits(t, 4, 1, 6, 4, 1<<20)
	if _, err := captureRevisionSetWithLimits(t.Context(), exact, request); err != nil {
		t.Fatalf("exact tree byte limit returned error: %v", err)
	}

	overflow := mustRevisionCaptureLimits(t, 4, 1, 5, 4, 1<<20)
	_, err := captureRevisionSetWithLimits(t.Context(), overflow, request)
	assertRevisionLimitError(t, err, RevisionLimitTreeBytes, 5, 6)
}

func TestContentRevisionCompleteFileIgnoresTreeByteLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	writeMutationTestFile(t, path, "12345", 0o600)
	limits := mustRevisionCaptureLimits(t, 0, 0, 3, 0, 1<<20)
	if _, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(path, PathEffectReferent),
	); err != nil {
		t.Fatalf("complete-content file observed tree bytes: %v", err)
	}
}

func TestDirectoryListingRevisionDoesNotChargeTreeBytes(t *testing.T) {
	root := t.TempDir()
	writeMutationTestFile(t, filepath.Join(root, "payload"), "12345", 0o600)
	limits := mustRevisionCaptureLimits(t, 1, 0, 0, 1, 0)
	if _, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedDirectoryListingRevisionRequest(root),
	); err != nil {
		t.Fatalf("directory listing charged tree bytes: %v", err)
	}
}

func TestContentRevisionOperationBytesStillBindAcrossTrees(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeMutationTestFile(t, filepath.Join(first, "entry"), "1234", 0o600)
	writeMutationTestFile(t, filepath.Join(second, "entry"), "56", 0o600)
	limits := mustRevisionCaptureLimits(t, 4, 1, 8, 4, 5)
	_, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(first, PathEffectReferent),
		NewBoundedContentRevisionRequest(second, PathEffectReferent),
	)
	assertRevisionLimitError(t, err, RevisionLimitOperationBytes, 5, 6)
}

func mustRevisionCaptureLimits(
	t *testing.T,
	maximumTreeEntries int,
	maximumTreeDepth int,
	maximumTreeBytes int64,
	maximumOperationEntries int,
	maximumOperationBytes int64,
) revisionCaptureLimits {
	t.Helper()
	limits, err := newRevisionCaptureLimits(
		maximumTreeEntries,
		maximumTreeDepth,
		maximumTreeBytes,
		maximumOperationEntries,
		maximumOperationBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	return limits
}

func assertRevisionLimitError(
	t *testing.T,
	err error,
	wantKind RevisionLimitKind,
	wantLimit int64,
	wantObserved int64,
) {
	t.Helper()
	if !errors.Is(err, ErrRevisionLimitExceeded) {
		t.Fatalf("error = %v, want ErrRevisionLimitExceeded", err)
	}
	var limitErr *RevisionLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want RevisionLimitError", err)
	}
	if limitErr.Kind() != wantKind ||
		limitErr.Limit() != wantLimit || limitErr.Observed() != wantObserved {
		t.Fatalf(
			"limit error = %#v, want kind=%q limit=%d observed=%d",
			limitErr,
			wantKind,
			wantLimit,
			wantObserved,
		)
	}
}

type cancelAfterErrChecks struct {
	checks   atomic.Int32
	cancelAt int32
}

func (*cancelAfterErrChecks) Deadline() (time.Time, bool) { return time.Time{}, false }

func (*cancelAfterErrChecks) Done() <-chan struct{} { return nil }

func (ctx *cancelAfterErrChecks) Err() error {
	if ctx.checks.Add(1) >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

func (*cancelAfterErrChecks) Value(any) any { return nil }
