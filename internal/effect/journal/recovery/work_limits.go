package recovery

const (
	// MaximumRemovalIntents bounds the complete operation-scoped removal authority.
	MaximumRemovalIntents = 4_096
	// MaximumArtifactTreeDepth bounds recursive observation, restore and cleanup.
	MaximumArtifactTreeDepth = 64
	// MaximumArtifactTreeEntries is the independent per-state descendant limit.
	MaximumArtifactTreeEntries = 100_000
	// MaximumArtifactTreeBytes is the independent per-state regular-file byte limit.
	MaximumArtifactTreeBytes int64 = 4 << 30
	// MaximumRecoveryBackupFileBytes bounds one regular-file recovery backup.
	MaximumRecoveryBackupFileBytes int64 = 128 << 20
)
