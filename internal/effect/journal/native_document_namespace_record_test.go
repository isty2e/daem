package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/topology"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/physicalwork"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestQualifiedNativeRecordRefusesWriterOrLogicalRootSubstitution(t *testing.T) {
	for _, change := range []string{"writer root", "writer path", "logical root", "manifest root"} {
		t.Run(change, func(t *testing.T) {
			fixture, roots := nativeNamespaceRecordFixture(t)
			other := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "unselected"))
			provenance := captureFixtureProvenance(t, other.SelectedPath)
			wireRoot := nativeRecordJSON(t, map[string]string{"physical_root": provenance.PhysicalRoot(), "object_fingerprint": provenance.ObjectFingerprint(), "mount_fingerprint": provenance.MountFingerprint()})
			if change == "writer path" {
				if err := os.WriteFile(filepath.Join(roots.PiAgent.SelectedPath, "other.json"), []byte("foreign"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			decoded := publishedNativeVariant(t, fixture, func(object map[string]json.RawMessage) {
				if change == "manifest root" {
					object["manifest_root_provenance"] = wireRoot
					return
				}
				var groups []map[string]json.RawMessage
				if err := json.Unmarshal(object["document_groups"], &groups); err != nil {
					t.Fatal(err)
				}
				group := groups[0]
				if change == "logical root" {
					var aliases []map[string]json.RawMessage
					if err := json.Unmarshal(group["aliases"], &aliases); err != nil {
						t.Fatal(err)
					}
					var logical map[string]json.RawMessage
					if err := json.Unmarshal(aliases[0]["logical_root"], &logical); err != nil {
						t.Fatal(err)
					}
					logical["provenance"] = wireRoot
					aliases[0]["logical_root"] = nativeRecordJSON(t, logical)
					group["aliases"] = nativeRecordJSON(t, aliases)
				} else {
					var writer map[string]json.RawMessage
					if err := json.Unmarshal(group["writer"], &writer); err != nil {
						t.Fatal(err)
					}
					if change == "writer root" {
						writer["root_provenance"] = wireRoot
					} else {
						writer["relative_path"] = nativeRecordJSON(t, "other.json")
					}
					group["writer"] = nativeRecordJSON(t, writer)
				}
				object["document_groups"] = nativeRecordJSON(t, groups)
			})
			reader := &nativeNamespaceReader{RootedReader: fixture.read.Filesystem}
			fixture.read.Filesystem = reader
			loaded, err := journalqualification.QualifyDocuments(decoded, t.Context(), fixture.read, roots)
			_, hasNamespace := loaded.NamespaceAt(0)
			if err == nil || hasNamespace || loaded.Len() != 0 || reader.imageReads != 0 {
				t.Fatal("substituted roots/writer returned a qualified prefix or read private images")
			}
		})
	}
}

func TestQualifiedNativeRecordRejectsTwoLogicalGroupsAtOneActualFile(t *testing.T) {
	fixture, roots := nativeNamespaceRecordFixture(t)
	if err := os.Symlink("custom.json", filepath.Join(roots.PiAgent.SelectedPath, "alias.json")); err != nil {
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
	before := selectionState(t, fixture.before.ManagedAggregates()[0], other)
	after := selectionState(t, fixture.after.ManagedAggregates()[0], other)
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), fixture.read.Budget)
	if err != nil {
		t.Fatal(err)
	}
	claims := fixture.claims
	claims.alias = alias
	claim := documentActiveClaim(t, claims, other.Contribution().Contract().Address().Footprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.journal.OperationID())
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, claims)
	draft, err := journalrecord.BeginDocumentGroup(claims.owner, fixture.journal.OperationID(), []ownership.DocumentBindingIntent{membershipIntent(t, claims, value, value)}, &transition, nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest := captureFixtureProvenance(t, roots.Manifest.SelectedPath)
	aliases := make([]journalrecord.DocumentGroupAlias, 0, len(namespace.Aliases()))
	for _, alias := range namespace.Aliases() {
		aliases = append(aliases, journalrecord.NewDocumentGroupAlias(alias.Address(), alias.Root().Provenance(), alias.Observation()))
	}
	pin := namespace.WriterPin()
	draft, err = draft.WithNamespace(t.Context(), namespace.Observation(), pin.Provenance(), pin.Relative(), aliases, manifest)
	if err != nil {
		t.Fatal(err)
	}
	comparison := publishedNativeComparison(t, fixture.comparison.BeforeDocument(), other, fixture.read.Codecs, fixture.read.Limits)
	candidate, err := draft.WithComparison(comparison, 1, []topology.SubjectID{other.Subject()})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := candidate.Seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	groups := append(slices.Clone(fixture.capturedGroups), second)
	journal, err := journalrecord.NewNativeCapture(fixture.journal.OperationID(), time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), claims.owner, manifest, before, after, groups)
	if err != nil {
		t.Fatal(err)
	}
	if content, err := journal.Encode(t.Context(), fixture.codec); err == nil || content != nil {
		t.Fatal("two logical groups at one recorded writer escaped strict construction")
	}
	if records, err := journalqualification.LoadDocuments(journal, t.Context(), fixture.read); err == nil || records.Len() != 0 {
		t.Fatal("bypassing decode admitted overlapping physical writers")
	}
	reader := &nativeNamespaceReader{RootedReader: fixture.read.Filesystem}
	fixture.read.Filesystem = reader
	result, err := journalqualification.QualifyDocuments(journal, t.Context(), fixture.read, roots)
	_, hasNamespace := result.NamespaceAt(0)
	if err == nil || hasNamespace || result.Len() != 0 || reader.imageReads != 0 {
		t.Fatal("two logical groups qualified the same physical file or returned a prefix")
	}
}

func TestQualifiedNativePiRecordKeepsCapturedLexicalContextSeparateFromPhysicalRoot(t *testing.T) {
	fixture, _ := publishedPiOrderFixtureWithManifest(t, target.ScopeProject, func(lexical string) journalnamespace.RootSelection {
		physical := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "physical-command"))
		if err := os.Mkdir(filepath.Join(physical.SelectedPath, ".pi"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(physical.SelectedPath, lexical); err != nil {
			t.Fatal(err)
		}
		command, err := rootedpath.CaptureRoot(lexical)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = command.Close() })
		return journalnamespace.RootSelection{SelectedPath: physical.SelectedPath, Root: command}
	})
	roots := fixture.roots
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	loaded, err := journalqualification.QualifyDocuments(fixture.journal, t.Context(), fixture.read, roots)
	if err != nil || loaded.Records().Len() != 1 || !requireLoadedNativeDocument(t, loaded.Records(), 0).Group().Comparison().Changed() {
		t.Fatalf("captured lexical/physical context was conflated: %v", err)
	}
	comparison := requireLoadedNativeDocument(t, loaded.Records(), 0).Group().Comparison()
	mode, present := comparison.ExpectedState().RegularFileMode()
	if !present {
		t.Fatal("loaded Pi-order witness lost its regular-file mode")
	}
	restored, err := comparison.RestoreBefore(t.Context(), comparison.ExpectedDocument(), &mode)
	if err != nil || !restored.Document().Equal(comparison.BeforeDocument()) {
		t.Fatalf("qualified Pi-order evidence lost its actual inverse: %v", err)
	}
	for _, source := range []string{"settings", "relation"} {
		t.Run("invalid "+source+" context", func(t *testing.T) {
			changed := publishedNativeGroupVariant(t, fixture, func(group map[string]json.RawMessage) {
				var order map[string]any
				if err := json.Unmarshal(group["pi_order"], &order); err != nil {
					t.Fatal(err)
				}
				evidence := order["evidence"].(map[string]any)
				if source == "settings" {
					evidence["settings_context"].(map[string]any)["home_root"] = ""
				} else {
					evidence["relations"].([]any)[0].(map[string]any)["source_context"].(map[string]any)["home_root"] = ""
				}
				group["pi_order"] = nativeRecordJSON(t, order)
			})
			reader := &imageReader{RootedReader: fixture.read.Filesystem}
			options := fixture.read
			options.Filesystem, options.Budget = reader, imageBudget(t)
			result, err := journalqualification.QualifyDocuments(changed, t.Context(), options, roots)
			_, hasNamespace := result.NamespaceAt(0)
			if err == nil || result.Len() != 0 || hasNamespace {
				t.Fatal("invalid source context returned qualified evidence")
			}
			if reader.calls != 2 {
				t.Fatal("source-context admission moved ahead of the recorded image pair")
			}
		})
	}
	other := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "different-command"))
	changed := publishedNativeGroupVariant(t, fixture, func(group map[string]json.RawMessage) {
		var order map[string]any
		if err := json.Unmarshal(group["pi_order"], &order); err != nil {
			t.Fatal(err)
		}
		order["evidence"].(map[string]any)["relations"].([]any)[0].(map[string]any)["source_context"].(map[string]any)["base_root"] = other.SelectedPath
		group["pi_order"] = nativeRecordJSON(t, order)
	})
	options := fixture.read
	options.Budget = imageBudget(t)
	result, err := journalqualification.QualifyDocuments(changed, t.Context(), options, roots)
	_, hasNamespace := result.NamespaceAt(0)
	if err == nil || result.Len() != 0 || hasNamespace {
		t.Fatal("Pi replay accepted a different command-root incarnation")
	}
}

func TestNativeNamespaceReobservesRealCreationWithoutForgingExactAuthority(t *testing.T) {
	pi := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "pi"))
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/Caf\u00e9.json")
	roots := journalnamespace.Roots{PiAgent: pi}
	budget := imageBudget(t)
	before, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	retained := namespaceRecordViewForTest(t, before)
	capability, err := before.AcquireReferent(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"created":true}`)
	if err := journalTestFilesystem().CreateRootedFile(t.Context(), capability, content, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	if _, known := after.Observation().Exact(); !known {
		t.Fatal("real visible entry did not supply exact namespace evidence")
	}
	if err := retained.RequireRetainedNamespace(t.Context(), after.Observation()); err != nil {
		t.Fatalf("captured missing path did not admit its actual visible entry: %v", err)
	}
	if _, provisional := before.Observation().Provisional(); provisional {
		if capability, err := before.AcquireReferent(t.Context(), budget); err == nil || capability != nil {
			t.Fatal("provisional proof silently promoted itself after creation")
		}
	}
	capability, err = after.AcquireReferent(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	actual, _, _, readErr := journalTestFilesystem().ReadRootedRegularFileUpTo(t.Context(), capability, 4096)
	closeErr := capability.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, content) {
		t.Fatalf("fresh exact referent did not read the created bytes: %v, %v", readErr, closeErr)
	}
}

func TestNativeMissingRootSelectionRetainsOriginalAnchorWithoutCreatingIt(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(base, "new", "agent")
	budget := physicalwork.NewCaptureBudget(nil)
	root, err := journalnamespace.CaptureRoot(t.Context(), selected, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Root.Close() })
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json")
	roots := journalnamespace.Roots{PiAgent: root}
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(base, "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("observation created selected root scaffolding", err)
	}
	authority, err := root.Root.AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	reopened, err := journalnamespace.CaptureRecordedRoot(t.Context(), selected, provenance, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Root.Close()
	current, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, journalnamespace.Roots{PiAgent: reopened}, journalTestFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	recorded := namespaceRecordViewForTest(t, namespace)
	if err := recorded.RequireRetainedNamespace(t.Context(), current.Observation()); err != nil {
		t.Fatal("created root lost its admitted original namespace", err)
	}
	if current.WriterPin().Provenance().PhysicalRoot() != namespace.WriterPin().Provenance().PhysicalRoot() {
		t.Fatal("recovery replaced the original anchor with the created directory")
	}
}

func namespaceRecordViewForTest(t *testing.T, namespace journalnamespace.DocumentNamespace) journalrecord.Document {
	t.Helper()
	owner := documentClaimsForTest(t).owner
	draft, err := journalrecord.BeginDocumentGroup(owner, "namespace-view", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	aliases := make([]journalrecord.DocumentGroupAlias, 0, len(namespace.Aliases()))
	for _, alias := range namespace.Aliases() {
		aliases = append(aliases, journalrecord.NewDocumentGroupAlias(alias.Address(), alias.Root().Provenance(), alias.Observation()))
	}
	pin := namespace.WriterPin()
	draft, err = draft.WithNamespace(t.Context(), namespace.Observation(), pin.Provenance(), pin.Relative(), aliases, pin.Provenance())
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{Before: aggregate.AbsentDocument(), Expected: aggregate.AbsentDocument(), Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := draft.WithComparison(comparison, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	group, _, err := candidate.Seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := journalrecord.NewNativeCapture("namespace-view", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), owner, pin.Provenance(), durable.EmptySnapshot(), durable.EmptySnapshot(), []journalrecord.SealedDocumentGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	record, err := journal.DocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
