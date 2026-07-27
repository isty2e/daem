package live

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/topology"
)

// ManagedPathRequest is one typed observation request derived from a locked
// realization or managed-state authority record.
type ManagedPathRequest struct {
	Subject     topology.SubjectID
	Destination output.Destination
	ContentKind realization.PathProjectionContentKind
}

// ManagedPathEvidence observes every distinct requested subject/address pair.
func ManagedPathEvidence(
	ctx context.Context,
	resolver DestinationResolver,
	requests []ManagedPathRequest,
) ([]observe.ManagedPathEvidence, error) {
	if resolver == nil {
		return nil, fmt.Errorf("managed path destination resolver is required")
	}
	type requestKey struct {
		subject     topology.SubjectID
		destination output.Destination
	}
	unique := make(map[requestKey]ManagedPathRequest, len(requests))
	for index, request := range requests {
		if err := request.Subject.Validate(); err != nil {
			return nil, fmt.Errorf("managed path request[%d] subject: %w", index, err)
		}
		if request.Subject.Kind() != topology.SubjectProjection {
			return nil, fmt.Errorf("managed path request[%d] requires projection subject", index)
		}
		if err := request.Destination.Validate(); err != nil {
			return nil, fmt.Errorf("managed path request[%d] destination: %w", index, err)
		}
		if request.ContentKind != realization.PathProjectionFile &&
			request.ContentKind != realization.PathProjectionDirectory {
			return nil, fmt.Errorf("managed path request[%d] content kind %q is unsupported", index, request.ContentKind)
		}
		key := requestKey{subject: request.Subject, destination: request.Destination}
		if existing, duplicate := unique[key]; duplicate && existing.ContentKind != request.ContentKind {
			return nil, fmt.Errorf("managed path request[%d] conflicts with duplicate subject/address", index)
		}
		unique[key] = request
	}

	keys := make([]requestKey, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left int, right int) bool {
		if keys[left].subject != keys[right].subject {
			return topology.CompareSubjectID(keys[left].subject, keys[right].subject) < 0
		}
		return keys[left].destination.String() < keys[right].destination.String()
	})

	result := make([]observe.ManagedPathEvidence, 0, len(keys))
	for _, key := range keys {
		request := unique[key]
		evidence, err := observeManagedPathEvidence(
			ctx,
			resolver,
			request,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, evidence)
	}
	return result, nil
}

func observeManagedPathEvidence(
	ctx context.Context,
	resolver DestinationResolver,
	request ManagedPathRequest,
) (observe.ManagedPathEvidence, error) {
	destination := request.Destination
	hostPath, err := resolver(destination)
	if err != nil {
		return observe.ManagedPathEvidence{}, err
	}
	info, err := os.Lstat(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return observe.NewManagedPathEvidence(request.Subject, destination, false, "", 0)
		}
		return observe.ManagedPathEvidence{}, fmt.Errorf("stat destination %q: %w", destination, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return observe.ManagedPathEvidence{}, fmt.Errorf("observe destination %q: symlinks are not supported yet", destination)
	}
	if request.ContentKind == realization.PathProjectionFile && !info.Mode().IsRegular() {
		return observe.ManagedPathEvidence{}, fmt.Errorf("observe destination %q: expected regular file", destination)
	}
	if request.ContentKind == realization.PathProjectionDirectory && !info.IsDir() {
		return observe.ManagedPathEvidence{}, fmt.Errorf("observe destination %q: expected directory", destination)
	}
	contentHash, artifactKind, err := access.HashPath(ctx, hostPath)
	if err != nil {
		return observe.ManagedPathEvidence{}, fmt.Errorf("observe destination %q: %w", destination, err)
	}
	if !managedPathArtifactKindMatches(request.ContentKind, artifactKind) {
		return observe.ManagedPathEvidence{}, fmt.Errorf(
			"observe destination %q: expected %s",
			destination,
			managedPathKindDescription(request.ContentKind),
		)
	}
	fileMode := os.FileMode(0)
	if artifactKind == artifact.ArtifactKindFile {
		fileMode = info.Mode().Perm()
	}
	return observe.NewManagedPathEvidence(
		request.Subject,
		destination,
		true,
		contentHash,
		fileMode,
	)
}

func managedPathArtifactKindMatches(
	contentKind realization.PathProjectionContentKind,
	artifactKind artifact.ArtifactKind,
) bool {
	switch contentKind {
	case realization.PathProjectionFile:
		return artifactKind == artifact.ArtifactKindFile
	case realization.PathProjectionDirectory:
		return artifactKind == artifact.ArtifactKindDirectory
	default:
		return false
	}
}

func managedPathKindDescription(kind realization.PathProjectionContentKind) string {
	if kind == realization.PathProjectionDirectory {
		return "directory"
	}
	return "regular file"
}
