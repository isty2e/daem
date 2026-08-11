package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/target"
)

func TestRecoveryForwardRemovalCertificatesUsePlannedBackupWork(t *testing.T) {
	work, err := recovery.NewArtifactWork(0, 8)
	if err != nil {
		t.Fatal(err)
	}
	actions := []recoveryHostAction{
		{
			Kind: recovery.ActionKindRestoreWrite, Scope: target.ScopeProject,
			Destination: ".agents/skills/alpha", BackupPath: "backup",
			BackupHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			BackupKind: recovery.PathKindFile, BackupWork: work,
			BeforePathExisted: true, BeforeParentExisted: true,
			BeforePathMode: recovery.NewPermissionMode(0o600),
		},
		{
			Kind: recovery.ActionKindRestoreWrite, Scope: target.ScopeProject,
			Destination: ".agents/skills/beta", BackupPath: "backup",
			BackupHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			BackupKind: recovery.PathKindFile, BackupWork: work,
			BeforePathExisted: true, BeforeParentExisted: true,
			BeforePathMode: recovery.NewPermissionMode(0o600),
		},
	}
	demands := make([]recovery.RemovalDemand, 0, len(actions))
	for _, action := range actions {
		destination, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			t.Fatal(err)
		}
		state, err := recoveryActionBeforeRemovalState(action)
		if err != nil {
			t.Fatal(err)
		}
		demand, err := recovery.NewRemovalDemand(action.Scope, destination, []recovery.RemovalState{state})
		if err != nil {
			t.Fatal(err)
		}
		demands = append(demands, demand)
	}
	demandSet, err := recovery.NewRemovalDemandSet(demands)
	if err != nil {
		t.Fatal(err)
	}
	certificates, err := recoveryForwardRemovalCertificates(
		t.Context(),
		actions,
		demandSet,
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(certificates) != 2 {
		t.Fatalf("certificate count = %d, want 2", len(certificates))
	}

	maximum, err := recovery.NewArtifactWork(0, 8)
	if err != nil {
		t.Fatal(err)
	}
	for index, certificate := range certificates {
		measured, err := certificate.measure(t.Context(), maximum)
		if err != nil {
			t.Fatalf("measure certificate[%d]: %v", index, err)
		}
		if !measured.Equal(work) {
			t.Fatalf(
				"certificate[%d] work = entries:%d bytes:%d, want entries:%d bytes:%d",
				index,
				measured.Entries(),
				measured.Bytes(),
				work.Entries(),
				work.Bytes(),
			)
		}
	}
}
