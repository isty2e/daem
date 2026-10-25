package journal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestNativeJournalMixedCodexRecordProtectsForeignMCPValues(t *testing.T) {
	fixture := nativeJournalFixture(t)
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !found {
		t.Fatal("Codex placement unavailable")
	}
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{ServerID: "search", Command: "node", AdapterContract: aggregate.CodexProjectMCPStdioCommandV1})
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
	fixture.claims.alias = contribution.Contract().Address().Document()
	native := membershipNativeRow(t, fixture.claims.alias, "native-toml-v1", "model")
	native = selectionNativeRow(t, native.Contribution().Contract(), nativeRecordCanonical(t, "true"))
	updated := selectionNativeRow(t, native.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	fixture.before, fixture.after = selectionState(t, native, mcp), selectionState(t, updated, mcp)
	original := aggregate.ExistingDocument([]byte("model=true\n[mcp_servers.search]\ncommand='node'\n[mcp_servers.foreign]\ncommand='foreign' # retained\n"))
	expected := aggregate.ExistingDocument([]byte("model='fast'\n[mcp_servers.search]\ncommand='node'\n[mcp_servers.foreign]\ncommand='foreign' # retained\n"))
	nativeRecordImages(t, &fixture, original, expected)
	group := &fixture.record.DocumentGroups[0]
	group.Aliases[0].Address = persistedDocumentAlias(fixture.claims.alias)
	group.Aliases[0].LogicalRoot.Role = "manifest-root"
	group.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
	group.SelectedSubjects = []persistedSubjectRef{
		{Kind: string(native.Subject().Kind()), Namespace: native.Subject().Namespace(), Name: native.Subject().Key()},
		{Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key()},
	}
	value := membershipBindingValue(t, fixture.claims)
	group.BindingIntents[0], err = persistedDocumentBindingIntent(membershipIntent(t, fixture.claims, value, value))
	if err != nil {
		t.Fatal(err)
	}
	fixture.read.codecs, err = aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{native.Contribution().CodecContractID(), mcp.Contribution().CodecContractID()}, fixture.read.limits, fixture.read.limits)
	if err != nil {
		t.Fatal(err)
	}
	fixture.codec, err = statefile.NewNativeCodec(fixture.read.codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	loaded := nativeRecordLoadFixture(t, fixture)
	if len(loaded.groups[0].selections) != 2 {
		t.Fatal("mixed record lost a semantic participant")
	}
	nativeRecordImages(t, &fixture, original, aggregate.ExistingDocument([]byte("model='fast'\n[mcp_servers.search]\ncommand='node'\n[mcp_servers.foreign]\ncommand='corrupt' # retained\n")))
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := decoded.loadDocuments(t.Context(), fixture.read); err == nil || loaded.groups != nil || loaded.images != nil {
		t.Fatal("valid image hashes turned the MCP rewrite parent into permission over foreign values")
	}
}

func TestNativeJournalPreservesOrdinarySiblingEntriesWithoutSharingTheirWriter(t *testing.T) {
	fixture := nativeJournalFixture(t)
	legacy := defaultRecoveryJournal()
	fixture.record.Entries, fixture.record.RemovalIntents = legacy.Entries, legacy.RemovalIntents
	var err error
	fixture.before, err = fixture.before.WithManagedPaths(legacy.StatefileBefore.ManagedPaths())
	if err != nil {
		t.Fatal(err)
	}
	fixture.after, err = fixture.after.WithManagedPaths(legacy.StatefileAfter.ManagedPaths())
	if err != nil {
		t.Fatal(err)
	}
	loaded := nativeRecordLoadFixture(t, fixture)
	if len(loaded.groups) != 1 || len(fixture.before.ManagedPaths()) != len(legacy.StatefileBefore.ManagedPaths()) {
		t.Fatal("native reconstruction lost ordinary complete-Snapshot state")
	}
	group := &fixture.record.DocumentGroups[0]
	group.Aliases[0].Address.Scope = legacy.Entries[0].Scope
	group.Aliases[0].Address.Destination = legacy.Entries[0].Path
	group.Aliases[0].LogicalRoot.Role = "manifest-root"
	if content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec); err == nil || content != nil {
		t.Fatal("ordinary entry and group acquired the same logical writer")
	}
}

func TestNativeJournalWholeExtentDoesNotParseOpaqueImages(t *testing.T) {
	fixture := nativeJournalFixture(t)
	group := &fixture.record.DocumentGroups[0]
	original, expected := aggregate.ExistingDocument([]byte("opaque before\x00\n")), aggregate.ExistingDocument([]byte("opaque expected\xff\n"))
	nativeRecordImages(t, &fixture, original, expected)
	group.Extent, group.SelectedSubjects = "whole", []persistedSubjectRef{}
	subject := nativeWholeFileJournalBaseline(t, target.TargetPi, target.ScopeGlobal).Subject()
	var rows []durable.ManagedPathState
	for _, document := range []aggregate.Document{original, expected} {
		row, err := durable.NewManagedPathState(subject, []target.Target{target.TargetPi}, target.ScopeGlobal, fixture.claims.alias.AggregateRoot(), artifact.HashFileContent(document.Content()), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	var err error
	fixture.before, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: rows[:1]})
	if err != nil {
		t.Fatal(err)
	}
	fixture.after, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: rows[1:]})
	if err != nil {
		t.Fatal(err)
	}
	claim := documentActiveClaim(t, fixture.claims, output.WholePathFootprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	group.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	loaded := nativeRecordLoadFixture(t, fixture)
	restored, err := loaded.groups[0].comparison.RestoreBefore(t.Context(), expected, recovery.NewPermissionMode(0o600))
	if err != nil || !restored.Document().Equal(original) {
		t.Fatalf("opaque whole witness was parsed or re-encoded: %v", err)
	}
	*group.Expected.Mode = 0o640
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := decoded.loadDocuments(t.Context(), fixture.read); err == nil || loaded.groups != nil || loaded.images != nil {
		t.Fatal("whole expected mode disagreed with the durable after baseline")
	}
}

func TestNativeJournalPiOnlyGroupReplaysEqualStateWithoutSyntheticNativeMembers(t *testing.T) {
	fixture := nativeJournalFixture(t)
	input, order := piEvidenceFixture(t, target.ScopeGlobal)
	evidence, err := persistedPiOrderEvidence(order)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := order.Project(input.Document)
	if err != nil {
		t.Fatal(err)
	}
	nativeRecordImages(t, &fixture, input.Document, expected)
	group := &fixture.record.DocumentGroups[0]
	alias := recoveryDocumentAliasRef{Target: "pi", Scope: "global", Destination: "@pi-agent/settings.json"}
	group.Aliases[0].Address = alias
	group.SelectedSubjects = []persistedSubjectRef{}
	group.BindingIntents = []recoveryDocumentBindingIntent{}
	group.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
	group.PiOrder = &recoveryDocumentPiOrder{Alias: alias, CommandRoot: recoveryDocumentLogicalRoot{Role: "manifest-root", Provenance: fixture.record.ManifestRootProvenance}, Evidence: evidence}
	fixture.before, fixture.after = durable.EmptySnapshot(), durable.EmptySnapshot()
	loaded := nativeRecordLoadFixture(t, fixture)
	if len(loaded.groups[0].selections) != 0 || loaded.groups[0].comparison.order == nil {
		t.Fatal("Pi-only work became native membership or lost its real inverse")
	}
	restored, err := loaded.groups[0].comparison.RestoreBefore(t.Context(), expected, recovery.NewPermissionMode(0o600))
	if err != nil || !restored.Document().Equal(input.Document) {
		t.Fatalf("record-backed Pi semantic replay: %v", err)
	}
	group.PiOrder.Alias.Destination = "@pi-agent/custom.json"
	group.Aliases[0].Address = group.PiOrder.Alias
	if content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec); err == nil || content != nil {
		t.Fatal("Pi order acquired an arbitrary JSON document")
	}
}

func TestNativeJournalProvisionalClaimsPromoteWithinTheSameCompleteRecord(t *testing.T) {
	fixture := nativeJournalFixture(t)
	group := &fixture.record.DocumentGroups[0]
	fixture.before = durable.EmptySnapshot()
	nativeRecordImages(t, &fixture, aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{"model":"fast"}`)))
	pending := fixture.claims.provisional
	observation := recoveryDocumentObservation{Kind: "provisional", Authority: &pathAuthorityDTO{Key: pending.CandidateKey(), Witness: pending.CandidateWitness()}, NamespaceAuthority: persistedPathAuthority(pending.Namespace())}
	group.CapturedTarget = observation
	group.Aliases[0].SelectedObservation, group.Aliases[0].ObservedBefore = observation, observation
	intent, err := ownership.NewProvisionalAcquireIntent(fixture.claims.alias.AggregateRoot(), documentKeyFootprint(t, "model"), pending, fixture.claims.owner, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	group.ExclusiveClaims, err = persistedDocumentProvisionalClaims([]documentProvisionalAcquire{{alias: fixture.claims.alias, intent: intent}})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewProvisionalDocumentBindingIntent(fixture.claims.owner, fixture.claims.alias.AggregateRoot(), pending)
	if err != nil {
		t.Fatal(err)
	}
	persistedBinding, err := persistedDocumentBindingIntent(binding)
	if err != nil {
		t.Fatal(err)
	}
	group.BindingIntents = []recoveryDocumentBindingIntent{persistedBinding}
	loaded := nativeRecordLoadFixture(t, fixture)
	if len(loaded.provisional[0]) != 1 || len(loaded.claims[0].After()) != 0 {
		t.Fatal("provisional record fabricated exact claims before promotion")
	}
	group.ExclusiveClaims, err = group.ExclusiveClaims.promoted([]aggregate.DocumentAddress{fixture.claims.alias}, fixture.claims.owner, fixture.record.OperationID, pending, fixture.claims.exact)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := binding.Promote(fixture.claims.exact)
	if err != nil {
		t.Fatal(err)
	}
	group.BindingIntents[0], err = persistedDocumentBindingIntent(promoted)
	if err != nil {
		t.Fatal(err)
	}
	loaded = nativeRecordLoadFixture(t, fixture)
	if len(loaded.provisional[0]) != 0 || len(loaded.claims[0].Prepared()) != 1 || loaded.images[0].original.document.Exists() {
		t.Fatal("promoted record lost original namespace/absence or exact prepared exclusion")
	}
	if group.CapturedTarget.Kind != "provisional" || group.Aliases[0].ObservedBefore.Kind != "provisional" {
		t.Fatal("promotion erased the retained before namespace")
	}
}

func nativeRecordLoadFixture(t *testing.T, fixture nativeRecordFixture) nativeDocumentRecords {
	t.Helper()
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decoded.loadDocuments(t.Context(), fixture.read)
	if err != nil || len(loaded.groups) != len(fixture.record.DocumentGroups) {
		t.Fatalf("complete variant record: %v", err)
	}
	comparisons := make([]DocumentComparison, len(loaded.groups))
	for index, group := range loaded.groups {
		comparisons[index] = group.comparison
	}
	prepared, err := decoded.prepareDocuments(t.Context(), comparisons, fixture.read.limits, fixture.read.codecs)
	if err != nil || len(prepared.groups) != len(loaded.groups) || len(prepared.claims) != len(loaded.claims) || len(prepared.provisional) != len(loaded.provisional) {
		t.Fatalf("prepared variant differs from artifact reconstruction: %v", err)
	}
	return loaded
}

func nativeRecordImages(t *testing.T, fixture *nativeRecordFixture, original, expected aggregate.Document) {
	t.Helper()
	group := &fixture.record.DocumentGroups[0]
	for index, document := range []aggregate.Document{original, expected} {
		path := []string{"images/before", "images/expected"}[index]
		var mode *recovery.PermissionMode
		if document.Exists() {
			mode = recovery.NewPermissionMode(0o600)
			if err := os.WriteFile(filepath.Join(fixture.record.ManifestRootProvenance.PhysicalRoot, filepath.FromSlash(path)), document.Content(), 0o600); err != nil {
				t.Fatal(err)
			}
		} else {
			path = ""
		}
		record, err := persistedDocumentImage(document, mode, path)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			group.Original = record
		} else {
			group.Expected = record
		}
	}
}
