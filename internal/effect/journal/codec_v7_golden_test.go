package journal

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func marshalRecoveryJournal(journal recoveryJournal, stateEncoder durable.SnapshotEncoder) ([]byte, error) {
	if stateEncoder == nil {
		return nil, fmt.Errorf("recovery journal state codec is required")
	}
	if err := validateRecoveryJournalEnvelope(journal); err != nil {
		return nil, err
	}
	before, after, err := encodeRecoveryJournalSnapshots(
		journal.StatefileBefore,
		journal.StatefileAfter,
		stateEncoder,
	)
	if err != nil {
		return nil, err
	}
	if err := validateRecoveryJournalRelationships(journal); err != nil {
		return nil, err
	}
	return encodeRecoveryJournal(journal, before, after)
}

func TestRecoveryJournalV7GoldenBytesAndFingerprint(t *testing.T) {
	t.Parallel()

	journal := defaultRecoveryJournal()
	content, err := marshalRecoveryJournal(journal, testStateCodec())
	if err != nil {
		t.Fatalf("marshalRecoveryJournal() error = %v", err)
	}

	wantContent := []byte(`{
  "version": 7,
  "operation_id": "20260625T000000.000000000Z-apply",
  "operation": "apply",
  "created_at": "2026-06-25T00:00:00Z",
  "project_root_provenance": {
    "physical_root": "/test/project",
    "object_fingerprint": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
    "mount_fingerprint": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
  },
  "entries": [
    {
      "subject": {
        "kind": "projection",
        "namespace": "instructions.project.agents",
        "name": "instructions:project"
      },
      "targets": [
        "codex"
      ],
      "scope": "project",
      "path": "AGENTS.md",
      "content_kind": "file",
      "before": {
        "existed": true,
        "path_mode": 384,
        "kind": "file",
        "content_hash": "sha256:4c1534302853792eb77cd68264a5262701aa64d27a6275e581d75842bbbe482d",
        "backup_path": "backups/AGENTS.md"
      },
      "expected_after": {
        "existed": true,
        "path_mode": 384,
        "kind": "file",
        "content_hash": "sha256:69489dee457a8786f7b7574b884155f2d84c3f4c7893aa3815837b565131d4a6"
      },
      "state_before": {
        "managed": true,
        "content_hash": "sha256:4c1534302853792eb77cd68264a5262701aa64d27a6275e581d75842bbbe482d"
      },
      "state_expected_after": {
        "managed": true,
        "content_hash": "sha256:69489dee457a8786f7b7574b884155f2d84c3f4c7893aa3815837b565131d4a6"
      }
    }
  ],
  "statefile_before": {
    "version": 7,
    "managed_paths": [
      {
        "subject": {
          "kind": "projection",
          "namespace": "instructions.project.agents",
          "name": "instructions:project"
        },
        "consumer_targets": [
          "codex"
        ],
        "scope": "project",
        "destination": "AGENTS.md",
        "content_hash": "sha256:4c1534302853792eb77cd68264a5262701aa64d27a6275e581d75842bbbe482d",
        "content_kind": "file",
        "permission_policy": "executable-class"
      }
    ],
    "managed_aggregate_contributions": [],
    "pending_carrier_installs": [],
    "pending_carrier_removals": [],
    "managed_carrier_claims": [],
    "delegate_attempts": [],
    "host_route_attempts": []
  },
  "statefile_after": {
    "version": 7,
    "managed_paths": [
      {
        "subject": {
          "kind": "projection",
          "namespace": "instructions.project.agents",
          "name": "instructions:project"
        },
        "consumer_targets": [
          "codex"
        ],
        "scope": "project",
        "destination": "AGENTS.md",
        "content_hash": "sha256:69489dee457a8786f7b7574b884155f2d84c3f4c7893aa3815837b565131d4a6",
        "content_kind": "file",
        "permission_policy": "executable-class"
      }
    ],
    "managed_aggregate_contributions": [],
    "pending_carrier_installs": [],
    "pending_carrier_removals": [],
    "managed_carrier_claims": [],
    "delegate_attempts": [],
    "host_route_attempts": []
  }
}`)
	if !bytes.Equal(content, wantContent) {
		t.Fatalf("journal bytes changed:\n%s", content)
	}
	if len(content) == 0 || content[len(content)-1] == '\n' {
		t.Fatalf("journal bytes must be non-empty and omit a trailing newline")
	}

	fingerprint, err := recoveryJournalAuthorityFingerprint(journal, testStateCodec())
	if err != nil {
		t.Fatalf("recoveryJournalAuthorityFingerprint() error = %v", err)
	}
	const wantFingerprint = "sha256:ddc6a2152c710cb484b89427af3d673cd8d78bd646f56f862727632d8c25a2b1"
	if fingerprint != wantFingerprint {
		t.Fatalf("journal fingerprint = %q, want %q", fingerprint, wantFingerprint)
	}
}

func TestRecoveryJournalV7PathDTOConversionOwnsExplicitZeroMode(t *testing.T) {
	t.Parallel()

	zero := recovery.PermissionMode(0)
	canonical := recovery.BeforePathState{
		Existed:     true,
		PathMode:    &zero,
		Kind:        recovery.PathKindFile,
		ContentHash: testBeforeHash,
		BackupPath:  testBackupPath,
	}
	persisted := persistedBeforePathState(canonical)
	if persisted.PathMode == nil || *persisted.PathMode != 0 {
		t.Fatalf("persisted path mode = %v, want explicit 0000", persisted.PathMode)
	}

	*canonical.PathMode = recovery.PermissionMode(0o777)
	if *persisted.PathMode != 0 {
		t.Fatalf("persisted path mode aliases canonical input: %#o", *persisted.PathMode)
	}

	disclosed := persisted.canonical()
	*disclosed.PathMode = recovery.PermissionMode(0o600)
	if *persisted.PathMode != 0 {
		t.Fatalf("canonical path mode aliases persisted DTO: %#o", *persisted.PathMode)
	}
}
