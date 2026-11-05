package publication

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func piEvidenceFixture(t *testing.T, scope target.Scope) (pipackage.LoadedOrderInput, pipackage.OrderObservation) {
	t.Helper()
	root := t.TempDir()
	command, home := filepath.Join(root, "command"), filepath.Join(root, "home")
	settingsBase := filepath.Join(command, ".pi")
	if scope == target.ScopeGlobal {
		settingsBase = filepath.Join(root, "agent")
	}
	commandContext, err := extensiontopology.NewLocalSourceContext(command, home)
	if err != nil {
		t.Fatal(err)
	}
	settingsContext, err := extensiontopology.NewLocalSourceContext(settingsBase, home)
	if err != nil {
		t.Fatal(err)
	}
	var relations []pipackage.ScopedRelation
	var members []hostrelation.RelationOrderMember
	for index, source := range []string{"./a", "~/b"} {
		name := []string{"a", "b"}[index]
		subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "pi.package-carrier", name)
		if err != nil {
			t.Fatal(err)
		}
		sourceKey, err := hostrelation.NewSubjectKey(source)
		if err != nil {
			t.Fatal(err)
		}
		managed, err := hostrelation.NewManagedInstanceKey("host-relation:v1:" + name)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hostrelation.NewExpectedRelation(sourceKey, managed)
		if err != nil {
			t.Fatal(err)
		}
		key, err := observerelation.NewCorrelationKey(subject, expected)
		if err != nil {
			t.Fatal(err)
		}
		relation, err := pipackage.NewScopedRelationWithContext(key, scope, commandContext)
		if err != nil {
			t.Fatal(err)
		}
		relations = append(relations, relation)
		identity, err := pipackage.HostLoadIdentityForContext(source, scope, commandContext)
		if err != nil {
			t.Fatal(err)
		}
		loadIdentity, err := hostrelation.NewHostLoadIdentity(identity)
		if err != nil {
			t.Fatal(err)
		}
		member, err := hostrelation.NewRelationOrderMember(subject, loadIdentity)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, member)
	}
	capability, found := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetPi, scope, desiredextension.CarrierPiPackage)
	if !found {
		t.Fatal("Pi order fixture capability is unavailable")
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(capability.ClassID(), capability.MemberIdentityContract(), capability.RuntimeMeaning(), members)
	if err != nil {
		t.Fatal(err)
	}
	storedA, err := filepath.Rel(settingsBase, filepath.Join(command, "a"))
	if err != nil {
		t.Fatal(err)
	}
	storedB, err := filepath.Rel(settingsBase, filepath.Join(home, "b"))
	if err != nil {
		t.Fatal(err)
	}
	quotedA, err := json.Marshal(storedA)
	if err != nil {
		t.Fatal(err)
	}
	quotedB, err := json.Marshal(storedB)
	if err != nil {
		t.Fatal(err)
	}
	input := pipackage.LoadedOrderInput{
		Scope: scope, SettingsContext: settingsContext, Constraint: constraint, Relations: relations,
		Document: aggregate.ExistingDocument([]byte(`{"packages":[{"source":` + string(quotedB) + `,"opaque":{"User-Key":null}},"npm:foreign@1",` + string(quotedA) + `],"theme":"day"}`)),
	}
	observation, err := pipackage.ReconstructOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Changed() {
		t.Fatal("fixture did not establish real fixed-slot order work")
	}
	return input, observation
}
