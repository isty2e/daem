package resolution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func TestGitRepositoryBatchAcquisitionCountIsConstantAcrossGroupSizes(t *testing.T) {
	for _, size := range []int{1, 2, 8, 32} {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			backend := newRepositoryBatchBackend(t)
			resolver := Resolver{git: backend}
			requests := repositoryBatchRequests(t, "https://example.test/repository.git", "main", size, 0)
			events := &repositoryBatchEventRecorder{}

			results, err := resolver.ResolveBatch(
				context.Background(),
				requests,
				acquisition.NewBatchOptions(8, events.record),
			)
			if err != nil {
				t.Fatalf("ResolveBatch returned error: %v", err)
			}
			if len(results) != size {
				t.Fatalf("result count = %d, want %d", len(results), size)
			}
			for index, result := range results {
				resolution, ok := result.Resolution()
				if !result.Request().Equal(requests[index]) || result.Err() != nil || !ok || resolution.Identity().SourceID() == "" {
					t.Fatalf("result[%d] = %#v, want successful stable slot for %#v", index, result, requests[index])
				}
			}
			if backend.acquisitionCount() != 1 {
				t.Fatalf("repository acquisition count = %d, want 1", backend.acquisitionCount())
			}
			if backend.resolveCount() != size {
				t.Fatalf("path resolve count = %d, want %d", backend.resolveCount(), size)
			}
			wantSourceID := mustSourceID(t, requests[0].Source())
			if got := backend.acquisitionSourceIDs(); len(got) != 1 || got[0] != wantSourceID {
				t.Fatalf("acquisition source IDs = %#v, want stable representative %q", got, wantSourceID)
			}
			if got := events.requestIDs(acquisition.EventFetch); len(got) != 1 || got[0] != requests[0].ID() {
				t.Fatalf("fetch event request IDs = %#v, want %q", got, requests[0].ID())
			}
		})
	}
}

func TestGitRepositoryBatchKeepsCanonicalRefGroupsDistinct(t *testing.T) {
	backend := newRepositoryBatchBackend(t)
	resolver := Resolver{git: backend}
	refs := []string{
		"main",
		"refs/heads/main",
		"refs/tags/main",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	requests := make([]acquisition.Request, 0, len(refs)*2)
	for groupIndex, ref := range refs {
		requests = append(requests, repositoryBatchRequests(
			t,
			"https://example.test/repository.git",
			ref,
			2,
			groupIndex*2,
		)...)
	}

	results, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(8, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	if len(results) != len(requests) {
		t.Fatalf("result count = %d, want %d", len(results), len(requests))
	}
	if backend.acquisitionCount() != len(refs) {
		t.Fatalf("repository acquisition count = %d, want %d distinct canonical refs", backend.acquisitionCount(), len(refs))
	}
	wantRepresentatives := []artifact.SourceID{
		mustSourceID(t, requests[0].Source()),
		mustSourceID(t, requests[2].Source()),
		mustSourceID(t, requests[4].Source()),
		mustSourceID(t, requests[6].Source()),
	}
	if got := backend.acquisitionSourceIDs(); !equalSourceIDSets(got, wantRepresentatives) {
		t.Fatalf("acquisition source IDs = %#v, want %#v", got, wantRepresentatives)
	}
}

func TestGitRepositoryBatchPreparationFailureFansOutAndRetries(t *testing.T) {
	backend := newRepositoryBatchBackend(t)
	backend.acquisitionErr = errors.New("repository unavailable")
	resolver := Resolver{git: backend}
	requests := repositoryBatchRequests(t, "https://example.test/repository.git", "main", 4, 0)

	results, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(4, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned top-level error: %v", err)
	}
	for index, result := range results {
		if !errors.Is(result.Err(), backend.acquisitionErr) {
			t.Fatalf("result[%d].Err = %v, want shared repository error", index, result.Err())
		}
	}
	if backend.acquisitionCount() != 1 || backend.resolveCount() != 0 {
		t.Fatalf("counts after failure = acquisition %d resolve %d, want 1/0", backend.acquisitionCount(), backend.resolveCount())
	}

	backend.setAcquisitionError(nil)
	retried, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(4, nil))
	if err != nil {
		t.Fatalf("retry ResolveBatch returned error: %v", err)
	}
	for index, result := range retried {
		if result.Err() != nil {
			t.Fatalf("retry result[%d].Err = %v, want success", index, result.Err())
		}
	}
	if backend.acquisitionCount() != 2 || backend.resolveCount() != len(requests) {
		t.Fatalf("counts after retry = acquisition %d resolve %d, want 2/%d", backend.acquisitionCount(), backend.resolveCount(), len(requests))
	}
}

func TestGitRepositoryBatchCancellationStopsLeaderAndFollowers(t *testing.T) {
	backend := newRepositoryBatchBackend(t)
	backend.acquisitionStarted = make(chan artifact.SourceID, 1)
	backend.acquisitionRelease = make(chan struct{})
	resolver := Resolver{git: backend}
	requests := repositoryBatchRequests(t, "https://example.test/repository.git", "main", 8, 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveBatch(ctx, requests, acquisition.NewBatchOptions(8, nil))
		done <- err
	}()

	select {
	case sourceID := <-backend.acquisitionStarted:
		wantSourceID := mustSourceID(t, requests[0].Source())
		if sourceID != wantSourceID {
			t.Fatalf("acquisition representative = %q, want %q", sourceID, wantSourceID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("repository leader did not reach deterministic barrier")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ResolveBatch error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResolveBatch did not terminate after cancellation")
	}
	if backend.acquisitionCount() != 1 || backend.resolveCount() != 0 {
		t.Fatalf("counts after cancellation = acquisition %d resolve %d, want 1/0", backend.acquisitionCount(), backend.resolveCount())
	}
}

func TestGitRepositoryBatchLaunchesUnrelatedLeadersBeforeFollowers(t *testing.T) {
	backend := newRepositoryBatchBackend(t)
	backend.acquisitionStarted = make(chan artifact.SourceID, 4)
	release := make(chan struct{})
	backend.acquisitionRelease = release
	resolver := Resolver{git: backend}
	requests := append(
		repositoryBatchRequests(t, "https://example.test/alpha.git", "main", 2, 0),
		repositoryBatchRequests(t, "https://example.test/beta.git", "main", 2, 2)...,
	)
	done := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(2, nil))
		done <- err
	}()

	started := []artifact.SourceID{waitForRepositoryStart(t, backend.acquisitionStarted), waitForRepositoryStart(t, backend.acquisitionStarted)}
	want := []artifact.SourceID{mustSourceID(t, requests[0].Source()), mustSourceID(t, requests[2].Source())}
	if !equalSourceIDSets(started, want) {
		t.Fatalf("first acquisition leaders = %#v, want %#v", started, want)
	}
	if backend.maxActiveAcquisitions() != 2 {
		t.Fatalf("max active acquisitions = %d, want 2", backend.maxActiveAcquisitions())
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResolveBatch returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResolveBatch did not finish after releasing repository barriers")
	}
}

func TestGitRepositoryBatchSharesPreparationAcrossListAndResolve(t *testing.T) {
	backend := newRepositoryBatchBackend(t)
	resolver := Resolver{git: backend}
	sourceSpec := mustGitSource(t, "https://example.test/repository.git", "skills", "main")
	requests := []acquisition.Request{
		batchRequest("root", 0, acquisition.OperationListRoot, sourceSpec),
		batchRequest("artifact", 1, acquisition.OperationResolve, sourceSpec),
	}

	results, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(2, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	if backend.acquisitionCount() != 1 || backend.resolveCount() != 1 || backend.listCount() != 1 {
		t.Fatalf("counts = acquisition %d resolve %d list %d, want 1/1/1", backend.acquisitionCount(), backend.resolveCount(), backend.listCount())
	}
	listing, listingOK := results[0].Listing()
	resolved, resolvedOK := results[1].Resolution()
	if !listingOK || !resolvedOK || listing.Kind() != artifact.ArtifactKindDirectory || resolved.Identity().Kind() != artifact.ArtifactKindDirectory {
		t.Fatalf("mixed operation results = %#v, want listing then artifact", results)
	}
}

func TestRepositoryBatchRejectsInvalidCapabilityGroup(t *testing.T) {
	backend := &invalidRepositoryGroupBackend{repositoryBatchBackend: newRepositoryBatchBackend(t)}
	resolver := Resolver{git: backend}
	requests := repositoryBatchRequests(t, "https://example.test/repository.git", "main", 2, 0)

	_, err := resolver.ResolveBatch(context.Background(), requests, acquisition.NewBatchOptions(2, nil))
	if err == nil || !strings.Contains(err.Error(), "repository snapshot group locator is required") {
		t.Fatalf("ResolveBatch error = %v, want invalid capability group rejection", err)
	}
	if backend.acquisitionCount() != 0 {
		t.Fatalf("repository acquisition count = %d, want 0", backend.acquisitionCount())
	}
}

type invalidRepositoryGroupBackend struct {
	*repositoryBatchBackend
}

func (*invalidRepositoryGroupBackend) RepositorySnapshotGroup(
	source.Source,
) (acquisition.RepositorySnapshotGroupID, bool, error) {
	return acquisition.RepositorySnapshotGroupID{}, true, nil
}
