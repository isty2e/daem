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
		return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: adopt.SkipReason(suppliedReason)}, nil
	}

	readPath, err := resolvedImportSkillReadPath(livePath)
	if err != nil {
		return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: importSkillSkipNotDirectory}, nil
	}
	if suppliedReason := suppliedSkillSkipReason(readPath, cleanName); suppliedReason != "" {
		return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: adopt.SkipReason(suppliedReason)}, nil
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
	contentHash, err := sourceIdentities.ContentHash(ctx, readPath)
	if err != nil {
		if errors.Is(err, access.ErrRequiredRootRegularFile) {
			return adopt.Skill{}, adopt.Skipped{LivePath: livePath, Reason: importSkillSkipMissingSkillMD}, nil
		}
		if relativePath, ok := access.UnsupportedSymlinkPath(err); ok {
			skipPath := readPath
			if relativePath != "" && relativePath != "." {
				skipPath = filepath.Join(readPath, filepath.FromSlash(relativePath))
			}
			return adopt.Skill{}, adopt.Skipped{LivePath: skipPath, Reason: importSkillSkipNestedSymlink}, nil
		}
		return adopt.Skill{}, adopt.Skipped{}, err
	}
	if claimed, exists := importedDestinations.source(destination); exists {
		reason := adopt.SkipReason(importSkillSkipDuplicateName)
		detail := "duplicates=" + claimed.livePath
		if contentHash != claimed.contentHash {
			reason = importSkillSkipConflictingName
			detail = "conflicts_with=" + claimed.livePath
		}
		return adopt.Skill{}, adopt.Skipped{
			LivePath: livePath,
			Reason:   reason,
			Detail:   detail,
		}, nil
	}

	sourcePath, err := importSkillSourcePath(sourceDirectory, cleanName, contentHash)
	if err != nil {
		return adopt.Skill{}, adopt.Skipped{}, err
	}
	importedDestinations.add(destination, destinationClaimSource{
		livePath:    livePath,
		contentHash: contentHash,
	})

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
