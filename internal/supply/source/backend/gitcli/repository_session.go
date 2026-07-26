package gitcli

import (
	"context"
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

type repositorySnapshotKey struct {
	locator      string
	canonicalRef string
}

type repositorySnapshotSession struct {
	mu        sync.Mutex
	snapshots map[repositorySnapshotKey]repositoryResolution
	calls     map[repositorySnapshotKey]*repositorySnapshotCall
}

type repositorySnapshotCall struct {
	done       chan struct{}
	resolution repositoryResolution
	err        error
}

func newRepositorySnapshotSession() *repositorySnapshotSession {
	return &repositorySnapshotSession{
		snapshots: make(map[repositorySnapshotKey]repositoryResolution),
		calls:     make(map[repositorySnapshotKey]*repositorySnapshotCall),
	}
}

func repositoryKey(gitSource source.GitSource) repositorySnapshotKey {
	return repositorySnapshotKey{
		locator:      gitSource.Locator().String(),
		canonicalRef: gitSource.Ref().Canonical(),
	}
}

// RepositorySnapshotGroup returns the operation-local sharing identity for a
// Git source.
func (resolver Resolver) RepositorySnapshotGroup(
	sourceSpec source.Source,
) (acquisition.RepositorySnapshotGroupID, bool, error) {
	gitSource, ok := sourceSpec.Git()
	if !ok {
		return acquisition.RepositorySnapshotGroupID{}, false, nil
	}
	group, err := acquisition.NewRepositorySnapshotGroupID(gitSource)
	if err != nil {
		return acquisition.RepositorySnapshotGroupID{}, false, err
	}
	return group, true, nil
}

func immutableRepositoryKey(locator string, commit string) repositorySnapshotKey {
	return repositorySnapshotKey{
		locator:      locator,
		canonicalRef: "commit:" + commit,
	}
}

func (session *repositorySnapshotSession) resolve(
	ctx context.Context,
	key repositorySnapshotKey,
	resolve func() (repositoryResolution, error),
) (repositoryResolution, error) {
	if session == nil {
		return repositoryResolution{}, fmt.Errorf("git repository snapshot session is required")
	}

	session.mu.Lock()
	if snapshot, ok := session.snapshots[key]; ok {
		session.mu.Unlock()
		return snapshot, nil
	}
	if existing, ok := session.calls[key]; ok {
		session.mu.Unlock()
		select {
		case <-existing.done:
			return existing.resolution, existing.err
		default:
		}
		select {
		case <-existing.done:
			return existing.resolution, existing.err
		case <-ctx.Done():
			select {
			case <-existing.done:
				return existing.resolution, existing.err
			default:
			}
			return repositoryResolution{}, fmt.Errorf(
				"wait for git repository snapshot %q at %q: %w",
				key.canonicalRef,
				key.locator,
				ctx.Err(),
			)
		}
	}

	current := &repositorySnapshotCall{done: make(chan struct{})}
	session.calls[key] = current
	session.mu.Unlock()

	current.resolution, current.err = resolve()

	session.mu.Lock()
	if current.err == nil {
		session.snapshots[key] = current.resolution
		session.snapshots[immutableRepositoryKey(key.locator, current.resolution.commit)] = current.resolution
	}
	delete(session.calls, key)
	close(current.done)
	session.mu.Unlock()

	return current.resolution, current.err
}

// PrepareRepositorySnapshot resolves the repository-level Git fact without
// listing, exporting, or hashing a path-specific artifact.
func (resolver Resolver) PrepareRepositorySnapshot(
	ctx context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) error {
	if ctx == nil {
		return fmt.Errorf("git repository snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if resolver.session == nil {
		return fmt.Errorf("git repository snapshot preparation requires a resolution session")
	}

	gitSource, ok := sourceSpec.Git()
	if !ok {
		return fmt.Errorf("git resolver only supports git sources, got %q", sourceSpec.Kind())
	}
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return err
	}

	_, _, err = resolver.resolveRepositoryCommit(ctx, gitSource, sourceSpec, sourceID, options)
	return err
}
