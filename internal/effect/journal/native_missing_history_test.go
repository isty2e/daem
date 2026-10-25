package journal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeMissingHistoryCapturesAndClassifiesWithoutHostPublication(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, retained := range []bool{false, true} {
			name := string(scope) + "/last-member"
			if retained {
				name = string(scope) + "/retained-sibling"
			}
			t.Run(name, func(t *testing.T) {
				fixture := wholeGroupFixture(t, target.TargetPi, scope, "record", []byte(`{"removed":1,"retained":1}`), nil)
				exact, known := fixture.namespace.observation.Exact()
				if !known {
					t.Fatal("existing document has no exact historical identity")
				}
				alias := fixture.namespace.Addresses()[0]
				removed := membershipNativeRow(t, alias, "native-json-v1", "removed")
				remaining := membershipNativeRow(t, alias, "native-json-v1", "retained")
				removed = selectionNativeRow(t, removed.Contribution().Contract(), nativeRecordCanonical(t, "1"))
				remaining = selectionNativeRow(t, remaining.Contribution().Contract(), nativeRecordCanonical(t, "1"))
				before, after := selectionState(t, removed, remaining), durable.EmptySnapshot()
				if retained {
					after = selectionState(t, remaining)
				}

				statePath := fixture.input.owner.StatefileKey()
				observedOwner, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
				if err != nil {
					t.Fatal(err)
				}
				owner, err := stateauthority.New(observedOwner.Exact(), fixture.input.owner.ManifestPath())
				if err != nil {
					t.Fatal(err)
				}
				binding, err := ownership.NewDocumentBinding(owner, alias.AggregateRoot(), exact)
				if err != nil {
					t.Fatal(err)
				}
				var claims []ownership.Claim
				if scope == target.ScopeGlobal {
					for _, row := range before.ManagedAggregates() {
						claims = append(claims, documentActiveClaim(t, documentClaimFixture{owner: owner, exact: exact}, row.Contribution().Contract().Address().Footprint()))
					}
				}
				current, err := ownership.NewRegistry(claims, binding)
				if err != nil {
					t.Fatal(err)
				}
				registryBytes, err := ownershipstore.Marshal(current)
				if err != nil {
					t.Fatal(err)
				}
				registryPath := filepath.Join(t.TempDir(), "ownership.json")
				if err := os.WriteFile(registryPath, registryBytes, 0o600); err != nil {
					t.Fatal(err)
				}
				root, destination, err := rootedpath.CaptureDestination(registryPath)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = root.Close() })
				store, err := ownershipstore.BindRooted(root, destination, recovery.MaximumPhysicalPathDepth, fixture.input.budget)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(fixture.path); err != nil {
					t.Fatal(err)
				}
				namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, fixture.namespace.roots, fixture.reader, fixture.input.budget)
				if err != nil {
					t.Fatal(err)
				}
				pending, missing := namespace.observation.Provisional()
				if !missing {
					t.Skip("filesystem supplies exact authority for the absent Unicode destination")
				}
				if !pending.MatchesMissingExact(exact) {
					t.Fatal("current missing observation does not match the actual historical namespace")
				}
				if scope == target.ScopeGlobal {
					if _, err := store.Load(t.Context()); err == nil {
						t.Fatal("default registry read was widened to historical missing claims")
					}
				}
				input := nativeCaptureOwnershipInput{
					owner: owner, operationID: fixture.input.operationID, before: before, after: after,
					namespaces: []NativeDocumentNamespace{namespace}, registry: store,
					limits: fixture.input.limits, budget: fixture.input.budget,
				}
				prepared, err := input.prepare(t.Context())
				if err != nil || len(prepared) != 1 || len(prepared[0].provisional) != 0 {
					t.Fatalf("historical missing namespace became a fresh pending acquisition: %v", err)
				}
				targetValue, ready := prepared[0].bindings[0].KnownAfter()
				settledBinding, hasBinding := targetValue.Get()
				if !ready || hasBinding != retained || retained && !settledBinding.Equal(binding) {
					t.Fatal("historical binding retirement/retention needs invented promotion")
				}
				if scope == target.ScopeGlobal && (prepared[0].claims == nil || len(prepared[0].claims.Before()) != 2 || len(prepared[0].claims.After()) != len(after.ManagedAggregates())) {
					t.Fatal("missing historical claim fragment was lost or promoted")
				}

				subjects, err := indexDocumentCaptureSubjects(t.Context(), before, after)
				if err != nil {
					t.Fatal(err)
				}
				group, comparison, err := prepareSelectedDocumentGroup(t.Context(), namespace, selectedDocumentCaptureInput{nativeDocumentCaptureInput: prepared[0]}, subjects)
				if err != nil || comparison.before.Exists() || comparison.expected.Exists() || group.CapturedTarget.Kind != "provisional" {
					t.Fatalf("metadata settlement fabricated a host image/observation: %v", err)
				}
				fixture.namespace, fixture.original, fixture.expected = namespace, aggregate.AbsentDocument(), aggregate.AbsentDocument()
				fixture.input.nativeDocumentCaptureInput = prepared[0]
				fixture.envelope.before, fixture.envelope.after = before, after
				fixture.envelope.claims.owner = owner
				fixture.envelope.record.StateAuthority = recoveryStateAuthority{StatefileAuthority: *persistedPathAuthority(owner.StatefileAuthority()), ManifestPath: owner.ManifestPath()}
				fixture.stageAndReconstruct(t, group, comparison)

				fixture.envelope.record.DocumentGroups = []recoveryDocumentGroup{group}
				manifest, err := namespace.roots.Manifest.Root.Authority()
				if err != nil {
					t.Fatal(err)
				}
				provenance, err := manifest.Provenance()
				if err != nil {
					t.Fatal(err)
				}
				fixture.envelope.record.ManifestRootProvenance = persistedNativeRoot(provenance)
				content, err := marshalNativeJournalRecord(t.Context(), fixture.envelope.record, before, after, fixture.envelope.codec)
				if err != nil {
					t.Fatal(err)
				}
				journal, err := decodeNativeJournalRecord(t.Context(), content, fixture.envelope.codec)
				if err != nil {
					t.Fatal(err)
				}
				for _, committed := range []bool{false, true} {
					state, want := before, nativeDocumentsBefore
					if committed {
						state, want = after, nativeDocumentsFinalize
						if retained && scope == target.ScopeProject {
							want = nativeDocumentsAfter
						}
					}
					stateBytes, err := fixture.envelope.codec.Encode(state)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(statePath, stateBytes, 0o600); err != nil {
						t.Fatal(err)
					}
					documents := fixture.envelope.read
					documents.budget = imageBudget(t)
					assessment, err := journal.assessDocumentRecovery(t.Context(), nativeDocumentRecoveryReadOptions{
						documents: documents, roots: namespace.roots, registry: store,
						state: func(ctx context.Context) (durable.Snapshot, error) {
							return statefile.LoadOptionalWithOptions(ctx, statePath, statefile.ReadOptions{Codec: fixture.envelope.codec, PhysicalPathBudget: documents.budget})
						},
					})
					if err != nil || assessment.disposition != want || len(assessment.stages) != 1 || assessment.stages[0].restoration.Changed() {
						t.Fatalf("committed=%t: no-write metadata phase=%s want=%s error=%v", committed, assessment.disposition, want, err)
					}
					actual, err := os.ReadFile(registryPath)
					if err != nil || !bytes.Equal(actual, registryBytes) {
						t.Fatalf("read-only assessment published metadata: %v", err)
					}
					actual, err = os.ReadFile(statePath)
					if err != nil || !bytes.Equal(actual, stateBytes) {
						t.Fatalf("assessment changed durable state: %v", err)
					}
					if _, err := os.Lstat(fixture.path); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("metadata-only settlement recreated the host file: %v", err)
					}
				}
			})
		}
	}
}

func TestNativeMissingHistoryRequalificationKeepsOriginalExactNamespace(t *testing.T) {
	fixture := wholeGroupFixture(t, target.TargetPi, target.ScopeGlobal, "record", []byte("before"), nil)
	if err := os.Remove(fixture.path); err != nil {
		t.Fatal(err)
	}
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), fixture.namespace.Addresses(), fixture.namespace.roots, fixture.reader, fixture.input.budget)
	if err != nil {
		t.Fatal(err)
	}
	missing, provisional := namespace.observation.Provisional()
	if !provisional {
		t.Skip("filesystem supplies exact authority for the absent Unicode document")
	}
	group, _, err := nativeDocumentGroupMetadata(t.Context(), namespace, fixture.input.nativeDocumentCaptureInput)
	if err != nil {
		t.Fatal(err)
	}
	journal := nativeJournalSnapshots{owner: fixture.input.owner, record: fixture.envelope.record}
	if err := journal.qualifyRetainedDocumentNamespace(t.Context(), group, namespace.observation); err != nil {
		t.Fatalf("matching missing historical namespace refused: %v", err)
	}
	otherPath := filepath.Join(filepath.Dir(fixture.path), "Other\u00e9.md")
	if err := os.WriteFile(otherPath, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	other, err := mutation.ObserveDirectoryEntryAuthority(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := other.Exact()
	if !known || missing.AdmitsExact(exact) != nil {
		t.Fatal("test requires a real different exact key in the new-publication envelope")
	}
	if err := matchRetainedNativeObservation(group.CapturedTarget, other); err != nil {
		t.Fatalf("new-publication observation envelope did not admit the test key: %v", err)
	}
	for _, history := range []string{"binding", "claims"} {
		candidate := group
		if history == "binding" {
			candidate.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
		} else {
			candidate.BindingIntents = nil
		}
		if err := journal.qualifyRetainedDocumentNamespace(t.Context(), candidate, other); err == nil {
			t.Fatalf("retained %s followed the broader new-publication key instead of its original namespace", history)
		}
	}
}
