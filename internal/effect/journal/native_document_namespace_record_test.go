package journal

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestQualifiedNativeRecordJoinsActualRootsImagesAndSemanticRollback(t *testing.T) {
	fixture, roots := nativeNamespaceRecordFixture(t)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	reader := &nativeNamespaceReader{RootedReader: fixture.read.filesystem}
	fixture.read.filesystem = reader
	loaded, err := decoded.loadQualifiedDocuments(t.Context(), fixture.read, roots)
	if err != nil || len(loaded.namespaces) != 1 || len(loaded.records.groups) != 1 || reader.imageReads != 2 {
		t.Fatalf("live namespace + complete image/record join: %v", err)
	}
	capability, err := loaded.namespaces[0].AcquireReferent(t.Context(), fixture.read.budget)
	if err != nil {
		t.Fatal(err)
	}
	host, mode, _, readErr := journalTestFilesystem().ReadRootedRegularFileUpTo(t.Context(), capability, 4096)
	closeErr := capability.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(host, loaded.records.images[0].original.document.Content()) {
		t.Fatalf("qualified capability did not read the retained original host: %v, %v", readErr, closeErr)
	}
	restored, err := loaded.records.groups[0].comparison.RestoreBefore(t.Context(), loaded.records.images[0].expected.document, recovery.NewPermissionMode(mode))
	if err != nil {
		t.Fatalf("qualified record lost semantic rollback: %v", err)
	}
	var values struct {
		Model   bool
		Outside int
	}
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || !values.Model || values.Outside != 1 {
		t.Fatalf("selected rollback lost before values or protected foreign data: %+v, %v", values, err)
	}
	after, err := os.ReadFile(filepath.Join(roots.PiAgent.SelectedPath, "custom.json"))
	if err != nil || !bytes.Equal(host, after) {
		t.Fatalf("read/reconstruction mutated host bytes: %v", err)
	}
}

func TestQualifiedNativeRecordRefusesWriterOrLogicalRootSubstitution(t *testing.T) {
	for _, change := range []string{"writer root", "writer path", "logical root", "manifest root"} {
		t.Run(change, func(t *testing.T) {
			fixture, roots := nativeNamespaceRecordFixture(t)
			other := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "unselected"))
			authority, err := other.Root.Authority()
			if err != nil {
				t.Fatal(err)
			}
			provenance, err := authority.Provenance()
			if err != nil {
				t.Fatal(err)
			}
			record := &fixture.record.DocumentGroups[0]
			switch change {
			case "writer root":
				record.Writer.Root = persistedNativeRoot(provenance)
			case "writer path":
				record.Writer.RelativePath = "other.json"
				if err := os.WriteFile(filepath.Join(roots.PiAgent.SelectedPath, "other.json"), []byte("foreign"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "logical root":
				record.Aliases[0].LogicalRoot.Provenance = persistedNativeRoot(provenance)
			case "manifest root":
				fixture.record.ManifestRootProvenance = persistedNativeRoot(provenance)
			}
			content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
			if err != nil {
				t.Fatal(err)
			}
			reader := &nativeNamespaceReader{RootedReader: fixture.read.filesystem}
			fixture.read.filesystem = reader
			loaded, err := decoded.loadQualifiedDocuments(t.Context(), fixture.read, roots)
			if err == nil || loaded.namespaces != nil || loaded.records.groups != nil || reader.imageReads != 0 {
				t.Fatal("substituted roots/writer returned a qualified prefix or read private images")
			}
		})
	}
}

func TestQualifiedNativeRecordRejectsTwoLogicalGroupsAtOneActualFile(t *testing.T) {
	fixture, roots := nativeNamespaceRecordFixture(t)
	aliasPath := filepath.Join(roots.PiAgent.SelectedPath, "alias.json")
	if err := os.Symlink("custom.json", aliasPath); err != nil {
		t.Fatal(err)
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/alias.json")
	other := selectionNativeRow(t, stateGroupContract(t, alias, "outside"), nativeRecordCanonical(t, "1"))
	id, err := entity.New(entity.KindHostConfig, "other-document")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, alias.Target(), alias.Scope(), other.Contribution().Contract().Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	other, err = durable.NewManagedAggregateState(subject, other.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	fixture.before = selectionState(t, fixture.before.ManagedAggregates()[0], other)
	fixture.after = selectionState(t, fixture.after.ManagedAggregates()[0], other)
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), fixture.read.budget)
	if err != nil {
		t.Fatal(err)
	}
	second := fixture.record.DocumentGroups[0]
	second.Expected = second.Original
	second.SelectedSubjects = []persistedSubjectRef{{Kind: string(other.Subject().Kind()), Namespace: other.Subject().Namespace(), Name: other.Subject().Key()}}
	second = nativeNamespaceGroupFacts(t, second, namespace)
	claims := fixture.claims
	claims.alias = alias
	claim := documentActiveClaim(t, claims, other.Contribution().Contract().Address().Footprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	second.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, claims)
	binding, err := persistedDocumentBindingIntent(membershipIntent(t, claims, value, value))
	if err != nil {
		t.Fatal(err)
	}
	second.BindingIntents = []recoveryDocumentBindingIntent{binding}
	fixture.record.DocumentGroups = append(fixture.record.DocumentGroups, second)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err == nil || content != nil {
		t.Fatal("two logical groups at one recorded writer escaped strict construction")
	}
	decoded := nativeJournalSnapshots{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
	if records, err := decoded.loadDocuments(t.Context(), fixture.read); err == nil || records.groups != nil {
		t.Fatal("bypassing decode admitted overlapping physical writers")
	}
	reader := &nativeNamespaceReader{RootedReader: fixture.read.filesystem}
	fixture.read.filesystem = reader
	result, err := decoded.loadQualifiedDocuments(t.Context(), fixture.read, roots)
	if err == nil || result.namespaces != nil || result.records.groups != nil || reader.imageReads != 0 {
		t.Fatal("two logical groups qualified the same physical file or returned a prefix")
	}
}

func TestQualifiedNativePiRecordKeepsCapturedLexicalContextSeparateFromPhysicalRoot(t *testing.T) {
	fixture := nativeJournalFixture(t)
	input, order := piEvidenceFixture(t, target.ScopeProject)
	context, known := input.Relations[0].SourceContext()
	if !known {
		t.Fatal("fixture lost its actual command context")
	}
	physical := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "physical-command"))
	if err := os.Mkdir(filepath.Join(physical.SelectedPath, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical.SelectedPath, context.BaseRoot()); err != nil {
		t.Fatal(err)
	}
	commandRoot, err := rootedpath.CaptureRoot(context.BaseRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = commandRoot.Close() })
	if err := os.WriteFile(filepath.Join(physical.SelectedPath, ".pi", "settings.json"), input.Document.Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	expected, err := order.Project(input.Document)
	if err != nil {
		t.Fatal(err)
	}
	nativeRecordImages(t, &fixture, input.Document, expected)
	roots := NativeDocumentRoots{Manifest: NativeDocumentRoot{SelectedPath: physical.SelectedPath, Root: commandRoot}}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json")
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), fixture.read.budget)
	if err != nil {
		t.Fatal(err)
	}
	fixture.record.ManifestRootProvenance = persistedNativeRoot(namespace.aliases[0].root.provenance)
	group := &fixture.record.DocumentGroups[0]
	*group = nativeNamespaceGroupFacts(t, *group, namespace)
	group.SelectedSubjects, group.BindingIntents = []persistedSubjectRef{}, []recoveryDocumentBindingIntent{}
	group.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
	evidence, err := persistedPiOrderEvidence(order)
	if err != nil {
		t.Fatal(err)
	}
	group.PiOrder = &recoveryDocumentPiOrder{Alias: persistedDocumentAlias(alias), CommandRoot: recoveryDocumentLogicalRoot{Role: "manifest-root", Provenance: fixture.record.ManifestRootProvenance}, Evidence: evidence}
	fixture.before, fixture.after = durable.EmptySnapshot(), durable.EmptySnapshot()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decoded.loadQualifiedDocuments(t.Context(), fixture.read, roots)
	if err != nil || len(loaded.records.groups) != 1 || loaded.records.groups[0].comparison.order == nil {
		t.Fatalf("captured lexical/physical context was conflated: %v", err)
	}
	other := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "different-command"))
	decoded.record.DocumentGroups[0].PiOrder.Evidence.Relations[0].SourceContext.BaseRoot = other.SelectedPath
	if result, err := decoded.loadQualifiedDocuments(t.Context(), fixture.read, roots); err == nil || result.records.groups != nil || result.namespaces != nil {
		t.Fatal("Pi replay accepted a different command-root incarnation")
	}
}

func nativeNamespaceRecordFixture(t *testing.T) (nativeRecordFixture, NativeDocumentRoots) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	manifest := NativeDocumentRoot{SelectedPath: fixture.record.ManifestRootProvenance.PhysicalRoot, Root: fixture.read.artifacts}
	pi := nativeNamespaceRoot(t, filepath.Join(manifest.SelectedPath, "host"))
	content, err := os.ReadFile(filepath.Join(manifest.SelectedPath, "images", "before"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pi.SelectedPath, "custom.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	roots := NativeDocumentRoots{Manifest: manifest, PiAgent: pi}
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{fixture.claims.alias}, roots, journalTestFilesystem(), fixture.read.budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := namespace.observation.Exact()
	if !known {
		t.Fatal("existing fixture file did not have exact authority")
	}
	fixture.claims.exact = exact
	group := &fixture.record.DocumentGroups[0]
	*group = nativeNamespaceGroupFacts(t, *group, namespace)
	claim := documentActiveClaim(t, fixture.claims, fixture.before.ManagedAggregates()[0].Contribution().Contract().Address().Footprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	group.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, fixture.claims)
	binding, err := persistedDocumentBindingIntent(membershipIntent(t, fixture.claims, value, value))
	if err != nil {
		t.Fatal(err)
	}
	group.BindingIntents = []recoveryDocumentBindingIntent{binding}
	return fixture, roots
}

func nativeNamespaceGroupFacts(t *testing.T, group recoveryDocumentGroup, namespace NativeDocumentNamespace) recoveryDocumentGroup {
	t.Helper()
	observed, err := persistedNativeObservation(namespace.observation)
	if err != nil {
		t.Fatal(err)
	}
	group.CapturedTarget, group.Writer = observed, namespace.writerRecord
	group.Aliases = make([]recoveryDocumentAlias, 0, len(namespace.aliases))
	for _, alias := range namespace.aliases {
		role := "global-root"
		if alias.address.Scope() == target.ScopeProject {
			role = "manifest-root"
		}
		group.Aliases = append(group.Aliases, recoveryDocumentAlias{Address: persistedDocumentAlias(alias.address), LogicalRoot: recoveryDocumentLogicalRoot{Role: role, Provenance: persistedNativeRoot(alias.root.provenance)}, SelectedObservation: observed, ObservedBefore: observed})
	}
	return group
}
