package document_test

import (
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func nativeReleaseSnapshot(t *testing.T, selected target.Target, scope target.Scope, ownership hostconfig.Ownership, name string) (durable.Snapshot, entity.ID) {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, name)
	if err != nil {
		t.Fatal(err)
	}
	path := name + ".json"
	if selected == target.TargetOpenCode && scope == target.ScopeProject {
		path = ".opencode/" + path
	}
	placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: selected, Scope: scope, Path: path, Ownership: ownership})
	if err != nil {
		t.Fatal(err)
	}
	var input durable.SnapshotInput
	input.NativeDocumentCommit = 7
	if ownership == hostconfig.OwnershipDocument {
		subject, err := aggregate.HostConfigDocumentSubject(id, selected, scope)
		if err != nil {
			t.Fatal(err)
		}
		row, err := durable.NewManagedPathState(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent([]byte("{}")), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		input.ManagedPaths = []durable.ManagedPathState{row}
	} else {
		for _, name := range []string{"first", "second"} {
			key, err := output.NewKeyPath(name)
			if err != nil {
				t.Fatal(err)
			}
			footprint, err := output.NewKeyFootprint(key)
			if err != nil {
				t.Fatal(err)
			}
			contract, err := aggregate.NewHostConfigProjectionContract(selected, scope, placement.Destination(), footprint, "native-json-v1")
			if err != nil {
				t.Fatal(err)
			}
			value, err := aggregate.NewManagedContribution(contract, "1")
			if err != nil {
				t.Fatal(err)
			}
			subject, err := topologyprojection.HostConfigKeySubject(id, selected, scope, key)
			if err != nil {
				t.Fatal(err)
			}
			row, err := durable.NewManagedAggregateState(subject, value)
			if err != nil {
				t.Fatal(err)
			}
			input.ManagedAggregates = append(input.ManagedAggregates, row)
		}
	}
	snapshot, err := durable.NewSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, id
}

func TestHostConfigManagementReleaseKeepsTheCompleteUnselectedSnapshot(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex, target.TargetClaudeCode, target.TargetOpenCode, target.TargetAntigravityCLI} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			if selected == target.TargetAntigravityCLI && scope == target.ScopeProject {
				continue
			}
			for _, ownership := range []hostconfig.Ownership{hostconfig.OwnershipKeys, hostconfig.OwnershipDocument} {
				t.Run(fmt.Sprintf("%s/%s/%s", selected, scope, ownership), func(t *testing.T) {
					chosen, id := nativeReleaseSnapshot(t, selected, scope, ownership, "chosen")
					peer, _ := nativeReleaseSnapshot(t, selected, scope, hostconfig.OwnershipDocument, "peer")
					before, err := durable.NewSnapshot(durable.SnapshotInput{
						ManagedPaths: append(chosen.ManagedPaths(), peer.ManagedPaths()...), ManagedAggregates: chosen.ManagedAggregates(), NativeDocumentCommit: 7,
					})
					if err != nil {
						t.Fatal(err)
					}
					identities, err := journaldocument.HostConfigManagements(t.Context(), before, id)
					if err != nil || len(identities) != 1 {
						t.Fatal("retained selection failed", identities, err)
					}
					identity := identities[0]
					if identity.Declaration() != id || identity.Target() != selected || identity.Scope() != scope || identity.Ownership() != ownership {
						t.Fatal("retained identity changed its native placement")
					}
					subjects := identity.Subjects()
					subjects[0] = topology.SubjectID{}
					after, err := identity.RemoveFrom(t.Context(), before)
					if err != nil || after.Equal(before) || !after.Equal(peer) || after.NativeDocumentCommit() != before.NativeDocumentCommit() {
						t.Fatal("release altered unrelated state or did not remove management", err)
					}
					if _, err := identity.RemoveFrom(t.Context(), after); err == nil {
						t.Fatal("absent management admitted an equal-snapshot release")
					}
				})
			}
		}
	}
}

func TestHostConfigManagementCannotReleaseAChangedMemberSet(t *testing.T) {
	before, id := nativeReleaseSnapshot(t, target.TargetPi, target.ScopeProject, hostconfig.OwnershipKeys, "chosen")
	identities, err := journaldocument.HostConfigManagements(t.Context(), before, id)
	if err != nil || len(identities) != 1 {
		t.Fatal(identities, err)
	}
	changed, err := before.WithManagedAggregates(before.ManagedAggregates()[:1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identities[0].RemoveFrom(t.Context(), changed); err == nil {
		t.Fatal("release silently selected only the surviving member")
	}
	if _, err := (journaldocument.HostConfigManagement{}).RemoveFrom(t.Context(), before); err == nil {
		t.Fatal("zero identity selected retained management")
	}
}

func TestHostConfigManagementRefusesAmbiguousOrContradictoryWholeFilePlacement(t *testing.T) {
	valid, id := nativeReleaseSnapshot(t, target.TargetPi, target.ScopeProject, hostconfig.OwnershipDocument, "chosen")
	row := valid.ManagedPaths()[0]
	for _, consumers := range [][]target.Target{{target.TargetPi, target.TargetCodex}, {target.TargetCodex}} {
		malformed, err := durable.NewManagedPathState(row.Subject(), consumers, row.Scope(), row.Destination(),
			row.ContentHash(), row.ContentKind(), row.PermissionPolicy(), row.FileMode())
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{malformed}})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := journaldocument.HostConfigManagements(t.Context(), snapshot, id); err == nil || len(result) != 0 {
			t.Fatal("whole-file placement contradiction selected a release identity")
		}
	}
}
