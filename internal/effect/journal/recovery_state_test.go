package journal

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestRecoveryJournalRejectsInvalidPersistedSubject(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*recoveryEntry)
		want   string
	}{
		{
			name: "missing subject",
			mutate: func(entry *recoveryEntry) {
				entry.Subject = persistedSubjectRef{}
			},
			want: "subject is required",
		},
		{
			name: "partial subject",
			mutate: func(entry *recoveryEntry) {
				entry.Subject.Namespace = ""
			},
			want: "kind, namespace, and name are required together",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := defaultRecoveryJournal()
			test.mutate(&journal.Entries[0])
			if err := validateRecoveryJournal(journal, testStateCodec()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateRecoveryJournal error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRecoveryJournalRejectsContentPathWithoutProjectionContract(t *testing.T) {
	journal := defaultRecoveryJournal()
	journal.Entries[0].ContentPath = "/mcpServers/context7"

	err := validateRecoveryJournal(journal, testStateCodec())
	if err == nil || !strings.Contains(err.Error(), "content path requires a projection contract") {
		t.Fatalf("validateRecoveryJournal error = %v, want missing projection-contract rejection", err)
	}
}

func TestRecoveryJournalRejectsInvalidManagedMembership(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*recoveryEntry)
		want   string
	}{
		{
			name: "managed state without hash",
			mutate: func(entry *recoveryEntry) {
				entry.StateBefore = recoveryManagedMembership{Managed: true}
			},
			want: "state_before: managed state content hash is required",
		},
		{
			name: "unmanaged expected state with hash",
			mutate: func(entry *recoveryEntry) {
				entry.StateExpectedAfter = recoveryManagedMembership{ContentHash: "sha256:stale"}
			},
			want: "state_expected_after: unmanaged state must not contain a content hash",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := defaultRecoveryJournal()
			test.mutate(&journal.Entries[0])
			if err := validateRecoveryJournal(journal, testStateCodec()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateRecoveryJournal error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRecoveryJournalValidatesManagedPathConsumerSetAndContentKind(t *testing.T) {
	entry := managedPathRecoveryEntry()
	journal := recoveryJournalFor(entry)
	if err := validateRecoveryJournal(journal, testStateCodec()); err != nil {
		t.Fatalf("valid managed path recovery journal: %v", err)
	}

	after := journal.StatefileAfter.ManagedPaths()[0]
	drifted, err := durable.NewManagedPathState(
		after.Subject(),
		[]target.Target{target.TargetAntigravityCLI, target.TargetCodex},
		after.Scope(),
		after.Destination(),
		after.ContentHash(),
		after.ContentKind(),
		after.PermissionPolicy(),
		after.FileMode(),
	)
	if err != nil {
		t.Fatalf("construct consumer drift state: %v", err)
	}
	journal.StatefileAfter = statefileFor(drifted)
	if err := validateRecoveryJournal(journal, testStateCodec()); err == nil || !strings.Contains(err.Error(), "consumers") {
		t.Fatalf("consumer drift error = %v, want exact consumer-set rejection", err)
	}

	journal = recoveryJournalFor(entry)
	after = journal.StatefileAfter.ManagedPaths()[0]
	drifted, err = durable.NewManagedPathState(
		after.Subject(),
		after.ConsumerTargets(),
		after.Scope(),
		after.Destination(),
		after.ContentHash(),
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
		0,
	)
	if err != nil {
		t.Fatalf("construct content-kind drift state: %v", err)
	}
	journal.StatefileAfter = statefileFor(drifted)
	if err := validateRecoveryJournal(journal, testStateCodec()); err == nil || !strings.Contains(err.Error(), "managed path occupancy") {
		t.Fatalf("content-kind drift error = %v, want persisted occupancy rejection", err)
	}

	journal = recoveryJournalFor(entry)
	journal.Entries[0].ContentKind = ""
	if err := validateRecoveryJournal(journal, testStateCodec()); err == nil || !strings.Contains(err.Error(), "realization discriminator") {
		t.Fatalf("missing realization discriminator error = %v, want ambiguous identity rejection", err)
	}
}

func TestRecoveryJournalKeepsExactManagedPathBaselineSeparateFromPhysicalMode(t *testing.T) {
	journal := exactManagedPathModeRecoveryJournal()
	if err := validateRecoveryJournal(journal, testStateCodec()); err != nil {
		t.Fatalf("physical permission drift must not rewrite durable exact-mode history: %v", err)
	}
}

func TestRecoveryJournalDoesNotPromoteExecutableClassToExactModeState(t *testing.T) {
	journal := exactManagedPathModeRecoveryJournal()
	before := journal.StatefileBefore.ManagedPaths()[0]
	after := journal.StatefileAfter.ManagedPaths()[0]
	journal.StatefileBefore = statefileFor(resourceStateWithPermissions(
		journal.Entries[0],
		string(before.ContentHash()),
		realization.PathPermissionsExecutableClass,
		0,
	))
	journal.StatefileAfter = statefileFor(resourceStateWithPermissions(
		journal.Entries[0],
		string(after.ContentHash()),
		realization.PathPermissionsExecutableClass,
		0,
	))
	journal.Entries[0].Before.PathMode = testRecoveryPermissionMode(0o755)
	journal.Entries[0].ExpectedAfter.PathMode = testRecoveryPermissionMode(0o700)

	if err := validateRecoveryJournal(journal, testStateCodec()); err != nil {
		t.Fatalf("executable-class recovery journal treated read/write bits as state: %v", err)
	}
}

func TestRecoveryJournalRejectsManagedPathNonDirectoryHostShapes(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*recoveryEntry)
	}{
		{
			name: "before symlink",
			mutate: func(entry *recoveryEntry) {
				entry.Before.Kind = recovery.PathKindSymlink
				entry.Before.ContentHash = ""
				entry.Before.BackupPath = ""
				entry.Before.LinkTarget = "/tmp/foreign"
			},
		},
		{
			name: "expected symlink",
			mutate: func(entry *recoveryEntry) {
				entry.ExpectedAfter.Kind = recovery.PathKindSymlink
				entry.ExpectedAfter.ContentHash = ""
				entry.ExpectedAfter.LinkTarget = "/tmp/foreign"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := recoveryJournalFor(managedPathRecoveryEntry())
			test.mutate(&journal.Entries[0])
			if err := validateRecoveryJournal(journal, testStateCodec()); err == nil ||
				!strings.Contains(err.Error(), "managed path requires directory kind") {
				t.Fatalf("validateRecoveryJournal error = %v", err)
			}
		})
	}
}

func exactManagedPathModeRecoveryJournal() recoveryJournal {
	digest := strings.Repeat("a", 64)
	contentHash := "sha256:" + digest
	entry := recoveryEntry{
		Subject: persistedSubjectRef{
			Kind:      string(topology.SubjectProjection),
			Namespace: "hook-asset.project.data",
			Name:      "hook_asset:runner",
		},
		Targets:     []string{string(target.TargetCodex)},
		Scope:       string(target.ScopeProject),
		Path:        ".daem/hook-assets/runner/sha256-" + digest + "/asset",
		ContentKind: string(realization.PathProjectionFile),
		Before: persistedBeforePathState(recovery.BeforePathState{
			Existed: true, PathMode: testRecoveryPermissionMode(0o755), Kind: recovery.PathKindFile,
			ContentHash: contentHash, BackupPath: "backup-0000",
		}),
		ExpectedAfter: persistedExpectedPathState(recovery.ExpectedPathState{
			Existed: true, PathMode: testRecoveryPermissionMode(0o700), Kind: recovery.PathKindFile,
			ContentHash: contentHash,
		}),
		StateBefore:        recoveryManagedMembership{Managed: true, ContentHash: contentHash},
		StateExpectedAfter: recoveryManagedMembership{Managed: true, ContentHash: contentHash},
	}
	journal := recoveryJournalFor(entry)
	journal.StatefileBefore = statefileFor(resourceStateWithPermissions(
		entry,
		contentHash,
		realization.PathPermissionsExact,
		0o700,
	))
	journal.StatefileAfter = statefileFor(resourceStateWithPermissions(
		entry,
		contentHash,
		realization.PathPermissionsExact,
		0o700,
	))
	return journal
}

func TestValidateRecoveryJournalAllowsOnlyMeaningfulStateOnlyOperations(t *testing.T) {
	journal := defaultRecoveryJournal()
	entry := journal.Entries[0]
	journal.Entries = nil
	journal.StatefileBefore = durable.EmptySnapshot()
	journal.StatefileAfter = statefileFor(
		resourceState(entry, entry.ExpectedAfter.ContentHash),
	)
	if err := validateRecoveryJournal(journal, testStateCodec()); err != nil {
		t.Fatalf("state-only recovery journal validation: %v", err)
	}

	journal.StatefileAfter = journal.StatefileBefore
	if err := validateRecoveryJournal(journal, testStateCodec()); err == nil || !strings.Contains(err.Error(), "host entries or a statefile change") {
		t.Fatalf("meaningless state-only journal error = %v", err)
	}
}
