package document_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestSelectedCandidatePreparesAndReconstructsActualNativeAndPiEdits(t *testing.T) {
	mode := recovery.PermissionMode(0o640)
	before := aggregate.ExistingDocument([]byte(`{"model":"before","packages":["npm:b", "npm:foreign", "npm:a"],"outside":true}`))
	order := candidateOrder(t, before)
	edit := candidateNativeEdit(t, before, "model", `"after"`)
	input := document.SelectedPreparationInput{
		Before: before, BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}
	prepared, err := document.PrepareSelected(t.Context(), input, []compose.ProjectionEdit{edit}, &order)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(prepared.Document().Content(), []byte(`"model":"after"`)) {
		t.Fatal("combined candidate omitted the actual native edit")
	}
	captured, present, err := prepared.OrderInput()
	if err != nil || !present || !captured.Document.Equal(before) {
		t.Fatalf("changed order lost its actual replay input: %v", err)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	replayed, err := pipackage.ReconstructOrder(captured)
	if err != nil {
		t.Fatal(err)
	}
	reconstructed, err := document.ReconstructSelected(t.Context(), document.ComparisonInput{
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
		bad, err := document.ReconstructSelected(t.Context(), document.ComparisonInput{
			Before: before, Expected: aggregate.ExistingDocument(changed), BeforeMode: &mode, ExpectedMode: &mode, Limits: input.Limits,
		}, []compose.Participant{edit.Participant()}, &replayed)
		if err == nil || bad.Comparison().Validate() == nil {
			t.Fatal("invalid expected image returned a usable comparison")
		}
	}
}

func TestSelectedCandidateRefusesLateFailureAndKeepsNoopOrderUnowned(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before := aggregate.ExistingDocument([]byte(`{"model":"before","packages":[]}`))
	order := candidateOrder(t, before)
	edit := candidateNativeEdit(t, before, "model", `"after"`)
	input := document.SelectedPreparationInput{
		Before: before, BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}
	prepared, err := document.PrepareSelected(t.Context(), input, []compose.ProjectionEdit{edit}, &order)
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := prepared.OrderInput(); err != nil || present {
		t.Fatalf("unchanged order became recovery-owned work: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	bad, err := document.PrepareSelected(ctx, input, []compose.ProjectionEdit{edit}, &order)
	if err != context.Canceled || bad.Comparison().Validate() == nil {
		t.Fatalf("canceled preparation returned authority: %v", err)
	}
	otherOrder := candidateOrder(t, aggregate.ExistingDocument([]byte(`{"packages":[],"model":"different"}`)))
	bad, err = document.PrepareSelected(t.Context(), input, []compose.ProjectionEdit{edit}, &otherOrder)
	if err == nil || bad.Comparison().Validate() == nil {
		t.Fatal("later mismatched Pi baseline returned a partial native candidate")
	}
	input.Limits.MaximumBytes = 4
	bad, err = document.PrepareSelected(t.Context(), input, []compose.ProjectionEdit{edit}, &order)
	if err == nil || bad.Comparison().Validate() == nil {
		t.Fatal("over-budget input returned a comparison")
	}
}

func TestSelectedCandidateReconstructsNativeMCPWithoutAuthorizingForeignRows(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before := "# keep\nmodel='before'\n[mcp_servers.foreign]\ncommand='keep'\n"
	expected := "# keep\nmodel='after'\n[mcp_servers.foreign]\ncommand='keep'\n[mcp_servers.search]\ncommand='node'\n"
	participants := restorationMCPParticipants(t)
	prepared, err := document.ReconstructSelected(t.Context(), comparisonInput(before, expected, &mode, &mode), participants, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{
		"# keep\nmodel='before'\n[mcp_servers.foreign]\ncommand='keep'\n[mcp_servers.search]\ncommand='node'\n",
		"# keep\nmodel='after'\n[mcp_servers.foreign]\ncommand='keep'\n",
	} {
		restored, err := prepared.Comparison().RestoreBefore(t.Context(), aggregate.ExistingDocument([]byte(current)), &mode)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := configdocument.Parse(t.Context(), restored.Document().Content(), configdocument.TOML, comparisonInput(before, expected, &mode, &mode).Limits)
		if err != nil {
			t.Fatal(err)
		}
		key, err := output.NewKeyPath("model")
		if err != nil {
			t.Fatal(err)
		}
		value, present, err := parsed.Lookup(key)
		if err != nil || !present || !value.Equal(expectedRestorationValue(t, `"before"`)) {
			t.Fatalf("native/MCP mixture failed to restore the actual native value: %v", err)
		}
	}
	bad, err := document.ReconstructSelected(t.Context(), comparisonInput(before, expected+"outside=true\n", &mode, &mode), participants, nil)
	if err == nil || bad.Comparison().Validate() == nil {
		t.Fatal("unexpected MCP content returned a usable comparison")
	}
}

func TestSelectedCandidateKeepsOrderOnlyAndAbsentNoopsDistinct(t *testing.T) {
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
		prepared, err := document.PrepareSelected(t.Context(), document.SelectedPreparationInput{
			Before: before, BeforeMode: selectedMode, ExpectedMode: selectedMode,
			Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
		}, nil, &order)
		if err != nil {
			t.Fatal(err)
		}
		if _, present, err := prepared.OrderInput(); err != nil || present != order.Changed() {
			t.Fatalf("order recovery selection differs from actual work: %v", err)
		}
		if !order.Changed() && !prepared.Document().Equal(before) {
			t.Fatal("unchanged order created a file or array")
		}
	}
}

func TestSelectedCandidateRejectsPiExpectedLimitBeforeCopying(t *testing.T) {
	before := aggregate.ExistingDocument([]byte(`{"packages":["npm:b","npm:a"]}`))
	order := candidateOrder(t, before)
	mode := recovery.PermissionMode(0o600)
	input := document.ComparisonInput{
		Before: before, Expected: aggregate.ExistingDocument(bytes.Repeat([]byte(" "), 2*pipackage.MaximumSettingsBytes)),
		BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 2 * pipackage.MaximumSettingsBytes, MaximumDepth: 32},
	}
	read := func() error {
		_, err := document.ReconstructSelected(t.Context(), input, nil, &order)
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

func candidateNativeEdit(t *testing.T, before aggregate.Document, name, desired string) compose.ProjectionEdit {
	t.Helper()
	participant := comparisonParticipant(t, name)
	codec := comparisonNativeCodec(t, configdocument.JSON)
	contract := participant.Selection().Contracts()[0]
	canonical, err := expectedRestorationValue(t, desired).CanonicalContext(t.Context(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	body, err := aggregate.NewManagedContribution(contract, canonical)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "preferences")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeProject, contract.Address().KeyPath())
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
	baseline, failure := codec.Read(before, participant.Selection())
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
	capability, ok := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetPi, scope, desiredextension.CarrierPiPackage)
	if !ok {
		t.Fatal("Pi order capability unavailable")
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
