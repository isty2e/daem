package record

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNative16SelectedPreparesAndReconstructsActualNativeAndPiEdits(t *testing.T) {
	mode := recovery.PermissionMode(0o640)
	before := aggregate.ExistingDocument([]byte(`{"model":"before","packages":["npm:b", "npm:foreign", "npm:a"],"outside":true}`))
	order := candidateOrder(t, before)
	edit := candidateNativeEdit(t, before, "model", `"after"`)
	input := document.SelectedPreparationInput{
		Before: before, BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}
	prepared, err := prepareNative16Selected(t, t.Context(), input, []compose.ProjectionEdit{edit}, &order)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(prepared.Document().Content(), []byte(`"model":"after"`)) {
		t.Fatal("combined candidate omitted the actual native edit")
	}
	captured, err := order.CaptureInput()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	replayed, err := pipackage.ReconstructOrder(captured)
	if err != nil {
		t.Fatal(err)
	}
	reconstructed, err := ReconstructNative16Selected(t.Context(), document.ComparisonInput{
		Before: before, Expected: prepared.Document(), BeforeMode: &mode, ExpectedMode: &mode, Limits: input.Limits,
	}, []compose.Participant{edit.Participant()}, &replayed)
	if err != nil {
		t.Fatal(err)
	}
	orderOnly, err := order.Project(before)
	if err != nil {
		t.Fatal(err)
	}
	nativeOnly, err := compose.Render(t.Context(), before, input.Limits, []compose.ProjectionEdit{edit})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := order.Project(nativeOnly.Document())
	if err != nil || !legacy.Equal(prepared.Document()) {
		t.Fatal("common completion changed the legacy combined candidate bytes", err)
	}
	for _, mixed := range []aggregate.Document{orderOnly, nativeOnly.Document(), prepared.Document()} {
		restored, err := reconstructed.Comparison().RestoreBefore(t.Context(), mixed, &mode)
		if err != nil {
			t.Fatal(err)
		}
		match, err := reconstructed.Comparison().Match(t.Context(), restored.Document(), &mode)
		if err != nil || !match.MatchesBefore() {
			t.Fatalf("mixed native/Pi phase did not restore: %+v, %v", match, err)
		}
		again, err := reconstructed.Comparison().RestoreBefore(t.Context(), restored.Document(), &mode)
		if err != nil || again.Changed() {
			t.Fatalf("restoration retry did not converge: %v", err)
		}
	}
	for _, changed := range [][]byte{
		bytes.Replace(prepared.Document().Content(), []byte(`"outside":true`), []byte(`"outside":false`), 1),
		bytes.Replace(prepared.Document().Content(), []byte("npm:foreign"), []byte("npm:third"), 1),
		nativeOnly.Document().Content(),
	} {
		bad, err := ReconstructNative16Selected(t.Context(), document.ComparisonInput{
			Before: before, Expected: aggregate.ExistingDocument(changed), BeforeMode: &mode, ExpectedMode: &mode, Limits: input.Limits,
		}, []compose.Participant{edit.Participant()}, &replayed)
		if err == nil || bad.Comparison().Validate() == nil {
			t.Fatal("invalid expected image returned a usable comparison")
		}
	}
}

func TestNative16SelectedRefusesLateFailureAndKeepsNoopOrderUnowned(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before := aggregate.ExistingDocument([]byte(`{"model":"before","packages":[]}`))
	order := candidateOrder(t, before)
	edit := candidateNativeEdit(t, before, "model", `"after"`)
	input := document.SelectedPreparationInput{
		Before: before, BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}
	prepared, err := prepareNative16Selected(t, t.Context(), input, []compose.ProjectionEdit{edit}, &order)
	if err != nil {
		t.Fatal(err)
	}
	current := aggregate.ExistingDocument([]byte(`{"model":"after","packages":["npm:foreign"]}`))
	restored, err := prepared.Comparison().RestoreBefore(t.Context(), current, &mode)
	if err != nil {
		t.Fatalf("unchanged order became recovery-owned work: %v", err)
	}
	assertNative16SelectedValue(t, restored.Document(), "model", `"before"`)
	assertNative16SelectedValue(t, restored.Document(), "packages", `["npm:foreign"]`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	bad, err := prepareNative16Selected(t, ctx, input, []compose.ProjectionEdit{edit}, &order)
	if !errors.Is(err, context.Canceled) || bad.Comparison().Validate() == nil {
		t.Fatalf("canceled preparation returned authority: %v", err)
	}
	otherOrder := candidateOrder(t, aggregate.ExistingDocument([]byte(`{"packages":[],"model":"different"}`)))
	bad, err = prepareNative16Selected(t, t.Context(), input, []compose.ProjectionEdit{edit}, &otherOrder)
	if err == nil || bad.Comparison().Validate() == nil {
		t.Fatal("later mismatched Pi baseline returned a partial native candidate")
	}
	input.Limits.MaximumBytes = 4
	bad, err = prepareNative16Selected(t, t.Context(), input, []compose.ProjectionEdit{edit}, &order)
	if err == nil || bad.Comparison().Validate() == nil {
		t.Fatal("over-budget input returned a comparison")
	}
}

func TestNative16SelectedKeepsOrderOnlyAndAbsentNoopsDistinct(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	for _, before := range []aggregate.Document{
		aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{}`)),
		aggregate.ExistingDocument([]byte(`{"packages":[]}`)),
		aggregate.ExistingDocument([]byte(`{"packages":["npm:b","npm:a"],"outside":true}`)),
	} {
		order := candidateOrder(t, before)
		var selectedMode *recovery.PermissionMode
		if before.Exists() {
			selectedMode = &mode
		}
		prepared, err := prepareNative16Selected(t, t.Context(), document.SelectedPreparationInput{
			Before: before, BeforeMode: selectedMode, ExpectedMode: selectedMode,
			Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
		}, nil, &order)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Comparison().Changed() != order.Changed() {
			t.Fatal("order recovery selection differs from actual work")
		}
		if !order.Changed() && !prepared.Document().Equal(before) {
			t.Fatal("unchanged order created a file or array")
		}
	}
}

func TestNative16SelectedRejectsPiExpectedLimitBeforeCopying(t *testing.T) {
	before := aggregate.ExistingDocument([]byte(`{"packages":["npm:b","npm:a"]}`))
	order := candidateOrder(t, before)
	mode := recovery.PermissionMode(0o600)
	input := document.ComparisonInput{
		Before: before, Expected: aggregate.ExistingDocument(bytes.Repeat([]byte(" "), 2*pipackage.MaximumSettingsBytes)),
		BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 2 * pipackage.MaximumSettingsBytes, MaximumDepth: 32},
	}
	read := func() error {
		_, err := ReconstructNative16Selected(t.Context(), input, nil, &order)
		return err
	}
	if err := read(); err == nil {
		t.Fatal("expected Pi image exceeded its consumer limit")
	}
	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if err := read(); err == nil {
				b.Fatal("oversized expected Pi image was accepted")
			}
		}
	})
	allocated := result.AllocedBytesPerOp()
	t.Logf("oversized expected Pi rejection: %d B/op", allocated)
	if result.N == 0 || allocated >= pipackage.MaximumSettingsBytes {
		t.Fatalf("Pi expected rejection allocated %d B/op before refusing", allocated)
	}
}

func TestNative16SelectedVerifiesCompletedImageEvenForUnchangedOrder(t *testing.T) {
	before := aggregate.ExistingDocument([]byte(`{"model":"before","packages":[]}`))
	order := candidateOrder(t, before)
	edit := candidateNativeEdit(t, before, "packages", `["npm:unexpected"]`)
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	expected, err := compose.Render(t.Context(), before, limits, []compose.ProjectionEdit{edit})
	if err != nil || order.Changed() {
		t.Fatal("fixture did not establish a semantic edit with unchanged order", err)
	}
	mode := recovery.PermissionMode(0o600)
	t.Run("preparation", func(t *testing.T) {
		if rejected, participants, err := PrepareNative16SelectedImage(t.Context(), before, limits, []compose.ProjectionEdit{edit}, &order); err == nil || rejected.Exists() || rejected.ContentSize() != 0 || participants != nil {
			t.Fatal("preparation lost the final private Pi semantic check", err)
		}
	})
	t.Run("replay", func(t *testing.T) {
		if rejected, err := ReconstructNative16Selected(t.Context(), document.ComparisonInput{
			Before: before, Expected: expected.Document(), BeforeMode: &mode, ExpectedMode: &mode, Limits: limits,
		}, []compose.Participant{edit.Participant()}, &order); err == nil || rejected.Comparison().Validate() == nil {
			t.Fatal("legacy replay lost its private Pi semantic check", err)
		}
	})
}

func prepareNative16Selected(t *testing.T, ctx context.Context, input document.SelectedPreparationInput, edits []compose.ProjectionEdit, order *pipackage.OrderObservation) (document.SelectedCandidate, error) {
	t.Helper()
	expected, participants, err := PrepareNative16SelectedImage(ctx, input.Before, input.Limits, edits, order)
	if err != nil {
		return document.SelectedCandidate{}, err
	}
	return ReconstructNative16Selected(ctx, document.ComparisonInput{
		Before: input.Before, Expected: expected,
		BeforeMode: input.BeforeMode, ExpectedMode: input.ExpectedMode, Limits: input.Limits,
	}, participants, order)
}

func assertNative16SelectedValue(t *testing.T, image aggregate.Document, field, want string) {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	parsed, err := configdocument.Parse(t.Context(), image.Content(), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	path, err := output.NewKeyPath(field)
	if err != nil {
		t.Fatal(err)
	}
	value, present, err := parsed.Lookup(path)
	if err != nil || !present {
		t.Fatal("expected selected value is absent", err)
	}
	canonical, err := value.CanonicalContext(t.Context(), limits)
	if err != nil || canonical != nativeRecordCanonical(t, want) {
		t.Fatal("selected value changed", field, err)
	}
}

func candidateNativeEdit(t *testing.T, before aggregate.Document, name, desired string) compose.ProjectionEdit {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	id, found := aggregate.HostConfigCodecForSyntax(configdocument.JSON.String())
	if !found {
		t.Fatal("fixture native grammar unavailable")
	}
	codec, err := nativecodec.New(t.Context(), id, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath(name)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
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
	body, err := aggregate.NewManagedContribution(contract, nativeRecordCanonical(t, desired))
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := entity.New(entity.KindHostConfig, "preferences")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(entityID, target.TargetPi, target.ScopeProject, contract.Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	baseline, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	intent, err := aggregate.NewProjectionIntent(baseline.States()[0], &set)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(baseline, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, configdocument.JSON, []output.KeyPath{contract.Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	return edit
}

func candidateOrder(t *testing.T, document aggregate.Document) pipackage.OrderObservation {
	t.Helper()
	return candidateOrderForScope(t, document, target.ScopeProject)
}

func candidateOrderForScope(t *testing.T, document aggregate.Document, scope target.Scope) pipackage.OrderObservation {
	t.Helper()
	root := t.TempDir()
	local, err := extensiontopology.NewLocalSourceContext(root, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	var relations []pipackage.ScopedRelation
	var members []hostrelation.RelationOrderMember
	for _, name := range []string{"a", "b"} {
		subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "pi.package-carrier", name)
		if err != nil {
			t.Fatal(err)
		}
		source, err := hostrelation.NewSubjectKey("npm:" + name)
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
		relation, err := pipackage.NewScopedRelationWithContext(key, scope, local)
		if err != nil {
			t.Fatal(err)
		}
		relations = append(relations, relation)
		identity, err := pipackage.HostLoadIdentityForContext("npm:"+name, scope, local)
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
	capability, err := native16PiOrderCapability(scope)
	if err != nil {
		t.Fatal(err)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(capability.ClassID(), capability.MemberIdentityContract(), capability.RuntimeMeaning(), members)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := pipackage.ReconstructOrder(pipackage.LoadedOrderInput{
		Scope: scope, Capability: capability, SettingsContext: local, Document: document, Constraint: constraint, Relations: relations,
	})
	if err != nil {
		t.Fatal(err)
	}
	return observation
}
