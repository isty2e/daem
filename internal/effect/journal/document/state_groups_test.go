package document

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestDocumentStateGroupsReconstructAndRestoreTheCompleteSelectedGroup(t *testing.T) {
	fixture := documentClaimsForTest(t)
	before := membershipNativeRow(t, fixture.alias, "native-json-v1", "model")
	after := selectionNativeRow(t, before.Contribution().Contract(), `"fast"`)
	outside := membershipNativeRow(t, fixture.alias, "native-json-v1", "outside")
	input := stateGroupInput(t, fixture, []durable.ManagedAggregateState{after}, `{"model":true,"outside":true}`, `{"model":"fast","outside":true}`)
	groups, err := ReconstructStateGroups(t.Context(), fixture.owner, selectionState(t, before, outside), selectionState(t, after, outside), []StateGroupInput{input})
	if err != nil || len(groups) != 1 {
		t.Fatalf("reconstruct selected operation: groups=%d error=%v", len(groups), err)
	}
	mode := recovery.NewPermissionMode(0o600)
	restored, err := groups[0].Comparison().RestoreBefore(t.Context(), aggregate.ExistingDocument([]byte(`{"model":"fast","outside":99}`)), mode)
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		Model   bool `json:"model"`
		Outside int  `json:"outside"`
	}
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || !values.Model || values.Outside != 99 {
		t.Fatalf("reconstructed recovery lost selected or fresh foreign data: %+v, %v", values, err)
	}
	input.aliases[0] = aggregate.DocumentAddress{}
	input.bindingIntents[0] = ownership.DocumentBindingIntent{}
	if err := groups[0].Aliases()[0].Validate(); err != nil || groups[0].bindingIntents[0].Validate() != nil {
		t.Fatal("caller slice mutation changed the reconstructed facts")
	}
}

func TestDocumentStateGroupsRequireEveryChangedSideButNotUntouchedFiles(t *testing.T) {
	fixture := documentClaimsForTest(t)
	old := selectionNativeRow(t, piSelectionContract(t, ".pi/old.json", "model"), "1")
	updated := selectionNativeRow(t, piSelectionContract(t, ".pi/new.json", "model"), "2")
	untouched := selectionNativeRow(t, piSelectionContract(t, ".pi/untouched.json", "unrelated"), "true")
	oldFixture, newFixture := fixture, fixture
	oldFixture.alias = old.Contribution().Contract().Address().Document()
	newFixture.alias = updated.Contribution().Contract().Address().Document()
	oldInput := stateGroupInput(t, oldFixture, []durable.ManagedAggregateState{old}, `{"model":1}`, `{}`)
	newInput := stateGroupInput(t, newFixture, []durable.ManagedAggregateState{updated}, `{}`, `{"model":2}`)
	oldValue, newValue := membershipBindingValue(t, oldFixture), membershipBindingValue(t, newFixture)
	oldInput.bindingIntents = []ownership.DocumentBindingIntent{membershipIntent(t, oldFixture, oldValue, ownership.NoDocumentBinding())}
	newInput.bindingIntents = []ownership.DocumentBindingIntent{membershipIntent(t, newFixture, ownership.NoDocumentBinding(), newValue)}
	before, after := selectionState(t, old, untouched), selectionState(t, updated, untouched)
	inputs := []StateGroupInput{oldInput, newInput}
	for range 2 {
		groups, err := ReconstructStateGroups(t.Context(), fixture.owner, before, after, inputs)
		if err != nil || len(groups) != 2 {
			t.Fatalf("explicit retire/create with untouched state: groups=%d error=%v", len(groups), err)
		}
		slices.Reverse(inputs)
	}
	for _, missing := range [][]StateGroupInput{{oldInput}, {newInput}} {
		if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, before, after, missing); err == nil || groups != nil {
			t.Fatal("destination change lost one state side or returned a partial prefix")
		}
	}
}

func TestDocumentStateGroupsRequireChangedAfterKeysWithinTheActualFrontier(t *testing.T) {
	fixture := documentClaimsForTest(t)
	parent := membershipNativeRow(t, fixture.alias, "native-json-v1", "node")
	child := selectionNativeRow(t, stateGroupContract(t, fixture.alias, "node", "leaf"), "2")
	for _, test := range []struct {
		before, after durable.ManagedAggregateState
		beforeBytes   string
		afterBytes    string
	}{
		{parent, child, `{"node":true}`, `{"node":{"leaf":2}}`},
		{child, parent, `{"node":{"leaf":2}}`, `{"node":true}`},
	} {
		input := stateGroupInput(t, fixture, []durable.ManagedAggregateState{parent}, test.beforeBytes, test.afterBytes)
		input.selectedSubjects = []topology.SubjectID{test.before.Subject(), test.after.Subject()}
		groups, err := ReconstructStateGroups(t.Context(), fixture.owner, selectionState(t, test.before), selectionState(t, test.after), []StateGroupInput{input})
		if err != nil || len(groups) != 1 || len(groups[0].Selections()[0].Contracts()) != 1 || !groups[0].Selections()[0].Contracts()[0].Equal(parent.Contribution().Contract()) {
			t.Fatalf("actual selected parent did not cover its changed descendants: %v", err)
		}
	}

	literal := selectionNativeRow(t, stateGroupContract(t, fixture.alias, "node/leaf"), "2")
	input := stateGroupInput(t, fixture, []durable.ManagedAggregateState{parent}, `{"node":true}`, `{"node":true,"node/leaf":2}`)
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, selectionState(t, parent), selectionState(t, parent, literal), []StateGroupInput{input}); err == nil || groups != nil {
		t.Fatal("literal slash key borrowed a structural parent's frontier")
	}
	input = stateGroupInput(t, fixture, nil, `{"node":true}`, `{"node":{"leaf":2}}`)
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, selectionState(t, parent), selectionState(t, child), []StateGroupInput{input}); err == nil || groups != nil {
		t.Fatal("empty frontier covered a newly managed key")
	}
}

func TestDocumentStateGroupsKeepNarrowingAndReleaseSeparateFromWholeRestoration(t *testing.T) {
	fixture := documentClaimsForTest(t)
	whole := nativeWholeFileJournalBaseline(t, target.TargetPi, target.ScopeGlobal)
	retained := selectionNativeRow(t, stateGroupContract(t, stateGroupWholeAlias(t, whole), "model"), `"fast"`)
	added := selectionNativeRow(t, stateGroupContract(t, stateGroupWholeAlias(t, whole), "added"), "true")
	fixture.alias = retained.Contribution().Contract().Address().Document()
	wholeState, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{whole}})
	if err != nil {
		t.Fatal(err)
	}
	keyedState := selectionState(t, retained, added)
	beforeBytes, afterBytes := `{"model":"slow","released":1}`, `{"model":"fast","added":true,"released":1}`
	narrow := stateGroupInput(t, fixture, []durable.ManagedAggregateState{retained, added}, beforeBytes, afterBytes)
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, wholeState, keyedState, []StateGroupInput{narrow}); err != nil || len(groups) != 1 {
		t.Fatalf("narrowing with changed and new retained keys: %v", err)
	}
	wholeInput := narrow
	wholeInput.selectedSubjects = nil
	wholeInput.comparison = stateGroupWholeComparison(t, beforeBytes, afterBytes)
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, wholeState, keyedState, []StateGroupInput{wholeInput}); err == nil || groups != nil {
		t.Fatal("whole comparison retained authority over released narrowing areas")
	}

	widen := wholeInput
	widen.comparison = stateGroupWholeComparison(t, afterBytes, afterBytes)
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, keyedState, wholeState, []StateGroupInput{widen}); err != nil || len(groups) != 1 {
		t.Fatalf("record-only widening state reconstruction: %v", err)
	}
	widen.comparison = stateGroupComparison(t, afterBytes, afterBytes)
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, keyedState, wholeState, []StateGroupInput{widen}); err == nil || groups != nil {
		t.Fatal("selected comparison claimed a whole-file after baseline")
	}

	for _, before := range []durable.Snapshot{wholeState, keyedState} {
		release := stateGroupInput(t, fixture, nil, afterBytes, afterBytes)
		value := membershipBindingValue(t, fixture)
		release.bindingIntents = []ownership.DocumentBindingIntent{membershipIntent(t, fixture, value, ownership.NoDocumentBinding())}
		if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, before, durable.EmptySnapshot(), []StateGroupInput{release}); err != nil || len(groups) != 1 {
			t.Fatalf("pure release selected the old owned area or lost its group: %v", err)
		}
		if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, before, durable.EmptySnapshot(), nil); err == nil || groups != nil {
			t.Fatal("last native retirement disappeared from operation coverage")
		}
	}
}

func TestDocumentStateGroupsCorrelateMixedNativeAndMCPComparisons(t *testing.T) {
	fixture := documentClaimsForTest(t)
	placement, present := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !present {
		t.Fatal("Codex project MCP placement unavailable")
	}
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{
		ServerID: "search", Command: "node", AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution("search", string(canonical))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, "search")
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	fixture.alias = contribution.Contract().Address().Document()
	native := membershipNativeRow(t, fixture.alias, "native-toml-v1", "model")
	updated := selectionNativeRow(t, native.Contribution().Contract(), `"fast"`)
	before, after := "model=true\n[mcp_servers.search]\ncommand='node'\n", "model='fast'\n[mcp_servers.search]\ncommand='node'\n"
	input := stateGroupInput(t, fixture, []durable.ManagedAggregateState{updated, mcp}, before, after)
	groups, err := ReconstructStateGroups(t.Context(), fixture.owner, selectionState(t, native, mcp), selectionState(t, updated, mcp), []StateGroupInput{input})
	if err != nil || len(groups) != 1 || len(groups[0].Selections()) != 2 {
		t.Fatalf("independent codec contracts did not assemble at one logical file: %v", err)
	}
	mode := recovery.NewPermissionMode(0o600)
	match, err := groups[0].Comparison().Match(t.Context(), aggregate.ExistingDocument([]byte(after)), mode)
	if err != nil || !match.MatchesExpected() || match.MatchesBefore() || !match.Recoverable() {
		t.Fatalf("reconstructed mixed comparison lost its semantic phase: %+v, %v", match, err)
	}
}

func TestDocumentStateGroupsRejectMisjoinedComparisonsAndBindingsWithoutPartialOutput(t *testing.T) {
	fixture := documentClaimsForTest(t)
	first := membershipNativeRow(t, fixture.alias, "native-json-v1", "first")
	second := membershipNativeRow(t, fixture.alias, "native-json-v1", "second")
	input := stateGroupInput(t, fixture, []durable.ManagedAggregateState{first, second}, `{"first":true,"second":true}`, `{"first":true,"second":true}`)
	state := selectionState(t, first, second)
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, []StateGroupInput{input}); err != nil || len(groups) != 1 {
		t.Fatalf("split disjoint participants were mistaken for a frontier mismatch: %v", err)
	}
	missing := input
	missing.comparison = stateGroupComparison(t, `{"first":true,"second":true}`, `{"first":true,"second":true}`, []aggregate.ProjectionContract{first.Contribution().Contract()})
	extra := input
	extra.selectedSubjects = []topology.SubjectID{first.Subject()}
	whole := input
	whole.comparison = stateGroupWholeComparison(t, `{}`, `{}`)
	invalid := input
	invalid.comparison = Comparison{}
	for _, bad := range []StateGroupInput{missing, extra, whole, invalid} {
		if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, []StateGroupInput{bad}); err == nil || groups != nil {
			t.Fatal("mismatched comparison returned a usable state group")
		}
	}
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, []StateGroupInput{input, input}); err == nil || groups != nil {
		t.Fatal("duplicate alias returned an ambiguous or partially usable operation")
	}
	whole.selectedSubjects = nil
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, []StateGroupInput{whole}); err == nil || groups != nil {
		t.Fatal("key-only state authorized a whole comparison")
	}

	otherFixture := fixture
	other := selectionNativeRow(t, piSelectionContract(t, ".pi/other.json", "other"), "true")
	otherFixture.alias = other.Contribution().Contract().Address().Document()
	otherInput := stateGroupInput(t, otherFixture, []durable.ManagedAggregateState{other}, `{"other":true}`, `{"other":true}`)
	input.bindingIntents = append(input.bindingIntents, otherInput.bindingIntents...)
	otherInput.bindingIntents = nil
	state = selectionState(t, first, second, other)
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, state, state, []StateGroupInput{input, otherInput}); err == nil || groups != nil {
		t.Fatal("an intent assigned to another group's alias passed the operation-wide join")
	}
}

func TestDocumentStateGroupsRejectWrongCodecAndAcceptOrdinaryOnlyOperations(t *testing.T) {
	fixture := documentClaimsForTest(t)
	row := membershipNativeRow(t, fixture.alias, "native-json-v1", "model")
	input := stateGroupInput(t, fixture, []durable.ManagedAggregateState{row}, `{"model":true}`, `{"model":true}`)
	otherContract, err := aggregate.NewHostConfigProjectionContract(fixture.alias.Target(), fixture.alias.Scope(), fixture.alias.AggregateRoot(), row.Contribution().Contract().Address().Footprint(), "native-jsonc-v1")
	if err != nil {
		t.Fatal(err)
	}
	input.comparison = stateGroupComparison(t, `{"model":true}`, `{"model":true}`, []aggregate.ProjectionContract{otherContract})
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, selectionState(t, row), selectionState(t, row), []StateGroupInput{input}); err == nil || groups != nil {
		t.Fatal("comparison codec borrowed an otherwise identical native frontier")
	}
	if groups, err := ReconstructStateGroups(t.Context(), fixture.owner, durable.EmptySnapshot(), durable.EmptySnapshot(), nil); err != nil || len(groups) != 0 {
		t.Fatalf("ordinary-only envelope required synthetic native groups: %v", err)
	}
	if groups, err := ReconstructStateGroups(t.Context(), stateauthority.Authority{}, durable.EmptySnapshot(), durable.EmptySnapshot(), nil); err == nil || groups != nil {
		t.Fatal("invalid operation owner returned successful empty reconstruction")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if groups, err := ReconstructStateGroups(ctx, fixture.owner, selectionState(t, row), selectionState(t, row), []StateGroupInput{input}); !errors.Is(err, context.Canceled) || groups != nil {
		t.Fatalf("canceled reconstruction returned a prefix: %v", err)
	}
	if groups, err := ReconstructStateGroups(nil, fixture.owner, durable.EmptySnapshot(), durable.EmptySnapshot(), nil); err == nil || groups != nil {
		t.Fatal("nil context returned usable reconstruction")
	}
}

func TestDocumentStateGroupsKeepPiOrderSeparateFromNativeMembership(t *testing.T) {
	fixture := documentClaimsForTest(t)
	destination, err := output.Parse("@pi-agent/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, destination, documentKeyFootprint(t, "model"), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, afterBytes := []byte(`{"packages":["a","b"]}`), []byte(`{"packages":["b","a"]}`)
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	before, err := configdocument.Parse(t.Context(), beforeBytes, configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	after, err := configdocument.Parse(t.Context(), afterBytes, configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	path, err := output.NewKeyPath("packages")
	if err != nil {
		t.Fatal(err)
	}
	order, err := configdocument.NewJSONArrayPermutation(t.Context(), before, after, path)
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.NewPermissionMode(0o600)
	comparison, err := NewSelectedComparison(t.Context(), ComparisonInput{
		Before: aggregate.ExistingDocument(beforeBytes), Expected: aggregate.ExistingDocument(afterBytes),
		BeforeMode: mode, ExpectedMode: mode, Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}, nil, []configdocument.JSONArrayEdit{order})
	if err != nil {
		t.Fatal(err)
	}
	input := StateGroupInput{aliases: []aggregate.DocumentAddress{contract.Address().Document()}, comparison: comparison}
	groups, err := ReconstructStateGroups(t.Context(), fixture.owner, durable.EmptySnapshot(), durable.EmptySnapshot(), []StateGroupInput{input})
	if err != nil || len(groups) != 1 || len(groups[0].Selections()) != 0 || len(groups[0].bindingIntents) != 0 {
		t.Fatalf("Pi-only reconstruction invented native membership: %v", err)
	}
	restored, err := groups[0].Comparison().RestoreBefore(t.Context(), aggregate.ExistingDocument(afterBytes), mode)
	if err != nil || !restored.Document().Equal(aggregate.ExistingDocument(beforeBytes)) {
		t.Fatalf("state-neutral order inverse was lost: %v", err)
	}
}

func stateGroupInput(t *testing.T, fixture documentClaimFixture, selected []durable.ManagedAggregateState, before, after string) StateGroupInput {
	t.Helper()
	var refs []topology.SubjectID
	var sets [][]aggregate.ProjectionContract
	for _, row := range selected {
		refs = append(refs, row.Subject())
		sets = append(sets, []aggregate.ProjectionContract{row.Contribution().Contract()})
	}
	value := membershipBindingValue(t, fixture)
	return StateGroupInput{
		aliases: []aggregate.DocumentAddress{fixture.alias}, selectedSubjects: refs,
		comparison:     stateGroupComparison(t, before, after, sets...),
		bindingIntents: []ownership.DocumentBindingIntent{membershipIntent(t, fixture, value, value)},
	}
}

func stateGroupComparison(t *testing.T, before, after string, sets ...[]aggregate.ProjectionContract) Comparison {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	var participants []compose.Participant
	for _, contracts := range sets {
		selection, err := aggregate.NewSelection(contracts)
		if err != nil {
			t.Fatal(err)
		}
		var codec aggregate.Codec
		syntax := configdocument.TOML
		if name, native := aggregate.HostConfigSyntaxForCodec(selection.CodecContractID()); native {
			syntax, err = configdocument.ParseSyntax(name)
			if err != nil {
				t.Fatal(err)
			}
			codec, err = nativecodec.New(t.Context(), selection.CodecContractID(), limits, limits)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			var present bool
			codec, present = mcpcodec.For(selection.CodecContractID())
			if !present {
				t.Fatal("selected fixture codec unavailable")
			}
		}
		var areas []output.KeyPath
		for _, contract := range contracts {
			areas = append(areas, contract.Address().KeyPath())
		}
		participant, err := compose.NewParticipant(codec, selection, syntax, areas)
		if err != nil {
			t.Fatal(err)
		}
		participants = append(participants, participant)
	}
	mode := recovery.NewPermissionMode(0o600)
	comparison, err := NewSelectedComparison(t.Context(), ComparisonInput{
		Before: aggregate.ExistingDocument([]byte(before)), Expected: aggregate.ExistingDocument([]byte(after)),
		BeforeMode: mode, ExpectedMode: mode, Limits: limits,
	}, participants, nil)
	if err != nil {
		t.Fatal(err)
	}
	return comparison
}

func stateGroupWholeComparison(t *testing.T, before, after string) Comparison {
	t.Helper()
	mode := recovery.NewPermissionMode(0o600)
	comparison, err := NewWholeComparison(t.Context(), ComparisonInput{
		Before: aggregate.ExistingDocument([]byte(before)), Expected: aggregate.ExistingDocument([]byte(after)),
		BeforeMode: mode, ExpectedMode: mode, Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	return comparison
}

func stateGroupContract(t *testing.T, alias aggregate.DocumentAddress, parts ...string) aggregate.ProjectionContract {
	t.Helper()
	contract, err := aggregate.NewHostConfigProjectionContract(alias.Target(), alias.Scope(), alias.AggregateRoot(), documentKeyFootprint(t, parts...), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func stateGroupWholeAlias(t *testing.T, row durable.ManagedPathState) aggregate.DocumentAddress {
	t.Helper()
	key, err := aggregate.NewHostConfigProjectionContract(row.ConsumerTargets()[0], row.Scope(), row.Destination(), documentKeyFootprint(t, "model"), "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	return key.Address().Document()
}
