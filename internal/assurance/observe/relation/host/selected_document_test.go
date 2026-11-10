package relationhost

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func selectedPiFixture(t *testing.T, root, namespace, content string) OrderObservation {
	t.Helper()
	capability, admitted := profile.Profile(target.TargetPi).ExtensionOrder(desiredextension.CarrierPiPackage, target.ScopeProject)
	if !admitted {
		t.Fatal("Pi capability unavailable")
	}
	settingsContext, err := extensiontopology.NewLocalSourceContext(filepath.Join(root, ".pi"), filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	commandContext, err := extensiontopology.NewLocalSourceContext(root, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	var relations []pipackage.ScopedRelation
	var members []hostrelation.RelationOrderMember
	for _, name := range []string{"a", "b"} {
		subject, err := topology.NewSubjectID(topology.SubjectHostRelation, namespace, name)
		if err != nil {
			t.Fatal(err)
		}
		source, err := hostrelation.NewSubjectKey("npm:" + name + "@1")
		if err != nil {
			t.Fatal(err)
		}
		managed, err := hostrelation.NewManagedInstanceKey("host-relation:v1:" + name)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hostrelation.NewExpectedRelation(source, managed)
		if err != nil {
			t.Fatal(err)
		}
		key, err := observerelation.NewCorrelationKey(subject, expected)
		if err != nil {
			t.Fatal(err)
		}
		relation, err := pipackage.NewScopedRelationWithContext(key, target.ScopeProject, commandContext)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := pipackage.HostLoadIdentityForContext("npm:"+name+"@1", target.ScopeProject, commandContext)
		if err != nil {
			t.Fatal(err)
		}
		member, err := hostrelation.NewRelationOrderMember(subject, hostrelation.HostLoadIdentity(identity))
		if err != nil {
			t.Fatal(err)
		}
		members, relations = append(members, member), append(relations, relation)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(capability.ClassID(), capability.MemberIdentityContract(), capability.RuntimeMeaning(), members)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := pipackage.ReconstructOrder(pipackage.LoadedOrderInput{Scope: target.ScopeProject, Capability: capability, SettingsContext: settingsContext, Document: aggregate.ExistingDocument([]byte(content)), Constraint: constraint, Relations: relations})
	if err != nil {
		t.Fatal(err)
	}
	return OrderObservation{pi: &observation}
}

func selectedNativeProjection(t *testing.T, original aggregate.Document, name, desired string) compose.ProjectionEdit {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	id, found := aggregate.HostConfigCodecForSyntax("json")
	if !found {
		t.Fatal("JSON codec unavailable")
	}
	codec, err := nativecodec.New(t.Context(), id, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	path, err := output.NewKeyPath(name)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(path)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(".pi/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, destination, footprint, codec.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	valueDocument, err := configdocument.Parse(t.Context(), []byte(`{"value":`+desired+`}`), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	valuePath, err := output.NewKeyPath("value")
	if err != nil {
		t.Fatal(err)
	}
	value, present, err := valueDocument.Lookup(valuePath)
	if err != nil || !present {
		t.Fatal("fixture has no desired native value", err)
	}
	canonical, err := value.CanonicalContext(t.Context(), limits)
	if err != nil {
		t.Fatal(err)
	}
	body, err := aggregate.NewManagedContribution(contract, canonical)
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(declaration, target.TargetPi, target.ScopeProject, path)
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{contribution})
	if err != nil {
		t.Fatal(err)
	}
	before, failure := codec.Read(original, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	intent, err := aggregate.NewProjectionIntent(before.States()[0], &set)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, configdocument.JSON, []output.KeyPath{path})
	if err != nil {
		t.Fatal(err)
	}
	return edit
}

func TestSelectedDocumentFactoryCompletesCompositionBeforeEveryVerifier(t *testing.T) {
	root := t.TempDir()
	text := `{"packages":[{"source":"npm:b@1","autoload":false},"npm:foreign@1","npm:a@1"],"model":"slow"}`
	observation := selectedPiFixture(t, root, "pi.package-carrier", text)
	edits, err := observation.SelectedEdits(nil)
	if err != nil || len(edits) != 1 || !edits[0].Changed() {
		t.Fatal("normalizer did not produce its selected edit", err)
	}
	before := edits[0].Original()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	projection := selectedNativeProjection(t, before, "model", `"fast"`)
	completed, err := CompleteSelectedImage(t.Context(), before, limits, []compose.ProjectionEdit{projection}, edits)
	if err != nil || completed.Validate() != nil || len(completed.Sequences()) != 1 || len(completed.Participants()) != 1 {
		t.Fatal("complete selection did not cross the verified boundary", err)
	}
	want := `{"packages":["npm:a@1","npm:foreign@1",{"source":"npm:b@1","autoload":false}],"model":"fast"}`
	if strings.TrimSpace(string(completed.Expected().Content())) != want || !completed.Original().Equal(before) {
		t.Fatal("completion changed a tuple, foreign slot or unrelated composition", string(completed.Expected().Content()))
	}
	operationLimits := configdocument.Limits{MaximumBytes: 2 * pipackage.MaximumSettingsBytes, MaximumDepth: 32}
	operationImage, err := CompleteSelectedImage(t.Context(), before, operationLimits, []compose.ProjectionEdit{projection}, edits)
	if err != nil || operationImage.Limits() != operationLimits || !operationImage.Expected().Equal(completed.Expected()) {
		t.Fatal("private host limits replaced the containing operation contract", err)
	}
	if _, err := CompleteSelectedImage(t.Context(), before, configdocument.Limits{MaximumBytes: before.ContentSize() - 1, MaximumDepth: 32}, nil, edits); err == nil {
		t.Fatal("private host limits replaced a stricter operation bound")
	}
	sequences := completed.Sequences()
	sequences[0] = CompletedSequence{}
	if completed.Sequences()[0].ArrayEdit().ArrayPath().IsRoot() {
		t.Fatal("completed witnesses exposed mutable backing")
	}
	other := selectedPiFixture(t, root, "other.package-correlation", text)
	otherEdits, err := other.SelectedEdits(nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongOrigin := edits[0]
	wrongOrigin.selected = otherEdits[0].selected
	if rejected, err := CompleteSelectedImage(t.Context(), before, limits, nil, []SelectedDocumentEdit{edits[0], wrongOrigin}); err == nil || rejected.Validate() == nil {
		t.Fatal("a second semantic verifier was dropped after positional coalescing")
	}
	changedOriginal := selectedPiFixture(t, root, "pi.package-carrier", strings.Replace(text, `"slow"`, `"different"`, 1))
	fresh, err := changedOriginal.SelectedEdits(nil)
	if err != nil || !edits[0].SameIntent(fresh[0]) || edits[0].SameMutation(fresh[0]) {
		t.Fatal("intent and exact mutation do not have distinct drift boundaries", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if rejected, err := CompleteSelectedImage(cancelled, before, limits, nil, edits); !errors.Is(err, context.Canceled) || rejected.Validate() == nil {
		t.Fatal("cancelled completion returned usable evidence", err)
	}
	limits.MaximumBytes = before.ContentSize() - 1
	if rejected, err := CompleteSelectedImage(t.Context(), before, limits, nil, edits); err == nil || rejected.Validate() == nil {
		t.Fatal("byte-limited completion returned usable evidence")
	}
}
