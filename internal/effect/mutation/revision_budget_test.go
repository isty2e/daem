package mutation

import (
	"context"
	"errors"
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
	limits := mustRevisionCaptureLimits(t, 1, 1, 2, 2)
	request := NewBoundedContentRevisionRequest(root, PathEffectReferent)

	if _, err := captureRevisionSetWithLimits(t.Context(), limits, request); err != nil {
		t.Fatalf("exact tree entry limit returned error: %v", err)
	}
	writeMutationTestFile(t, filepath.Join(root, "two"), "", 0o600)
	_, err := captureRevisionSetWithLimits(t.Context(), limits, request)
	assertRevisionLimitError(
		t,
		err,
		revisionLimitTree,
		revisionLimitEntries,
		1,
		2,
	)
}

func TestContentRevisionTreeDepthLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "one", "two"), 0o700); err != nil {
		t.Fatal(err)
	}
	limits := mustRevisionCaptureLimits(t, 4, 1, 4, 1)
	_, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(root, PathEffectReferent),
	)
	assertRevisionLimitError(
		t,
		err,
		revisionLimitTree,
		revisionLimitDepth,
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
	limits := mustRevisionCaptureLimits(t, 2, 2, 2, 0)
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
	limits := mustRevisionCaptureLimits(t, 2, 1, 1, 1)
	_, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(first, PathEffectReferent),
		NewBoundedContentRevisionRequest(second, PathEffectReferent),
	)
	assertRevisionLimitError(
		t,
		err,
		revisionLimitOperation,
		revisionLimitEntries,
		1,
		2,
	)
}

func TestContentRevisionOperationByteLimitSpansFiles(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeMutationTestFile(t, first, "123", 0o600)
	writeMutationTestFile(t, second, "456", 0o600)
	limits := mustRevisionCaptureLimits(t, 0, 0, 0, 5)
	_, err := captureRevisionSetWithLimits(
		t.Context(),
		limits,
		NewBoundedContentRevisionRequest(first, PathEffectReferent),
		NewBoundedContentRevisionRequest(second, PathEffectReferent),
	)
	assertRevisionLimitError(
		t,
		err,
		revisionLimitOperation,
		revisionLimitBytes,
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
	limits := mustRevisionCaptureLimits(t, 0, 0, 0, 5)
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
		revisionLimitOperation,
		revisionLimitBytes,
		5,
		6,
	)
}

func TestContentRevisionRevalidationReportsGrowthAsExhaustion(t *testing.T) {
	root := t.TempDir()
	writeMutationTestFile(t, filepath.Join(root, "one"), "", 0o600)
	limits := mustRevisionCaptureLimits(t, 1, 1, 1, 1)
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
		revisionLimitTree,
		revisionLimitEntries,
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
	path := filepath.Join(t.TempDir(), "content")
	writeMutationTestFile(t, path, "content", 0o600)
	ctx := &cancelAfterErrChecks{cancelAt: 3}
	_, err := CaptureRevisionSet(
		ctx,
		NewBoundedContentRevisionRequest(path, PathEffectReferent),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("file streaming cancellation error = %v", err)
	}
}

func mustRevisionCaptureLimits(
	t *testing.T,
	maximumTreeEntries int,
	maximumTreeDepth int,
	maximumOperationEntries int,
	maximumOperationBytes int64,
) revisionCaptureLimits {
	t.Helper()
	limits, err := newRevisionCaptureLimits(
		maximumTreeEntries,
		maximumTreeDepth,
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
	wantScope revisionLimitScope,
	wantResource revisionLimitResource,
	wantLimit int64,
	wantObserved int64,
) {
	t.Helper()
	var limitErr RevisionLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want RevisionLimitError", err)
	}
	if limitErr.scope != wantScope || limitErr.resource != wantResource ||
		limitErr.Limit() != wantLimit || limitErr.Observed() != wantObserved {
		t.Fatalf(
			"limit error = %#v, want scope=%q resource=%q limit=%d observed=%d",
			limitErr,
			wantScope,
			wantResource,
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
