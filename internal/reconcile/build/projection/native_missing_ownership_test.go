package projection

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativeAbsentRetirementDoesNotAdmitDesiredRecreationOrOrdinaryAbsence(t *testing.T) {
	_, sets := nativeNarrowingPlanFixture(t, `{"a":2,"z":2}`, "@pi-agent/extension-settings/test.json")
	set := sets[0]
	item := set.Contributions()[0]
	row, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	codec, err := nativecodec.New(t.Context(), set.Contract().CodecContractID(), limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	current, err := aggregate.NewProjectionState(set.Contract(), false, false, "")
	if err != nil {
		t.Fatal(err)
	}
	group := aggregateGroupInput{contract: set.Contract(), desired: set.Contributions(), previous: []durable.ManagedAggregateState{row}}
	blocked, _, err := prepareAggregateProjection(codec, group, current)
	if err != nil || blocked.Kind() != reconcile.AggregateBlocked || blocked.Reason() != reconcile.ReasonMissingOutput {
		t.Fatal("ordinary native planning recreated a missing desired owned key", err)
	}
	group.desired = nil
	retiring, intent, err := prepareAggregateProjection(codec, group, current)
	if err != nil || retiring.Kind() == reconcile.AggregateBlocked || intent.Before().Present() {
		t.Fatal("native absent retirement manufactured present before evidence", err)
	}
	ordinary, ordinaryRow := aggregateOwnershipProjection(t)
	canonical, err := mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.ClaudeGlobalMCPServerProjection{
		ServerID: "context7", Command: "npx", Args: []string{}, Env: map[string]string{},
		AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(ordinary.contract, string(canonical))
	if err != nil {
		t.Fatal(err)
	}
	ordinaryRow, err = durable.NewManagedAggregateState(ordinaryRow.Subject(), contribution)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryCurrent, err := aggregate.NewProjectionState(ordinary.contract, false, false, "")
	if err != nil {
		t.Fatal(err)
	}
	ordinaryCodec, present := aggregatecodec.Catalog().Lookup(ordinary.contract.CodecContractID())
	if !present {
		t.Fatal("ordinary fixture codec is missing")
	}
	prior := aggregateGroupInput{contract: ordinary.contract, previous: []durable.ManagedAggregateState{ordinaryRow}}
	refused, _, err := prepareAggregateProjection(ordinaryCodec, prior, ordinaryCurrent)
	if err != nil || refused.Kind() != reconcile.AggregateBlocked || refused.Reason() != reconcile.ReasonMissingOutput {
		t.Fatal("native absence settlement widened ordinary missing-baseline behavior", err)
	}
	if _, err := aggregate.NewProjectionIntent(ordinaryCurrent, nil); err == nil {
		t.Fatal("native intent admission widened fixed-codec absent removal")
	}
}

func TestNativeManagedPathClaimCorrelationDoesNotWidenOrdinaryEnforcement(t *testing.T) {
	root := t.TempDir()
	owner := aggregateOwnershipAuthority(t, root, "owner")
	physical := filepath.Join(root, "Café.json")
	exact := pathtest.DarwinCaseSensitive(physical)
	missing, err := pathauthority.NewProvisional(physical, exact.Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(exact, output.WholePathFootprint())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := ownership.PresentClaim(claim)
	destination, err := output.Parse("@pi-agent/extension-settings/test.json")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := observe.NewProvisionalOwnershipObservation(destination, output.WholePathFootprint(), missing, value)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	native, err := aggregate.HostConfigDocumentSubject(id, target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, blocked := managedPathActiveClaimBlock(observation, owner, native); blocked {
		t.Fatal("matching native missing history failed claim correlation")
	}
	skillID, err := entity.New(entity.KindSkill, "settings")
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := topologyprojection.Subject(skillID, "pi.global.skills")
	if err != nil {
		t.Fatal(err)
	}
	if reason, _, blocked := managedPathActiveClaimBlock(observation, owner, ordinary); !blocked || reason != reconcile.ReasonOwnershipConflict {
		t.Fatal("ordinary enforcement accepted provisional history as exact")
	}
	reserved, err := ownership.NewReservedClaim(address, owner, "pending")
	if err != nil {
		t.Fatal(err)
	}
	reservedValue, _ := ownership.PresentClaim(reserved)
	reservedObservation, err := observe.NewProvisionalOwnershipObservation(destination, output.WholePathFootprint(), missing, reservedValue)
	if err != nil {
		t.Fatal(err)
	}
	if reason, _, blocked := managedPathActiveClaimBlock(reservedObservation, owner, native); !blocked || reason != reconcile.ReasonOwnershipReserved {
		t.Fatal("missing-history correlation bypassed a reserved native claim")
	}
}
