package acquisition

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestNewRequestOwnsRequestValidity(t *testing.T) {
	validSource := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	request, err := NewRequest("skill:000001", 3, OperationResolve, validSource)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	if request.ID() != "skill:000001" || request.Ordinal() != 3 ||
		request.Operation() != OperationResolve || request.Source() != validSource {
		t.Fatalf("request = %#v, want exact constructor facts", request)
	}

	tests := []struct {
		name      string
		id        RequestID
		ordinal   int
		operation Operation
		source    source.Source
		want      string
	}{
		{name: "empty id", ordinal: 0, operation: OperationResolve, source: validSource, want: "id is required"},
		{name: "untrimmed id", id: " request ", ordinal: 0, operation: OperationResolve, source: validSource, want: "must be trimmed"},
		{name: "control id", id: "request\x00", ordinal: 0, operation: OperationResolve, source: validSource, want: "unsafe control"},
		{name: "invalid utf8 id", id: RequestID("request\xff"), ordinal: 0, operation: OperationResolve, source: validSource, want: "valid UTF-8"},
		{name: "negative ordinal", id: "request", ordinal: -1, operation: OperationResolve, source: validSource, want: "non-negative"},
		{name: "unknown operation", id: "request", ordinal: 0, operation: "download", source: validSource, want: "unknown source acquisition operation"},
		{name: "invalid source", id: "request", ordinal: 0, operation: OperationResolve, source: source.Source{}, want: "unsupported source kind"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewRequest(test.id, test.ordinal, test.operation, test.source)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewRequest error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestResolutionRequiresCorrelatedIdentityAndMatchingViewKind(t *testing.T) {
	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	view, identity := testResolutionFacts(t, sourceSpec, true)
	resolution, err := NewResolution(sourceSpec, identity, view)
	if err != nil {
		t.Fatalf("NewResolution returned error: %v", err)
	}
	if !resolution.Identity().Equal(identity) || resolution.View().Kind() != identity.Kind() {
		t.Fatalf("resolution = %#v, want exact identity and matching view", resolution)
	}

	otherSource := sourcetest.Local(t, "skills/other", source.LocalSourceModeVendor)
	if _, err := NewResolution(otherSource, identity, view); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("source mismatch error = %v", err)
	}
	fileView, _ := testResolutionFacts(t, sourceSpec, false)
	if _, err := NewResolution(sourceSpec, identity, fileView); err == nil || !strings.Contains(err.Error(), "view kind") {
		t.Fatalf("kind mismatch error = %v", err)
	}
	if _, err := NewResolution(sourceSpec, artifact.ExactIdentity{}, view); err == nil {
		t.Fatal("NewResolution accepted zero identity")
	}
}

func TestResolutionDoesNotCacheViewLivenessOrIdentityVerification(t *testing.T) {
	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	root := filepath.Join(t.TempDir(), "artifact")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := access.OpenView(root)
	if err != nil {
		t.Fatal(err)
	}
	contentHash, err := view.Hash(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := artifact.NewExactIdentity(sourceID, "", view.Kind(), contentHash)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := NewResolution(sourceSpec, identity, view)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(skillPath, []byte("mutated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := resolution.Validate(sourceSpec); err != nil {
		t.Fatalf("semantic Resolution validation became a liveness probe: %v", err)
	}
	if err := resolution.View().Verify(context.Background(), resolution.Identity()); err == nil {
		t.Fatal("Resolution view retained stale identity authority after source mutation")
	}
}

func TestResultVariantsAreClosedAndRequestCorrelated(t *testing.T) {
	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	resolveRequest := mustTestRequest(t, "resolve", 0, OperationResolve, sourceSpec)
	listRequest := mustTestRequest(t, "list", 1, OperationListRoot, sourceSpec)
	view, identity := testResolutionFacts(t, sourceSpec, true)
	resolution, err := NewResolution(sourceSpec, identity, view)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := NewResolutionResult(resolveRequest, resolution)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := resolved.Resolution(); !ok || !got.Identity().Equal(identity) || resolved.Err() != nil {
		t.Fatalf("resolution result = %#v", resolved)
	}
	if _, ok := resolved.Listing(); ok {
		t.Fatal("resolution result also exposed a listing")
	}
	if _, err := NewResolutionResult(listRequest, resolution); err == nil {
		t.Fatal("NewResolutionResult accepted list-root request")
	}

	listing, err := source.NewRootListing(sourceSpec, "", artifact.ArtifactKindDirectory, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := NewListingResult(listRequest, listing)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := listed.Listing(); !ok || len(got.ChildNames()) != 1 || got.ChildNames()[0] != "alpha" {
		t.Fatalf("listing result = %#v", listed)
	}
	if _, ok := listed.Resolution(); ok {
		t.Fatal("listing result also exposed a resolution")
	}
	if _, err := NewListingResult(resolveRequest, listing); err == nil {
		t.Fatal("NewListingResult accepted resolve request")
	}
	otherSource := sourcetest.Local(t, "other", source.LocalSourceModeVendor)
	otherListing, err := source.NewRootListing(otherSource, "", artifact.ArtifactKindDirectory, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewListingResult(listRequest, otherListing); err == nil {
		t.Fatal("NewListingResult accepted a listing for another source")
	}

	wantErr := errors.New("backend failed")
	failed, err := NewFailureResult(resolveRequest, wantErr)
	if err != nil || !errors.Is(failed.Err(), wantErr) {
		t.Fatalf("failure result = %#v, %v", failed, err)
	}
	if _, err := NewFailureResult(resolveRequest, nil); err == nil {
		t.Fatal("NewFailureResult accepted nil error")
	}
	if err := (Result{}).Validate(); err == nil {
		t.Fatal("zero Result validated")
	}
}

func TestEventRejectsIncoherentProgressFacts(t *testing.T) {
	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	request := mustTestRequest(t, "resolve", 0, OperationResolve, sourceSpec)
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewEvent(EventQueued, request, "", "", nil); err != nil {
		t.Fatalf("queued event returned error: %v", err)
	}
	if _, err := NewEvent(EventCompleted, request, sourceID, "", nil); err != nil {
		t.Fatalf("completed local event returned error: %v", err)
	}
	wantErr := errors.New("failed")
	if _, err := NewEvent(EventFailed, request, "", "", wantErr); err != nil {
		t.Fatalf("pre-correlation failure returned error: %v", err)
	}

	tests := []struct {
		name        string
		kind        EventKind
		sourceID    artifact.SourceID
		resolvedRef artifact.ResolvedRef
		err         error
		want        string
	}{
		{name: "queued identity", kind: EventQueued, sourceID: sourceID, want: "queued"},
		{name: "failed without error", kind: EventFailed, want: "requires an error"},
		{name: "nonfailure with error", kind: EventStarted, sourceID: sourceID, err: wantErr, want: "must not carry an error"},
		{name: "resolved ref without source id", kind: EventFailed, resolvedRef: "unexpected", err: wantErr, want: "without source id"},
		{name: "wrong source id", kind: EventStarted, sourceID: "local:other?mode=vendor", want: "does not match"},
		{name: "invalid utf8 resolved ref", kind: EventCompleted, sourceID: sourceID, resolvedRef: artifact.ResolvedRef("ref\xff"), want: "valid UTF-8"},
		{name: "local resolved ref", kind: EventCompleted, sourceID: sourceID, resolvedRef: "unexpected", want: "must not carry a resolved ref"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewEvent(test.kind, request, test.sourceID, test.resolvedRef, test.err)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewEvent error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestOperationAndBatchOptionsPreserveBoundedRouting(t *testing.T) {
	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	request := mustTestRequest(t, "resolve", 0, OperationResolve, sourceSpec)
	var events []Event
	options, err := NewOperationOptions(request, func(event Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	sourceID, _ := source.SourceIDFor(sourceSpec)
	options.Emit(EventStarted, sourceSpec, sourceID, "", nil)
	options.Emit(EventStarted, sourcetest.Local(t, "skills/other", source.LocalSourceModeVendor), sourceID, "", nil)
	if len(events) != 1 || events[0].Request().ID() != request.ID() {
		t.Fatalf("events = %#v, want only correlated event", events)
	}

	if got := NewBatchOptions(0, nil).NormalizedMaxParallel(); got != 1 {
		t.Fatalf("zero max parallel = %d, want 1", got)
	}
	if got := NewBatchOptions(7, nil).NormalizedMaxParallel(); got != 7 {
		t.Fatalf("positive max parallel = %d, want 7", got)
	}
}

func TestZeroOperationOptionsDropsProgress(t *testing.T) {
	OperationOptions{}.Emit(EventStarted, source.Source{}, "", "", nil)
}

func TestEventSinkPanicPropagatesToInternalCaller(t *testing.T) {
	want := errors.New("event sink panic")
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		EventSink(func(Event) {
			panic(want)
		}).Emit(Event{})
	}()

	if recovered != want {
		t.Fatalf("recovered = %v, want exact event-sink panic", recovered)
	}
}

func TestRepositorySnapshotGroupOwnsCanonicalRepositoryCorrelation(t *testing.T) {
	first, err := source.NewGitSource("https://example.test/repository.git", "skills/alpha", "main")
	if err != nil {
		t.Fatal(err)
	}
	second, err := source.NewGitSource("https://example.test/repository.git", "skills/beta", "main")
	if err != nil {
		t.Fatal(err)
	}
	differentRef, err := source.NewGitSource("https://example.test/repository.git", "skills/alpha", "refs/tags/main")
	if err != nil {
		t.Fatal(err)
	}

	firstGit, _ := first.Git()
	secondGit, _ := second.Git()
	differentRefGit, _ := differentRef.Git()
	firstGroup, err := NewRepositorySnapshotGroupID(firstGit)
	if err != nil {
		t.Fatal(err)
	}
	secondGroup, err := NewRepositorySnapshotGroupID(secondGit)
	if err != nil {
		t.Fatal(err)
	}
	differentRefGroup, err := NewRepositorySnapshotGroupID(differentRefGit)
	if err != nil {
		t.Fatal(err)
	}

	if firstGroup != secondGroup {
		t.Fatal("repository path changed operation-local snapshot correlation")
	}
	if firstGroup == differentRefGroup {
		t.Fatal("different canonical refs shared one repository snapshot group")
	}
	if err := (RepositorySnapshotGroupID{}).Validate(); err == nil {
		t.Fatal("zero repository snapshot group validated")
	}
}

func mustTestRequest(
	t *testing.T,
	id RequestID,
	ordinal int,
	operation Operation,
	sourceSpec source.Source,
) Request {
	t.Helper()
	request, err := NewRequest(id, ordinal, operation, sourceSpec)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func testResolutionFacts(
	t *testing.T,
	sourceSpec source.Source,
	directory bool,
) (access.View, artifact.ExactIdentity) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "artifact")
	if directory {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(root, []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := access.OpenView(root)
	if err != nil {
		t.Fatal(err)
	}
	contentHash, err := view.Hash(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := artifact.NewExactIdentity(sourceID, "", view.Kind(), contentHash)
	if err != nil {
		t.Fatal(err)
	}
	return view, identity
}
