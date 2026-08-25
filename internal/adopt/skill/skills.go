package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	importSkillSkipMissingRoot             = "missing"
	importSkillSkipNotDirectory            = "skill_not_directory"
	importSkillSkipMissingSkillMD          = "missing_skill_md"
	importSkillSkipInvalidName             = "invalid_skill_name"
	importSkillSkipDuplicateName           = "duplicate_skill_name"
	importSkillSkipConflictingName         = "conflicting_skill_name"
	importSkillSkipSuppliedRoot            = "supplied_skill_root"
	importSkillSkipSuppliedEntry           = "supplied_skill_entry"
	importSkillSkipSuppliedPluginCache     = "supplied_plugin_cache_skill"
	importSkillSkipNestedSymlink           = "nested_symlink"
	importSkillManifestSourceDirectoryName = "skills"
	importSkillGroupSourceDirectoryName    = "skill-groups"
	importSkillRootScanResourceName        = "skill-root"
	importSkillRootScanEmpty               = "empty"
	importSkillRootScanScanned             = "scanned"
	importSkillRootScanNoImportableEntries = "no_importable_entries"
)

type DestinationClaim struct {
	Target      targetpkg.Target
	Scope       targetpkg.Scope
	InstallName string
}

type destinationClaimSource struct {
	livePath    string
	contentHash artifact.ContentHash
}

type DestinationClaims struct {
	sources map[DestinationClaim]destinationClaimSource
}

func NewDestinationClaims() DestinationClaims {
	return DestinationClaims{sources: make(map[DestinationClaim]destinationClaimSource)}
}

func (claims DestinationClaims) source(destination DestinationClaim) (destinationClaimSource, bool) {
	source, exists := claims.sources[destination]
	return source, exists
}

func (claims DestinationClaims) add(destination DestinationClaim, source destinationClaimSource) {
	claims.sources[destination] = source
}

func Candidates(
	ctx context.Context,
	sourceDirectory adopt.SourceDirectory,
	target targetpkg.Target,
	scope targetpkg.Scope,
	importedDestinations DestinationClaims,
	sourceIdentities *SourceIdentityCache,
) ([]adopt.Skill, []adopt.Scan, []adopt.Skipped, error) {
	if sourceIdentities == nil {
		return nil, nil, nil, fmt.Errorf("skill source identity cache is required")
	}
	locations := profile.Profile(target).DiscoveryLocations(entity.KindSkill, scope)
	skills := make([]adopt.Skill, 0)
	scans := make([]adopt.Scan, 0, len(locations))
	skipped := make([]adopt.Skipped, 0)

	for _, location := range locations {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		liveRoot, err := skillLocationPath(location.Path())
		if err != nil {
			return nil, nil, nil, err
		}
		if location.ImportPolicy() == profile.ImportPolicyClassify {
			if exists, err := skillPathExists(liveRoot); err != nil {
				return nil, nil, nil, fmt.Errorf("inspect skill root %q: %w", liveRoot, err)
			} else if exists {
				scans = append(scans, newSkillRootScan(target, scope, liveRoot, importSkillSkipSuppliedRoot, 0, 0, 0))
				skipped = append(skipped, adopt.Skipped{LivePath: liveRoot, Reason: importSkillSkipSuppliedRoot})
			}
			continue
		}
		rootInfo, err := os.Stat(liveRoot)
		if os.IsNotExist(err) {
			scans = append(scans, newSkillRootScan(target, scope, liveRoot, importSkillSkipMissingRoot, 0, 0, 0))
			skipped = append(skipped, adopt.Skipped{LivePath: liveRoot, Reason: importSkillSkipMissingRoot})
			continue
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read skill root %q: %w", liveRoot, err)
		}
		if !rootInfo.IsDir() {
			scans = append(scans, newSkillRootScan(target, scope, liveRoot, importSkillSkipNotDirectory, 0, 0, 0))
			skipped = append(skipped, adopt.Skipped{LivePath: liveRoot, Reason: importSkillSkipNotDirectory})
			continue
		}

		installTo := ""
		if admission, admitted := profile.Profile(target).PlacementAdmissionAt(
			entity.KindSkill,
			scope,
			location.Path(),
		); admitted && !admission.Default() {
			installTo = location.Path()
		}
		rootSkills, rootScan, rootSkipped, err := importSkillsFromRoot(
			ctx,
			sourceDirectory,
			target,
			scope,
			installTo,
			liveRoot,
			importedDestinations,
			sourceIdentities,
		)
		if err != nil {
			return nil, nil, nil, err
		}
		skills = append(skills, rootSkills...)
		scans = append(scans, rootScan)
		skipped = append(skipped, rootSkipped...)
	}
	for _, location := range profile.Profile(target).RuntimeLocations(entity.KindSkill, scope) {
		liveRoot, err := skillLocationPath(location.Path())
		if err != nil {
			return nil, nil, nil, err
		}
		if exists, err := skillPathExists(liveRoot); err != nil {
			return nil, nil, nil, fmt.Errorf("inspect skill runtime root %q: %w", liveRoot, err)
		} else if exists {
			scans = append(scans, newSkillRootScan(target, scope, liveRoot, importSkillSkipSuppliedRoot, 0, 0, 0))
			skipped = append(skipped, adopt.Skipped{LivePath: liveRoot, Reason: importSkillSkipSuppliedRoot})
		}
	}
	return skills, scans, skipped, nil
}

func skillLocationPath(locationPath string) (string, error) {
	if strings.HasPrefix(locationPath, "~/") {
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(homeDirectory, filepath.FromSlash(strings.TrimPrefix(locationPath, "~/"))), nil
	}
	if filepath.IsAbs(locationPath) {
		return filepath.Clean(locationPath), nil
	}
	return filepath.FromSlash(locationPath), nil
}

func skillPathExists(livePath string) (bool, error) {
	_, err := os.Lstat(livePath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
