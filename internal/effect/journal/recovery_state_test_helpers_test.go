package journal

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/target"
)

func managedPathRecoveryEntry() recoveryEntry {
	return recoveryEntry{
		Subject: persistedSubjectRef{
			Kind:      "projection",
			Namespace: "skill.project.agents",
			Name:      "skill:oracle",
		},
		Targets:     []string{"codex"},
		Scope:       "project",
		Path:        ".agents/skills/oracle",
		ContentKind: "directory",
		Before: persistedBeforePathState(recovery.BeforePathState{
			Existed:     true,
			Kind:        recovery.PathKindDirectory,
			ContentHash: testBeforeHash,
			BackupPath:  "backup-0000",
		}),
		ExpectedAfter: persistedExpectedPathState(recovery.ExpectedPathState{
			Existed:     true,
			Kind:        recovery.PathKindDirectory,
			ContentHash: testAfterHash,
		}),
		StateBefore: recoveryManagedMembership{
			Managed:     true,
			ContentHash: testBeforeHash,
		},
		StateExpectedAfter: recoveryManagedMembership{
			Managed:     true,
			ContentHash: testAfterHash,
		},
	}
}

func testAppliedState(selectedTarget target.Target, contentHash string) durable.ManagedPathState {
	destination := "AGENTS.md"
	if selectedTarget == target.TargetClaudeCode {
		destination = "CLAUDE.md"
	}
	entry := recoveryEntryFor("shared", destination, contentHash, contentHash, "backup-0000")
	return resourceState(entry, contentHash)
}
