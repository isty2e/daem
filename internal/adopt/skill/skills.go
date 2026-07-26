package skill

import (
	"context"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	importSkillSkipMissingRoot             = "missing"
	importSkillSkipNotDirectory            = "skill_not_directory"
	importSkillSkipMissingSkillMD          = "missing_skill_md"
	importSkillSkipInvalidName             = "invalid_skill_name"
	importSkillSkipDuplicateName           = "duplicate_skill_name"
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

type DestinationClaims map[DestinationClaim]string

func NewDestinationClaims() DestinationClaims {
	return make(DestinationClaims)
}

func Candidates(
	ctx context.Context,
	sourceDirectory adopt.SourceDirectory,
	target targetpkg.Target,
	scope targetpkg.Scope,
	importedDestinations DestinationClaims,
) ([]adopt.Skill, []adopt.Scan, []adopt.Skipped, error) {
	locations := skillImportLocations(target, scope)
	skills := make([]adopt.Skill, 0)
	scans := make([]adopt.Scan, 0, len(locations))
	skipped := make([]adopt.Skipped, 0)

	for _, location := range locations {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		liveRoot, err := adopt.LocationPath(location.Path())
		if err != nil {
			return nil, nil, nil, err
		}
		if location.ImportPolicy() == profile.ImportPolicyClassify {
			if exists, err := adopt.PathExists(liveRoot); err != nil {
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

		rootSkills, rootScan, rootSkipped, err := importSkillsFromRoot(ctx, sourceDirectory, target, scope, liveRoot, importedDestinations)
		if err != nil {
			return nil, nil, nil, err
		}
		skills = append(skills, rootSkills...)
		scans = append(scans, rootScan)
		skipped = append(skipped, rootSkipped...)
	}
	for _, location := range adopt.RuntimeLocations(target, entity.KindSkill, scope) {
		liveRoot, err := adopt.LocationPath(location.Path())
		if err != nil {
			return nil, nil, nil, err
		}
		if exists, err := adopt.PathExists(liveRoot); err != nil {
			return nil, nil, nil, fmt.Errorf("inspect skill runtime root %q: %w", liveRoot, err)
		} else if exists {
			scans = append(scans, newSkillRootScan(target, scope, liveRoot, importSkillSkipSuppliedRoot, 0, 0, 0))
			skipped = append(skipped, adopt.Skipped{LivePath: liveRoot, Reason: importSkillSkipSuppliedRoot})
		}
	}
	return skills, scans, skipped, nil
}

func skillImportLocations(target targetpkg.Target, scope targetpkg.Scope) []profile.DiscoveryLocation {
	return adopt.DiscoveryLocations(target, entity.KindSkill, scope)
}
