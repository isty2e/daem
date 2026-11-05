package journal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestPiOrderEvidenceJoinsRealImageCustodyAndSelectedRecovery(t *testing.T) {
	fixture, input := publishedPiOrderFixture(t, target.ScopeProject)
	loaded, err := loadDocuments(fixture.journal, t.Context(), fixture.read)
	if err != nil || loaded.Len() != 1 {
		t.Fatal("published Pi order did not reconstruct", err)
	}
	value := requireLoadedNativeDocument(t, loaded, 0)
	restored, err := value.Group().Comparison().RestoreBefore(t.Context(), value.Images().Expected().Document(), value.Images().Expected().Mode())
	if err != nil || !restored.Document().Equal(input.Document) {
		t.Fatalf("actual persisted order and private images failed selected rollback: %v", err)
	}
	changed := publishedNativeGroupVariant(t, fixture, func(group map[string]json.RawMessage) {
		var image map[string]json.RawMessage
		if err := json.Unmarshal(group["expected"], &image); err != nil {
			t.Fatal(err)
		}
		var reference map[string]json.RawMessage
		if err := json.Unmarshal(image["artifact"], &reference); err != nil {
			t.Fatal(err)
		}
		reference["digest"] = nativeRecordJSON(t, artifact.HashFileContent(input.Document.Content()))
		reference["size"] = nativeRecordJSON(t, input.Document.ContentSize())
		image["artifact"] = nativeRecordJSON(t, reference)
		group["expected"] = nativeRecordJSON(t, image)
	})
	session, err := changed.BeginLoadedDocuments(t.Context(), fixture.read.limits, fixture.read.codecs)
	if err != nil {
		t.Fatal(err)
	}
	demand, present, err := session.Next(t.Context())
	if err != nil || !present {
		t.Fatal("Pi variant has no image demand", err)
	}
	reference, err := demand.Expected().FileReference()
	if err != nil {
		t.Fatal(err)
	}
	root, err := fixture.read.artifacts.Authority()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.PhysicalRoot(), filepath.FromSlash(reference)), input.Document.Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	original, expected, err := loadDocumentImages(t.Context(), demand.Original(), demand.Expected(), fixture.read.artifacts, journalTestFilesystem(), nativeFixtureBudget(t), pipackage.MaximumSettingsBytes)
	if err != nil || !expected.Document().Equal(input.Document) {
		t.Fatal("wrong-order image did not reach semantic validation with a valid hash", err)
	}
	if err := session.SupplyLoaded(t.Context(), demand, original, expected); err == nil {
		t.Fatal("a valid hashed image incorrectly substituted for the actual expected Pi order")
	}
	if result, err := session.FinishLoaded(t.Context()); err == nil || result.Len() != 0 {
		t.Fatal("wrong expected Pi order returned a reconstructed prefix")
	}
}

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
