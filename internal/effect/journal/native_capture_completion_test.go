package journal

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func captureProjectCompletionFacts(t *testing.T, owner stateauthority.Authority, name string) (durablecarrier.PendingCarrierInstall, durablecarrier.ManagedCarrierClaim) {
	t.Helper()
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindMarketplace, "context7@market")
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, source)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "claude-code.plugin-carrier", name)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey("context7@market")
	if err != nil {
		t.Fatal(err)
	}
	relation, err := hostrelation.Derive(key, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, relation)
	if err != nil {
		t.Fatal(err)
	}
	request, err := realizationdelegate.NewRequest("claude-code.plugin-carrier.install", "claude-plugin-carrier-v1", "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(owner, identity, request, durablecarrier.ClaimProvenanceInstalledObserved)
	if err != nil {
		t.Fatal(err)
	}
	return pending, claim
}

func TestNativeCaptureRequiresDeclaredExactProjectCompletions(t *testing.T) {
	root := t.TempDir()
	owner, err := stateauthority.New(mustObservedPathAuthority(t, filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pending, claim := captureProjectCompletionFacts(t, owner, "selected")
	unselected, _ := captureProjectCompletionFacts(t, owner, "unselected")
	_, foreign := captureProjectCompletionFacts(t, owner, "foreign")
	before, err := durable.NewSnapshot(durable.SnapshotInput{PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending, unselected}})
	if err != nil {
		t.Fatal(err)
	}
	after, changed, err := before.WithPromotedCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil || !changed {
		t.Fatal(err)
	}
	if err := validateNativeCaptureWork(before, after, nil, nil, []durablecarrier.ManagedCarrierClaim{claim}); err != nil {
		t.Fatal("exact declared completion was refused", err)
	}
	if err := validateNativeCaptureWork(before, after, nil, nil, nil); err == nil {
		t.Fatal("undeclared carrier state change passed native capture")
	}
	if err := validateNativeCaptureWork(before, after, nil, nil, []durablecarrier.ManagedCarrierClaim{foreign}); err == nil {
		t.Fatal("claim without its exact pending acquisition passed native capture")
	}
	lost, err := after.WithPendingCarrierInstalls(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNativeCaptureWork(before, lost, nil, nil, []durablecarrier.ManagedCarrierClaim{claim}); err == nil {
		t.Fatal("declared completion authorized loss of unrelated pending history")
	}
}
