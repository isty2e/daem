package extension

import (
	"strings"
	"testing"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestPlanOrderBlocksNativeOrderThatReversesExistingIntent(t *testing.T) {
	leftKey := testCarrierKey(t, "left")
	rightKey := testCarrierKey(t, "right")
	left := testExtension(t, "left", leftKey)
	right := testExtension(t, "right", rightKey)
	leftIdentity := testLoadIdentity(t, "left")
	rightIdentity := testLoadIdentity(t, "right")
	candidates := map[desiredextension.CarrierKey]candidateFact{
		leftKey:  {key: leftKey, loadIdentity: leftIdentity},
		rightKey: {key: rightKey, loadIdentity: rightIdentity},
	}
	assigned, err := assignExtensionIDs(
		candidates,
		[]desiredextension.Extension{left, right},
	)
	if err != nil {
		t.Fatal(err)
	}
	capability := mustOrderCapability(
		t,
		target.TargetOpenCode,
		desiredextension.CarrierOpenCodePlugin,
		target.ScopeProject,
	)
	sequence := testSequenceFact(
		t,
		capability,
		capability.PhysicalSequenceIDs()[0],
		[]sequenceRowFact{
			{loadIdentity: rightIdentity, key: rightKey, correlated: true},
			{loadIdentity: leftIdentity, key: leftKey, correlated: true},
		},
	)

	_, _, _, _, _, err = planOrder(
		candidates,
		[]desiredextension.Extension{left, right},
		map[desiredextension.CarrierKey]hostrelation.HostLoadIdentity{
			leftKey:  leftIdentity,
			rightKey: rightIdentity,
		},
		assigned,
		[]sequenceFact{sequence},
	)
	if err == nil || !strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("planOrder error = %v", err)
	}
}

func TestPlanOrderCombinesConsistentPhysicalSequences(t *testing.T) {
	leftKey := testCarrierKey(t, "left")
	rightKey := testCarrierKey(t, "right")
	leftIdentity := testLoadIdentity(t, "left")
	rightIdentity := testLoadIdentity(t, "right")
	candidates := map[desiredextension.CarrierKey]candidateFact{
		leftKey:  {key: leftKey, loadIdentity: leftIdentity},
		rightKey: {key: rightKey, loadIdentity: rightIdentity},
	}
	assigned, err := assignExtensionIDs(candidates, nil)
	if err != nil {
		t.Fatal(err)
	}
	capability := mustOrderCapability(
		t,
		target.TargetOpenCode,
		desiredextension.CarrierOpenCodePlugin,
		target.ScopeProject,
	)
	rows := []sequenceRowFact{
		{loadIdentity: leftIdentity, key: leftKey, correlated: true},
		{loadIdentity: rightIdentity, key: rightKey, correlated: true},
	}
	sequences := []sequenceFact{
		testSequenceFact(
			t,
			capability,
			capability.PhysicalSequenceIDs()[0],
			rows,
		),
		testSequenceFact(
			t,
			capability,
			capability.PhysicalSequenceIDs()[1],
			rows,
		),
	}

	imported, ordered, keys, observed, constraints, err := planOrder(
		candidates,
		nil,
		nil,
		assigned,
		sequences,
	)
	if err != nil {
		t.Fatalf("planOrder: %v", err)
	}
	if len(imported) != 2 ||
		len(ordered) != 2 ||
		len(keys) != 2 ||
		keys[0] != leftKey ||
		keys[1] != rightKey {
		t.Fatalf("order = %#v", keys)
	}
	if len(observed) != 2 || len(constraints) != 1 {
		t.Fatalf(
			"observed=%d constraints=%d",
			len(observed),
			len(constraints),
		)
	}
	if got := constraints[0].Members(); len(got) != 2 ||
		got[0].HostLoadIdentity() != leftIdentity ||
		got[1].HostLoadIdentity() != rightIdentity {
		t.Fatalf("constraint members = %#v", got)
	}
}

func testExtension(
	t *testing.T,
	id string,
	key desiredextension.CarrierKey,
) desiredextension.Extension {
	t.Helper()
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    id,
		Carrier: key.Carrier(),
		Target:  key.Target(),
		Scope:   key.Scope(),
		Source:  key.Source(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustOrderCapability(
	t *testing.T,
	selectedTarget target.Target,
	carrier desiredextension.Carrier,
	scope target.Scope,
) profile.ExtensionOrderCapability {
	t.Helper()
	capability, ok := profile.Profile(selectedTarget).ExtensionOrder(
		carrier,
		scope,
	)
	if !ok {
		t.Fatal("extension order capability is absent")
	}
	return capability
}

func testSequenceFact(
	t *testing.T,
	capability profile.ExtensionOrderCapability,
	sequenceID hostrelation.PhysicalSequenceID,
	rows []sequenceRowFact,
) sequenceFact {
	t.Helper()
	authority, err := relationobserve.NewSequenceAuthority(
		"test:" + string(sequenceID),
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := relationobserve.NewSequenceRevision("sha256:test")
	if err != nil {
		t.Fatal(err)
	}
	return sequenceFact{
		classID:   capability.ClassID(),
		sequence:  sequenceID,
		authority: authority,
		revision:  revision,
		rows:      append([]sequenceRowFact(nil), rows...),
	}
}
