package skill

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

type sourceIdentityObserver func(context.Context, string) (artifact.ContentHash, error)

// SourceIdentityCache owns successful skill-directory identity observations
// for one candidate-collection pass. It must not outlive one BuildPlan call.
type SourceIdentityCache struct {
	contentHashes map[string]artifact.ContentHash
	observe       sourceIdentityObserver
}

// NewSourceIdentityCache constructs an empty operation-local identity cache.
func NewSourceIdentityCache() *SourceIdentityCache {
	return newSourceIdentityCache(observeSkillDirectoryIdentity)
}

func newSourceIdentityCache(observer sourceIdentityObserver) *SourceIdentityCache {
	return &SourceIdentityCache{
		contentHashes: make(map[string]artifact.ContentHash),
		observe:       observer,
	}
}

// ContentHash returns the exact content hash of one fully resolved skill route.
// The cache is operation-local and intentionally not safe for concurrent use.
func (cache *SourceIdentityCache) ContentHash(
	ctx context.Context,
	readPath string,
) (artifact.ContentHash, error) {
	if cache == nil || cache.observe == nil || cache.contentHashes == nil {
		return "", fmt.Errorf("skill source identity cache is required")
	}
	if ctx == nil {
		return "", fmt.Errorf("skill source identity context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(readPath) || filepath.Clean(readPath) != readPath {
		return "", fmt.Errorf("skill read path %q must be canonical and absolute", readPath)
	}
	if contentHash, exists := cache.contentHashes[readPath]; exists {
		return contentHash, nil
	}
	contentHash, err := cache.observe(ctx, readPath)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := contentHash.Validate(); err != nil {
		return "", fmt.Errorf("skill source identity for %q: %w", readPath, err)
	}
	cache.contentHashes[readPath] = contentHash
	return contentHash, nil
}

func observeSkillDirectoryIdentity(
	ctx context.Context,
	readPath string,
) (artifact.ContentHash, error) {
	view, err := access.OpenNoFollowView(readPath)
	if err != nil {
		return "", fmt.Errorf("open skill tree %q: %w", readPath, err)
	}
	if view.Kind() != artifact.ArtifactKindDirectory {
		return "", fmt.Errorf("skill tree %q is not a directory", readPath)
	}
	contentHash, err := view.HashDirectoryRequiringRootFile(ctx, "SKILL.md")
	if err != nil {
		return "", fmt.Errorf("hash skill tree %q: %w", readPath, err)
	}
	return contentHash, nil
}
