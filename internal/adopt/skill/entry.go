package skill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func importSkillsFromRoot(
	ctx context.Context,
	sourceDirectory adopt.SourceDirectory,
	target targetpkg.Target,
	scope targetpkg.Scope,
	installTo string,
	liveRoot string,
	importedDestinations DestinationClaims,
	sourceIdentities *SourceIdentityCache,
) ([]adopt.Skill, adopt.Scan, []adopt.Skipped, error) {
	entries, err := os.ReadDir(liveRoot)
	if err != nil {
		return nil, adopt.Scan{}, nil, fmt.Errorf("read skill root %q: %w", liveRoot, err)
	}

	skills := make([]adopt.Skill, 0, len(entries))
	skipped := make([]adopt.Skipped, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, adopt.Scan{}, nil, err
		}
		livePath := filepath.Join(liveRoot, entry.Name())
		skill, skip, err := importSkillFromEntry(
			ctx,
			sourceDirectory,
			target,
			scope,
			installTo,
			livePath,
			entry.Name(),
			importedDestinations,
			sourceIdentities,
		)
		if err != nil {
			return nil, adopt.Scan{}, nil, err
		}
		if skip.Reason != "" {
			skipped = append(skipped, skip)
			continue
		}
		skills = append(skills, skill)
	}

	scanStatus := importSkillRootScanScanned
	if len(entries) == 0 {
		scanStatus = importSkillRootScanEmpty
	} else if len(skills) == 0 {
		scanStatus = importSkillRootScanNoImportableEntries
	}
	scan := newSkillRootScan(target, scope, liveRoot, scanStatus, len(entries), len(skills), len(skipped))

	return skills, scan, skipped, nil
}

func newSkillRootScan(
	target targetpkg.Target,
	scope targetpkg.Scope,
	liveRoot string,
	status string,
	entries int,
	imported int,
	skipped int,
) adopt.Scan {
	return adopt.Scan{
		ResourceKind: "skill",
		ResourceName: importSkillRootScanResourceName,
		Target:       target,
		Scope:        scope,
		LivePath:     liveRoot,
		Status:       status,
		Entries:      entries,
		Imported:     imported,
		Skipped:      skipped,
		Evidence:     adopt.DirectoryListingScanEvidence(),
	}
}

func importSkillFromEntry(
	ctx context.Context,
	sourceDirectory adopt.SourceDirectory,
	target targetpkg.Target,
	scope targetpkg.Scope,
	installTo string,
	livePath string,
	name string,
	importedDestinations DestinationClaims,
	sourceIdentities *SourceIdentityCache,
) (adopt.Skill, adopt.Skipped, error) {
	cleanName, err := cleanImportSkillName(name)
	if err != nil {
		return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: importSkillSkipInvalidName}, nil
	}
	if suppliedReason := suppliedSkillSkipReason(livePath, cleanName); suppliedReason != "" {
		return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: suppliedReason}, nil
	}

	readPath, err := resolvedImportSkillReadPath(livePath)
	if err != nil {
		return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: importSkillSkipNotDirectory}, nil
	}
	if suppliedReason := suppliedSkillSkipReason(readPath, cleanName); suppliedReason != "" {
		return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: suppliedReason}, nil
	}
	info, err := os.Stat(readPath)
	if err != nil {
		return adopt.Skill{}, adopt.Skipped{}, fmt.Errorf("stat skill %q: %w", livePath, err)
	}
	if !info.IsDir() {
		return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: importSkillSkipNotDirectory}, nil
	}
	destination := DestinationClaim{
		Target:      target,
		Scope:       scope,
		InstallName: cleanName,
	}
	if _, exists := importedDestinations[destination]; exists {
		return adopt.Skill{}, adopt.Skipped{
			LivePath: livePath,
			Reason:   importSkillSkipDuplicateName,
		}, nil
	}
	if nestedSymlink, ok, err := firstNestedSymlink(readPath); err != nil {
		return adopt.Skill{}, adopt.Skipped{}, fmt.Errorf("inspect skill tree %q: %w", livePath, err)
	} else if ok {
		return adopt.Skill{}, adopt.Skipped{LivePath: nestedSymlink, Reason: importSkillSkipNestedSymlink}, nil
	}

	contentHash, err := sourceIdentities.ContentHash(ctx, readPath)
	if err != nil {
		if errors.Is(err, access.ErrRequiredRootRegularFile) {
			return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: importSkillSkipMissingSkillMD}, nil
		}
		return adopt.Skill{}, adopt.Skipped{}, err
	}

	sourcePath, err := importSkillSourcePath(sourceDirectory, cleanName, contentHash)
	if err != nil {
		return adopt.Skill{}, adopt.Skipped{}, err
	}
	importedDestinations[destination] = livePath

	placements := make(map[targetpkg.Target]string)
	if installTo != "" {
		placements[target] = installTo
	}
	return adopt.Skill{
		ResourceName: cleanName,
		InstallName:  cleanName,
		Target:       target,
		Targets:      []targetpkg.Target{target},
		Placements:   placements,
		Scope:        scope,
		SourceRoutes: []adopt.SkillSourceRoute{{
			Target: target, LivePath: livePath, ReadPath: readPath,
		}},
		SourcePath:  sourcePath,
		ContentHash: contentHash,
	}, adopt.Skipped{}, nil
}
