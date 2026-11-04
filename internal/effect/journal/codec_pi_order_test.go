package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestPiOrderEvidenceRoundtripsActualContextsAndSemanticCandidate(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			input, observation := piEvidenceFixture(t, scope)
			record, err := persistedPiOrderEvidence(observation)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			var decoded recoveryPiOrderEvidence
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			for _, home := range []string{filepath.Join(t.TempDir(), "other-home"), ""} {
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				actual, err := decoded.canonical(t.Context(), input.Document)
				if err != nil {
					t.Fatal(err)
				}
				expectedBytes, expectedExists := observation.Candidate()
				actualBytes, actualExists := actual.Candidate()
				if expectedExists != actualExists || !bytes.Equal(expectedBytes, actualBytes) || !actual.ExpectedSequence().SameOrdering(observation.ExpectedSequence()) {
					t.Fatal("record replay changed the actual semantic candidate or consulted ambient context")
				}
			}
			decoded.Relations[0], decoded.Relations[1] = decoded.Relations[1], decoded.Relations[0]
			actual, err := decoded.canonical(t.Context(), input.Document)
			if err != nil || !actual.ExpectedSequence().SameOrdering(observation.ExpectedSequence()) {
				t.Fatalf("relation-array ordinal became semantic identity: %v", err)
			}
			captured, err := actual.CaptureInput()
			if err != nil {
				t.Fatal(err)
			}
			if captured.SettingsContext.HomeRoot() != input.SettingsContext.HomeRoot() || captured.SettingsContext.BaseRoot() != input.SettingsContext.BaseRoot() {
				t.Fatal("record lost the selected settings base or home")
			}
			for _, relation := range captured.Relations {
				context, bound := relation.SourceContext()
				if !bound || context.HomeRoot() != input.SettingsContext.HomeRoot() || relation.Scope() != scope {
					t.Fatal("scoped relation lost the actual captured context")
				}
			}
		})
	}
}

func TestPiOrderEvidenceRefusesMismatchedOrIncompleteSemanticFacts(t *testing.T) {
	input, observation := piEvidenceFixture(t, target.ScopeProject)
	original, err := persistedPiOrderEvidence(observation)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*recoveryPiOrderEvidence)
	}{
		{"physical sequence", func(record *recoveryPiOrderEvidence) { record.SequenceID = "other-sequence" }},
		{"scope", func(record *recoveryPiOrderEvidence) { record.Scope = string(target.ScopeGlobal) }},
		{"class", func(record *recoveryPiOrderEvidence) { record.Constraint.ClassID = "other-class" }},
		{"runtime meaning", func(record *recoveryPiOrderEvidence) { record.Constraint.RuntimeMeaning = "unknown" }},
		{"missing home", func(record *recoveryPiOrderEvidence) { record.SettingsContext.HomeRoot = "" }},
		{"changed command home", func(record *recoveryPiOrderEvidence) { record.Relations[1].SourceContext.HomeRoot = t.TempDir() }},
		{"changed settings base", func(record *recoveryPiOrderEvidence) { record.SettingsContext.BaseRoot = t.TempDir() }},
		{"missing relation", func(record *recoveryPiOrderEvidence) { record.Relations = record.Relations[:1] }},
		{"duplicate relation", func(record *recoveryPiOrderEvidence) {
			record.Relations = append(record.Relations, record.Relations[0])
		}},
		{"wrong full key", func(record *recoveryPiOrderEvidence) { record.Relations[0].ManagedInstanceKey = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := clonePiEvidence(t, original)
			test.edit(&record)
			actual, err := record.canonical(t.Context(), input.Document)
			if err == nil || actual.Validate() == nil {
				t.Fatal("incomplete or mismatched evidence returned a canonical order")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := original.canonical(ctx, input.Document); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
	if _, err := original.canonical(t.Context(), aggregate.ExistingDocument(bytes.Repeat([]byte(" "), 2*pipackage.MaximumSettingsBytes))); err == nil {
		t.Fatal("order replay admitted an oversized original image")
	}
}

func TestPiOrderEvidenceDoesNotPersistOrReconstructUnchangedOrder(t *testing.T) {
	_, observation := piEvidenceFixture(t, target.ScopeProject)
	record, err := persistedPiOrderEvidence(observation)
	if err != nil {
		t.Fatal(err)
	}
	content, exists := observation.Candidate()
	if !exists {
		t.Fatal("fixture must have actual package work")
	}
	if _, err := record.canonical(t.Context(), aggregate.ExistingDocument(content)); err == nil {
		t.Fatal("record claimed an unchanged order as selected recovery work")
	}
	input, err := observation.CaptureInput()
	if err != nil {
		t.Fatal(err)
	}
	input.Document = aggregate.ExistingDocument(content)
	unchanged, err := pipackage.ReconstructOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := persistedPiOrderEvidence(unchanged); err == nil {
		t.Fatal("capture persisted inapplicable unchanged-order evidence")
	}
}

func TestPiOrderEvidenceWireUsesStrictRecursiveRecordAdmission(t *testing.T) {
	_, observation := piEvidenceFixture(t, target.ScopeProject)
	record, err := persistedPiOrderEvidence(observation)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{
		nativeEvidenceObjectChange(t, encoded, []string{"settings_context"}, func(fields map[string]json.RawMessage) { fields["HomeRoot"] = fields["home_root"] }),
		nativeEvidenceObjectChange(t, encoded, nil, func(fields map[string]json.RawMessage) { delete(fields, "relations") }),
		nativeEvidenceObjectChange(t, encoded, nil, func(fields map[string]json.RawMessage) { fields["permutation"] = json.RawMessage(`[1,0]`) }),
		nativeEvidenceObjectChange(t, encoded, nil, func(fields map[string]json.RawMessage) { fields["relations"] = json.RawMessage(`[null]`) }),
	} {
		var actual recoveryPiOrderEvidence
		if err := json.Unmarshal(content, &actual); err == nil {
			t.Fatal("noncanonical Pi order record was admitted")
		}
	}
}

func TestPiOrderEvidenceJoinsRealImageCustodyAndSelectedRecovery(t *testing.T) {
	input, observation := piEvidenceFixture(t, target.ScopeProject)
	root, originalPath := imageFixture(t, input.Document.Content())
	expectedPath := filepath.Join(filepath.Dir(originalPath), "expected")
	expectedBytes, _ := observation.Candidate()
	if err := os.WriteFile(expectedPath, expectedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	mode := recovery.PermissionMode(0o640)
	originalImage, err := persistedDocumentImage(input.Document, &mode, "images/before")
	if err != nil {
		t.Fatal(err)
	}
	expectedImage, err := persistedDocumentImage(aggregate.ExistingDocument(expectedBytes), &mode, "images/expected")
	if err != nil {
		t.Fatal(err)
	}
	record, err := persistedPiOrderEvidence(observation)
	if err != nil {
		t.Fatal(err)
	}
	decoded := clonePiEvidence(t, record)
	images, err := loadDocumentImages(t.Context(), requireDocumentImageReference(t, originalImage), requireDocumentImageReference(t, expectedImage), root, journalTestFilesystem(), imageBudget(t), pipackage.MaximumSettingsBytes)
	if err != nil {
		t.Fatal(err)
	}
	order, err := decoded.canonical(t.Context(), images.original.document)
	if err != nil {
		t.Fatal(err)
	}
	comparisonInput := journaldocument.ComparisonInput{
		Before: images.original.document, Expected: images.expected.document,
		BeforeMode: images.original.mode, ExpectedMode: images.expected.mode,
		Limits: configdocument.Limits{MaximumBytes: pipackage.MaximumSettingsBytes, MaximumDepth: 64},
	}
	candidate, err := journaldocument.ReconstructSelected(t.Context(), comparisonInput, nil, &order)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := candidate.Comparison().RestoreBefore(t.Context(), images.expected.document, images.expected.mode)
	if err != nil || !restored.Document().Equal(input.Document) {
		t.Fatalf("actual persisted order and private images failed selected rollback: %v", err)
	}
	if err := os.WriteFile(expectedPath, input.Document.Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	wrongExpected, err := persistedDocumentImage(input.Document, &mode, "images/expected")
	if err != nil {
		t.Fatal(err)
	}
	images, err = loadDocumentImages(t.Context(), requireDocumentImageReference(t, originalImage), requireDocumentImageReference(t, wrongExpected), root, journalTestFilesystem(), imageBudget(t), pipackage.MaximumSettingsBytes)
	if err != nil {
		t.Fatal(err)
	}
	comparisonInput.Expected = images.expected.document
	bad, err := journaldocument.ReconstructSelected(t.Context(), comparisonInput, nil, &order)
	if err == nil || bad.Comparison().Validate() == nil {
		t.Fatal("a valid hashed image incorrectly substituted for the actual expected Pi order")
	}
}

func clonePiEvidence(t *testing.T, record recoveryPiOrderEvidence) recoveryPiOrderEvidence {
	t.Helper()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var result recoveryPiOrderEvidence
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
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
