//go:build darwin || linux

package execute

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRecoveryBackupAdmissionRefusesLateConflictWithoutAReadablePrefix(t *testing.T) {
	fixture := newGlobalFileRecoveryFixture(t, outputtest.Parse(t, "~/.codex/AGENTS.md"), true)
	action := recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])
	authority, err := newRecoveryMutationAuthority(fixture.paths, fixture.plan, destinationResolver(fixture.paths), testFilesystem(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })
	active := bindRecoveryBackupSourceForTest(t, t.Context(), authority, fixture.plan)
	request := action.backupRequest(0)
	conflict := request
	conflict.Index = 1
	conflict.BackupHash = string(artifact.HashFileContent([]byte("conflicting backup")))
	set := authority.hostBackups()
	if err := set.Prepare(t.Context(), fixture.plan.OperationDir(), []hostmutation.BackupRequest{request, conflict}, active); err == nil {
		t.Fatal("later conflicting backup authority was accepted")
	}
	if _, err := set.For(request); err == nil {
		t.Fatal("failed backup admission exposed a readable prefix")
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
}

func TestRecoveryBackupAdmissionReservesSharedUsesAndRetainsNamespaceCustody(t *testing.T) {
	for _, test := range []struct {
		name            string
		closeAfterFirst bool
	}{
		{name: "reserved uses"},
		{name: "closed namespace", closeAfterFirst: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newGlobalFileRecoveryFixture(t, outputtest.Parse(t, "~/.codex/AGENTS.md"), true)
			action := recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])
			authority, err := newRecoveryMutationAuthority(fixture.paths, fixture.plan, destinationResolver(fixture.paths), testFilesystem(), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = authority.close() })
			active := bindRecoveryBackupSourceForTest(t, t.Context(), authority, fixture.plan)
			request := action.backupRequest(0)
			shared := request
			shared.Index = 1
			set := authority.hostBackups()
			if err := set.Prepare(t.Context(), fixture.plan.OperationDir(), []hostmutation.BackupRequest{request, shared}, active); err != nil {
				t.Fatal(err)
			}
			reader, err := set.For(request)
			if err != nil {
				t.Fatal(err)
			}
			content, err := reader.ReadFile(t.Context())
			if err != nil || !bytes.Equal(content, fixture.before) {
				t.Fatalf("first admitted backup use = %q, %v", content, err)
			}
			if test.closeAfterFirst {
				if err := authority.close(); err != nil {
					t.Fatal(err)
				}
				if _, err := reader.ReadFile(t.Context()); err == nil {
					t.Fatal("selected backup reader outlived its closed namespace")
				}
			} else {
				content, err := reader.ReadFile(t.Context())
				if err != nil || !bytes.Equal(content, fixture.before) {
					t.Fatalf("second admitted shared use = %q, %v", content, err)
				}
				if _, err := reader.ReadFile(t.Context()); err == nil {
					t.Fatal("shared backup use fell back to new execution capacity")
				}
			}
		})
	}
}
