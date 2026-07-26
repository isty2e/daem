package recovery

import (
	"fmt"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

type pathEvidenceKey struct {
	path        string
	contentPath string
}

func pathEvidenceIndex(values []PathEvidence) (map[pathEvidenceKey]PathEvidence, error) {
	observations := make(map[pathEvidenceKey]PathEvidence, len(values))
	for _, value := range values {
		if value.Path == "" {
			return nil, fmt.Errorf("path observation path is required")
		}
		key := pathEvidenceKey{path: value.Path, contentPath: value.ContentPath}
		if _, exists := observations[key]; exists {
			return nil, fmt.Errorf("duplicate path observation for %q content_path %q", value.Path, value.ContentPath)
		}
		observations[key] = value
	}
	return observations, nil
}

func backupEvidenceIndex(values []BackupEvidence) (map[string]BackupEvidence, error) {
	observations := make(map[string]BackupEvidence, len(values))
	for _, value := range values {
		if value.BackupPath == "" {
			return nil, fmt.Errorf("backup observation path is required")
		}
		if _, exists := observations[value.BackupPath]; exists {
			return nil, fmt.Errorf("duplicate backup observation for %q", value.BackupPath)
		}
		observations[value.BackupPath] = value
	}
	return observations, nil
}

func backupMismatch(before BeforePathState, backups map[string]BackupEvidence) string {
	backup, ok := backups[before.BackupPath]
	if !ok {
		return "backup observation is required"
	}
	if backup.Error != "" {
		return backup.Error
	}
	if !backup.Exists {
		return "backup file is missing"
	}
	if backup.Kind != before.Kind {
		return fmt.Sprintf("backup kind %q does not match before kind %q", backup.Kind, before.Kind)
	}
	if backup.ContentHash != before.ContentHash {
		return fmt.Sprintf("backup hash %q does not match before hash %q", backup.ContentHash, before.ContentHash)
	}
	return ""
}

func pathMatchesBefore(before BeforePathState, observation PathEvidence) bool {
	if observation.ContentPath != "" {
		if observation.PathExisted != before.PathExisted {
			return false
		}
		if before.PathExisted && !permissionModesEqual(before.PathMode, observation.PathMode) {
			return false
		}
	}
	if !before.Existed {
		return !observation.Exists
	}
	if !observation.Exists || observation.Kind != before.Kind {
		return false
	}
	switch before.Kind {
	case PathKindFile:
		return observation.ContentHash == before.ContentHash && permissionModesEqual(before.PathMode, observation.PathMode)
	case PathKindDirectory:
		return observation.ContentHash == before.ContentHash
	case PathKindSymlink:
		return observation.LinkTarget == before.LinkTarget
	default:
		return false
	}
}

func pathMatchesExpected(expected ExpectedPathState, observation PathEvidence) bool {
	if observation.ContentPath != "" {
		if observation.PathExisted != expected.PathExisted {
			return false
		}
		if expected.PathExisted && !permissionModesEqual(expected.PathMode, observation.PathMode) {
			return false
		}
	}
	if !expected.Existed {
		return !observation.Exists
	}
	if !observation.Exists || observation.Kind != expected.Kind {
		return false
	}
	switch expected.Kind {
	case PathKindFile:
		return observation.ContentHash == expected.ContentHash && permissionModesEqual(expected.PathMode, observation.PathMode)
	case PathKindDirectory:
		return observation.ContentHash == expected.ContentHash
	case PathKindSymlink:
		return observation.LinkTarget == expected.LinkTarget
	default:
		return false
	}
}

func permissionModesEqual(left *PermissionMode, right *PermissionMode) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func isContentBackedPathKind(kind string) bool {
	return kind == PathKindFile || kind == PathKindDirectory
}

func classifyClaimTransitions(
	transitions []ownershipmutation.ClaimTransition,
	registry ownership.Registry,
) (before bool, prepared bool, after bool, rollbackEligible bool, finalizeEligible bool) {
	before, prepared, after, rollbackEligible, finalizeEligible = true, true, true, true, true
	for _, transition := range transitions {
		actual := ownership.NoClaim()
		if claim, present := registry.Conflict(transition.Address()); present {
			actual, _ = ownership.PresentClaim(claim)
		}
		matchesBefore := actual.Equal(transition.Before())
		matchesPrepared := actual.Equal(transition.Prepared())
		matchesAfter := actual.Equal(transition.After())
		before = before && matchesBefore
		prepared = prepared && matchesPrepared
		after = after && matchesAfter
		rollbackEligible = rollbackEligible && (matchesBefore || matchesPrepared)
		finalizeEligible = finalizeEligible && (matchesPrepared || matchesAfter)
	}
	return before, prepared, after, rollbackEligible, finalizeEligible
}
