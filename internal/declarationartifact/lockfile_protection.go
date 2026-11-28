package declarationartifact

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
)

var ErrLockfileInputOverlap = errors.New("destination overlaps the selected manifest or a local source")

// RequireDisjointLockfile checks a bound physical write entry against the
// selected manifest and sources declared before or after the prospective edit.
// It observes names, not source contents, and grants no write authority.
func RequireDisjointLockfile(ctx context.Context, destination string, workspace paths.Paths, environments ...desired.Environment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	depth := physicalwork.MaximumPhysicalPathDepth
	budget := physicalwork.NewPathBudget()
	lock, err := mutation.ObserveDirectoryEntryAuthorityBounded(destination, depth, budget)
	if err != nil {
		return err
	}
	if reference, present := lock.Reference(); present {
		destination = reference.Locator()
	}
	observe := mutation.NewFutureReferentAuthorityObserverBounded(depth, budget)
	resolver, err := localfs.NewResolver(workspace.ManifestRoot)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{})
	checkArtifact := func(path string) error {
		if _, checked := seen[path]; checked {
			return nil
		}
		seen[path] = struct{}{}
		physical, observed, err := observe(path)
		if err != nil {
			if hasNoReferent(path) {
				return nil
			}
			return err
		}
		if !lock.MayOverlap(observed) {
			return nil
		}
		if _, err := os.Lstat(physical); errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		return ErrLockfileInputOverlap
	}
	if err := checkArtifact(workspace.ManifestPath); err != nil {
		return err
	}
	for _, environment := range environments {
		for _, spec := range environment.EntityArtifactSources() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if spec.Kind() != source.SourceKindLocal {
				continue
			}
			path, err := resolver.LocalInputAuthorityPath(spec)
			if err != nil {
				return err
			}
			if err := checkArtifact(path); err != nil {
				return err
			}
		}
		for _, group := range environment.SkillSets() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if group.Source().Kind() != source.SourceKindLocal {
				continue
			}
			path, err := resolver.LocalInputAuthorityPath(group.Source())
			if err != nil {
				return err
			}
			_, observed, err := observe(path)
			if err != nil {
				if hasNoReferent(path) {
					continue
				}
				return err
			}
			root, present := observed.Reference()
			if !present {
				continue
			}
			if err := checkSelectedGroupChild(root.Locator(), destination, group); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func hasNoReferent(path string) bool {
	// A declaration being removed need not point to a usable source. Do not
	// mistake a missing entry, a non-directory ancestor, or an alias loop for
	// a live referent. Other observation failures still prevent publication.
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP)
}

func checkSelectedGroupChild(root, destination string, group skill.SkillSet) error {
	relative, err := filepath.Rel(root, filepath.Dir(destination))
	if err != nil {
		return fmt.Errorf("locate lockfile relative to skill group root: %w", err)
	}
	child, _, _ := strings.Cut(relative, string(filepath.Separator))
	if child == "." || child == ".." {
		return nil
	}

	// The root is only listed. Only real direct child directories can become
	// sources, and the lock can be inside at most one of them for this group.
	info, err := os.Lstat(filepath.Join(root, child))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	selected, err := group.Selects(child)
	if errors.Is(err, skill.ErrInvalidSkillSetChildName) {
		return nil
	}
	if err != nil {
		return err
	}
	if selected {
		return ErrLockfileInputOverlap
	}
	return nil
}
