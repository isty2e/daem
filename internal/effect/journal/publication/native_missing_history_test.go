package publication

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"

	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeMissingHistoryCapturesAndSettlesWithoutHostPublication(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, retained := range []bool{false, true} {
			name := string(scope) + "/last-member"
			if retained {
				name = string(scope) + "/retained-sibling"
			}
			t.Run(name, func(t *testing.T) {
				fixture := wholeGroupFixture(t, target.TargetPi, scope, "record", []byte(`{"removed":1,"retained":1}`), nil)
				exact, known := fixture.namespace.Observation().Exact()
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

				statePath := fixture.input.Owner().StatefileKey()
				observedOwner, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
				if err != nil {
					t.Fatal(err)
				}
				owner, err := stateauthority.New(observedOwner.Exact(), fixture.input.Owner().ManifestPath())
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
				store, err := ownershipstore.BindRooted(root, destination, physicalwork.MaximumPhysicalPathDepth, fixture.input.Budget())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(fixture.path); err != nil {
					t.Fatal(err)
				}
				namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, fixture.roots, namespacetest.Reservations(t), fixture.reader, fixture.input.Budget())
				if err != nil {
					t.Fatal(err)
				}
				pending, missing := namespace.Observation().Provisional()
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
					owner: owner, operationID: fixture.input.OperationID(), before: before, after: after,
					protection: namespacetest.Reservations(t),
					namespaces: []journalnamespace.DocumentNamespace{namespace}, registry: store,
					roots: fixture.roots, limits: fixture.input.Limits(), budget: physicalwork.NewCaptureBudget(nil),
				}
				prepared, err := input.prepare(t.Context())
				if err != nil || len(prepared) != 1 || len(prepared[0].Provisional()) != 0 {
					t.Fatalf("historical missing namespace became a fresh pending acquisition: %v", err)
				}
				targetValue, ready := prepared[0].Bindings()[0].KnownAfter()
				settledBinding, hasBinding := targetValue.Get()
				if !ready || hasBinding != retained || retained && !settledBinding.Equal(binding) {
					t.Fatal("historical binding retirement/retention needs invented promotion")
				}
				if scope == target.ScopeGlobal && (prepared[0].Claims() == nil || len(prepared[0].Claims().Before()) != 2 || len(prepared[0].Claims().After()) != len(after.ManagedAggregates())) {
					t.Fatal("missing historical claim fragment was lost or promoted")
				}

				subjects, err := indexDocumentCaptureSubjects(t.Context(), before, after)
				if err != nil {
					t.Fatal(err)
				}
				candidate, err := prepareSelectedDocumentCandidate(t.Context(), namespace, selectedDocumentCaptureInput{CaptureInput: prepared[0]}, subjects)
				if err != nil {
					t.Fatal(err)
				}
				groups, comparisons, _, err := captureNativeDocumentCandidates(t.Context(), []nativeDocumentCaptureCandidate{candidate}, recovery.RemovalDemandSet{}, prepared[0].Budget())
				if err != nil {
					t.Fatal(err)
				}
				group, comparison := groups[0], comparisons[0]
				if comparison.BeforeDocument().Exists() || comparison.ExpectedDocument().Exists() {
					t.Fatalf("metadata settlement fabricated a host image/observation: %v", err)
				}
				fixture.namespace, fixture.original, fixture.expected = namespace, aggregate.AbsentDocument(), aggregate.AbsentDocument()
				fixture.input.CaptureInput = prepared[0]
				fixture.envelope.before, fixture.envelope.after = before, after
				fixture.envelope.claims.owner = owner
				fixture.stageAndReconstruct(t, group, comparison)

				journal := fixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
				journal, fixture.envelope.read.Artifacts = publishNativeFixture(t, journal, comparison, StageOptions{StateCodec: fixture.envelope.codec, Codecs: fixture.envelope.read.Codecs, Limits: fixture.input.Limits(), Budget: imageBudget(t)})
				recorded, err := journal.DocumentAt(0)
				if err != nil {
					t.Fatal(err)
				}
				if err := recorded.RequireCapturedNamespace(namespace.Observation()); err != nil {
					t.Fatal("published missing history lost its actual provisional observation", err)
				}
				for _, committed := range []bool{false, true} {
					state, want := before, recovery.ClassificationCleanBefore
					if committed {
						state, want = after, recovery.ClassificationNeedsFinalize
						if retained && scope == target.ScopeProject {
							want = recovery.ClassificationCleanAfter
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
					documents.Budget = imageBudget(t)
					assessment, err := journalqualification.AssessNativeDocuments(journal, t.Context(), journalqualification.NativeReadOptions{
						Documents: documents, Roots: fixture.roots, Registry: store,
						State: func(ctx context.Context) (durable.Snapshot, error) {
							return statefile.LoadOptionalWithOptions(ctx, statePath, statefile.ReadOptions{Codec: fixture.envelope.codec, PhysicalPathBudget: documents.Budget})
						},
					})
					if err != nil || assessment.Facts().Classification() != want || assessment.Facts().DocumentCount() != 1 || requireAssessedDocument(t, assessment, 0).Restoration().Changed() {
						t.Fatalf("committed=%t: no-write metadata phase=%s want=%s error=%v", committed, assessment.Facts().Classification(), want, err)
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
					if committed {
						writer, supported := store.(ownershipmutation.DocumentRegistryStore)
						if !supported {
							t.Fatal("actual rooted store lacks document-qualified settlement")
						}
						result, err := writer.ConvergeForDocuments(t.Context(), assessment.Facts().FinalizeClaims(), assessment.Facts().FinalizeBindings(), owner, []pathauthority.Provisional{pending}, physicalwork.MaximumPhysicalPathDepth, documents.Budget)
						if err != nil {
							t.Fatalf("persisted after-state metadata did not settle: %v", err)
						}
						var afterClaims []ownership.Claim
						if prepared[0].Claims() != nil {
							afterClaims = prepared[0].Claims().After()
						}
						var afterBindings []ownership.DocumentBinding
						if retained {
							afterBindings = []ownership.DocumentBinding{binding}
						}
						expected, err := ownership.NewRegistry(afterClaims, afterBindings...)
						if err != nil || !result.Equal(expected) {
							t.Fatalf("joint settlement lost its exact after fragment: %v", err)
						}
						encoded, err := ownershipstore.Marshal(expected)
						if err != nil {
							t.Fatal(err)
						}
						actual, err = os.ReadFile(registryPath)
						if err != nil || !bytes.Equal(actual, encoded) {
							t.Fatalf("joint settlement did not persist both exact families: %v", err)
						}
						if _, err := os.Lstat(fixture.path); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("joint metadata write created the absent host: %v", err)
						}
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
	namespace, err := journalnamespace.Observe(t.Context(), fixture.namespace.Addresses(), fixture.roots, namespacetest.Reservations(t), fixture.reader, fixture.input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	missing, provisional := namespace.Observation().Provisional()
	if !provisional {
		t.Skip("filesystem supplies exact authority for the absent Unicode document")
	}
	build := func(claims *ownershipmutation.DocumentClaimTransition, bindings []ownership.DocumentBindingIntent) journalrecord.Native {
		draft, err := journalrecord.BeginDocumentGroup(fixture.input.Owner(), fixture.input.OperationID(), bindings, claims, nil)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := namespacetest.LegacyRoot(t, fixture.roots, output.RootProject).Root.Authority()
		if err != nil {
			t.Fatal(err)
		}
		provenance, err := manifest.Provenance()
		if err != nil {
			t.Fatal(err)
		}
		var aliases []journalrecord.DocumentGroupAlias
		for _, alias := range namespace.Aliases() {
			aliases = append(aliases, journalrecord.NewDocumentGroupAlias(alias.Address(), alias.Root().Provenance(), alias.Observation()))
		}
		pin := namespace.WriterPin()
		draft, err = draft.WithNamespace(t.Context(), namespace.Observation(), pin.Provenance(), pin.Relative(), aliases, provenance)
		if err != nil {
			t.Fatal(err)
		}
		comparison, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{Before: aggregate.AbsentDocument(), Expected: aggregate.AbsentDocument(), Limits: fixture.input.Limits()})
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
		return fixture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	}
	journal := build(fixture.input.Claims(), fixture.input.Bindings())
	recorded, err := journal.DocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorded.RequireRetainedNamespace(t.Context(), namespace.Observation()); err != nil {
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
	if err := recorded.RequireCapturedNamespace(other); err != nil {
		t.Fatalf("new-publication observation envelope did not admit the test key: %v", err)
	}
	for _, history := range []string{"binding", "claims"} {
		if history == "binding" {
			journal = build(nil, fixture.input.Bindings())
		} else {
			journal = build(fixture.input.Claims(), nil)
		}
		view, err := journal.DocumentAt(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := view.RequireRetainedNamespace(t.Context(), other); err == nil {
			t.Fatalf("retained %s followed the broader new-publication key instead of its original namespace", history)
		}
	}
}
