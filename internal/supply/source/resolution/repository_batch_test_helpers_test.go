package resolution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/backend/gitcli"
)

type repositoryBatchBackend struct {
	gitcli.Resolver

	mu sync.Mutex

	completed           map[acquisition.RepositorySnapshotGroupID]struct{}
	acquisitionErr      error
	acquisitionSources  []artifact.SourceID
	resolveRequests     int
	listRequests        int
	activeAcquisitions  int
	maximumAcquisitions int
	acquisitionStarted  chan artifact.SourceID
	acquisitionRelease  <-chan struct{}
	view                access.View
	contentHash         artifact.ContentHash
}

func newRepositoryBatchBackend(t *testing.T) *repositoryBatchBackend {
	t.Helper()
	view, contentHash := newResolutionTestView(t)
	return &repositoryBatchBackend{
		completed:   make(map[acquisition.RepositorySnapshotGroupID]struct{}),
		view:        view,
		contentHash: contentHash,
	}
}

func newResolutionTestView(t *testing.T) (access.View, artifact.ContentHash) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "artifact")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("Mkdir test artifact returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("test artifact\n"), 0o600); err != nil {
		t.Fatalf("WriteFile test artifact returned error: %v", err)
	}
	view, err := access.OpenView(root)
	if err != nil {
		t.Fatalf("OpenView returned error: %v", err)
	}
	contentHash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	return view, contentHash
}

func (backend *repositoryBatchBackend) PrepareRepositorySnapshot(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) error {
	return backend.ensureRepositorySnapshot(ctx, sourceSpec, options)
}

func (backend *repositoryBatchBackend) Resolve(ctx context.Context, sourceSpec source.Source) (acquisition.Resolution, error) {
	return backend.ResolveWithOptions(ctx, sourceSpec, acquisition.OperationOptions{})
}

func (backend *repositoryBatchBackend) ResolveWithOptions(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	if err := backend.ensureRepositorySnapshot(ctx, sourceSpec, options); err != nil {
		return acquisition.Resolution{}, err
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	backend.mu.Lock()
	backend.resolveRequests++
	backend.mu.Unlock()
	resolvedRef := artifact.ResolvedRef(strings.Repeat("a", 40))
	options.Emit(acquisition.EventExport, sourceSpec, sourceID, resolvedRef, nil)
	identity, err := artifact.NewExactIdentity(
		sourceID,
		resolvedRef,
		artifact.ArtifactKindDirectory,
		backend.contentHash,
	)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	return acquisition.NewResolution(sourceSpec, identity, backend.view)
}

func (backend *repositoryBatchBackend) ListSourceRoot(ctx context.Context, sourceSpec source.Source) (source.RootListing, error) {
	return backend.ListSourceRootWithOptions(ctx, sourceSpec, acquisition.OperationOptions{})
}

func (backend *repositoryBatchBackend) ListSourceRootWithOptions(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (source.RootListing, error) {
	if err := backend.ensureRepositorySnapshot(ctx, sourceSpec, options); err != nil {
		return source.RootListing{}, err
	}
	backend.mu.Lock()
	backend.listRequests++
	backend.mu.Unlock()
	return source.NewRootListing(
		sourceSpec,
		artifact.ResolvedRef(strings.Repeat("a", 40)),
		artifact.ArtifactKindDirectory,
		[]string{"alpha", "beta"},
	)
}

func (backend *repositoryBatchBackend) ensureRepositorySnapshot(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) error {
	group, ok, err := backend.RepositorySnapshotGroup(sourceSpec)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("repository batch backend requires Git source")
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return err
	}

	backend.mu.Lock()
	if _, ok := backend.completed[group]; ok {
		backend.mu.Unlock()
		return nil
	}
	backend.acquisitionSources = append(backend.acquisitionSources, sourceID)
	backend.activeAcquisitions++
	if backend.activeAcquisitions > backend.maximumAcquisitions {
		backend.maximumAcquisitions = backend.activeAcquisitions
	}
	configuredErr := backend.acquisitionErr
	started := backend.acquisitionStarted
	release := backend.acquisitionRelease
	backend.mu.Unlock()

	options.Emit(acquisition.EventFetch, sourceSpec, sourceID, "", nil)
	if started != nil {
		select {
		case started <- sourceID:
		case <-ctx.Done():
			backend.finishAcquisition(group, ctx.Err())
			return ctx.Err()
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			backend.finishAcquisition(group, ctx.Err())
			return ctx.Err()
		}
	}

	backend.finishAcquisition(group, configuredErr)
	return configuredErr
}

func (backend *repositoryBatchBackend) finishAcquisition(group acquisition.RepositorySnapshotGroupID, err error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.activeAcquisitions--
	if err == nil {
		backend.completed[group] = struct{}{}
	}
}

func (backend *repositoryBatchBackend) setAcquisitionError(err error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.acquisitionErr = err
}

func (backend *repositoryBatchBackend) acquisitionCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return len(backend.acquisitionSources)
}

func (backend *repositoryBatchBackend) acquisitionSourceIDs() []artifact.SourceID {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return append([]artifact.SourceID(nil), backend.acquisitionSources...)
}

func (backend *repositoryBatchBackend) resolveCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.resolveRequests
}

func (backend *repositoryBatchBackend) listCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.listRequests
}

func (backend *repositoryBatchBackend) maxActiveAcquisitions() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.maximumAcquisitions
}

type repositoryBatchEventRecorder struct {
	mu     sync.Mutex
	events []acquisition.Event
}

func (recorder *repositoryBatchEventRecorder) record(event acquisition.Event) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.events = append(recorder.events, event)
}

func (recorder *repositoryBatchEventRecorder) requestIDs(kind acquisition.EventKind) []acquisition.RequestID {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	ids := make([]acquisition.RequestID, 0)
	for _, event := range recorder.events {
		if event.Kind() == kind {
			ids = append(ids, event.Request().ID())
		}
	}
	return ids
}

func repositoryBatchRequests(
	t *testing.T,
	locator string,
	ref string,
	size int,
	ordinalOffset int,
) []acquisition.Request {
	t.Helper()
	requests := make([]acquisition.Request, size)
	for index := range size {
		ordinal := ordinalOffset + index
		request, err := acquisition.NewRequest(
			acquisition.RequestID(fmt.Sprintf("skill:%06d", ordinal)),
			ordinal,
			acquisition.OperationResolve,
			mustGitSource(t, locator, fmt.Sprintf("skills/skill-%06d", ordinal), ref),
		)
		if err != nil {
			t.Fatalf("NewRequest returned error: %v", err)
		}
		requests[index] = request
	}
	return requests
}

func waitForRepositoryStart(t *testing.T, started <-chan artifact.SourceID) artifact.SourceID {
	t.Helper()
	select {
	case requestID := <-started:
		return requestID
	case <-time.After(5 * time.Second):
		t.Fatal("repository acquisition did not reach deterministic barrier")
		return ""
	}
}

func equalSourceIDSets(left []artifact.SourceID, right []artifact.SourceID) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[artifact.SourceID]int, len(left))
	for _, sourceID := range left {
		counts[sourceID]++
	}
	for _, sourceID := range right {
		counts[sourceID]--
		if counts[sourceID] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func equalRequestIDSets(left []acquisition.RequestID, right []acquisition.RequestID) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[acquisition.RequestID]int, len(left))
	for _, requestID := range left {
		counts[requestID]++
	}
	for _, requestID := range right {
		counts[requestID]--
		if counts[requestID] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}
