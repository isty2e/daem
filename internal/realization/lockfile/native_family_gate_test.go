package lockfile

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativeWireDoesNotGrantCompleteLockFamilyAdmission(t *testing.T) {
	base := claudeProjectMCPSubjectContract(t)
	observe, present := base.OperationContract(lock.OperationObserve)
	if !present {
		t.Fatal("MCP fixture lacks its observe contract")
	}
	id, err := entity.New(entity.KindHostConfig, "native")
	if err != nil {
		t.Fatal(err)
	}
	for _, property := range []string{"ordinary", "a/b", ""} {
		key, err := output.NewKeyPath(property)
		if err != nil {
			t.Fatal(err)
		}
		spec := nativeWireRealization(t, key)
		subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeProject, key)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := lock.NewLockedSubjectContract(lock.LockedSubjectContractInput{
			EntityID: id, SubjectID: subject, Realization: &spec,
			Ownership: lock.OwnershipManifest, OnAbsent: lock.OnAbsentApply,
			Replay: base.ReplayCoverage(), OperationContracts: []lock.OperationContract{observe},
		})
		if err != nil {
			t.Fatal(err)
		}
		dto, err := subjectToDTO(contract)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := subjectFromDTO(dto); err != nil {
			t.Fatalf("native transport was refused before the family gate: %v", err)
		}
		file, err := snapshotFromDTO(fileDTO{
			Version: int64(lock.CurrentVersion), Locked: lockedSectionDTO{Subjects: []lockedSubjectDTO{dto}},
		})
		if err == nil || file.Version != 0 || len(file.Locked.Subjects()) != 0 {
			t.Fatal("native transport bypassed complete closed lock admission")
		}
	}
}
