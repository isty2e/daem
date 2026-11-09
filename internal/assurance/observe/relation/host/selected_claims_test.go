package relationhost

import (
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func selectedCarrierClaim(t *testing.T, owner stateauthority.Authority, key desiredextension.CarrierKey, namespace, name string) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, namespace, name)
	if err != nil {
		t.Fatal(err)
	}
	source, err := hostrelation.NewSubjectKey(key.Source().Ref())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.Derive(key, subject, source)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindExtension, name)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(id, key, subject, source)
	if err != nil {
		t.Fatal(err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(owner, identity, request, durablecarrier.ClaimProvenanceInstalledObserved)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func TestSelectedEditsFilterActionClaimsAndRetainTheirExplicitCommandContext(t *testing.T) {
	root := t.TempDir()
	base := selectedPiFixture(t, root, "pi.package-carrier", `{"packages":["npm:b@1","../retired","../other-scope","../other-host","npm:foreign@1","npm:a@1"]}`)
	owner, err := stateauthority.New(pathtest.DarwinCaseSensitive(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claims := []durablecarrier.ManagedCarrierClaim{
		selectedCarrierClaim(t, owner, settingsProfileKey(t, desiredextension.CarrierPiPackage, target.TargetPi, target.ScopeProject, "./retired"), "pi.package-carrier", "retired"),
		selectedCarrierClaim(t, owner, settingsProfileKey(t, desiredextension.CarrierPiPackage, target.TargetPi, target.ScopeGlobal, "./other-scope"), "pi.package-carrier", "other-scope"),
		selectedCarrierClaim(t, owner, settingsProfileKey(t, desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode, target.ScopeProject, "./other-host"), "opencode.plugin-carrier", "other-host"),
	}
	edits, err := base.SelectedEdits(claims)
	if err != nil || len(edits) != 1 {
		t.Fatal("selected action claims did not reach their document factory", err)
	}
	completed, err := CompleteSelectedImage(t.Context(), edits[0].Original(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}, nil, edits)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"packages":["npm:a@1","../other-scope","../other-host","npm:foreign@1","npm:b@1"]}`
	if string(completed.Expected().Content()) != want {
		t.Fatal("retirement selected a foreign scope/host or lost its lexical command context", string(completed.Expected().Content()))
	}
	if roots := edits[0].DefinitionRoots(); len(roots) != 1 || roots[0] != root {
		t.Fatal("retirement normalization lost its explicit selected definition root", roots)
	}
}
