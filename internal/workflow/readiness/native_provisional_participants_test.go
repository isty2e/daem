package readiness

import (
	"bytes"
	"context"
	"os"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeParticipantsRetainExactBeforeMembership(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativePlanningFixture(t, scope, "extension-settings/host.json")
			projection, err := fixture.input.Documents.Ownership(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			alias := fixture.observation.Aliases[0]
			participants, err := projection.Participants(t.Context(), alias)
			if err != nil || participants.Validate() != nil {
				t.Fatal("exact document did not retain participant facts", err)
			}
			before := fixture.observation.Before.ManagedAggregates()[0].Subject()
			if len(participants.BeforeSubjects()) != 1 || participants.BeforeSubjects()[0] != before || len(participants.CurrentBindings()) != 1 || !participants.CurrentBindings()[0].Owner().Equal(fixture.observation.Owner) {
				t.Fatal("participant facts lost actual before ownership")
			}
			claimView, err := projection.Document(t.Context(), alias)
			if err != nil || !slices.Equal(claimView.Participants().BeforeSubjects(), participants.BeforeSubjects()) || !slices.Equal(claimView.Participants().Aliases(), participants.Aliases()) || len(claimView.Participants().CurrentBindings()) != 1 || !claimView.Participants().CurrentBindings()[0].Equal(participants.CurrentBindings()[0]) {
				t.Fatal("exact claim and participant views disagree", err)
			}

			participants.BeforeSubjects()[0] = topology.SubjectID{}
			participants.CurrentBindings()[0] = ownership.DocumentBinding{}
			if participants.BeforeSubjects()[0] != before || participants.CurrentBindings()[0].Validate() != nil {
				t.Fatal("participant getters exposed mutable membership")
			}
			if value, err := projection.Participants(nil, alias); err == nil || value.Validate() == nil {
				t.Fatal("missing context yielded usable participant facts")
			}
		})
	}
}

func TestFirstMissingNativeDocumentKeepsParticipantFactsWithoutExactClaims(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, hasForeign := range []bool{false, true} {
			t.Run(string(scope)+map[bool]string{false: "/empty", true: "/foreign"}[hasForeign], func(t *testing.T) {
				fixture := newNativePlanningFixture(t, scope, "extension-settings/Café.json")
				empty, err := durable.NewSnapshot(durable.SnapshotInput{})
				if err != nil {
					t.Fatal(err)
				}
				var expected []ownership.DocumentBinding
				if hasForeign {
					expected = append(expected, nativeForeignProjectBinding(t, fixture, outputtest.Parse(t, "shared/peer.json"), fixture.hostPath))
				}
				fixture.persistBefore(t, empty, nil)
				if hasForeign {
					registry, err := ownership.NewRegistry(nil, expected...)
					if err != nil {
						t.Fatal(err)
					}
					content, err := ownershipstore.Marshal(registry)
					if err != nil {
						t.Fatal(err)
					}
					requireNativePreparationFile(t, fixture.registryPath, content)
				}
				if err := os.Remove(fixture.hostPath); err != nil {
					t.Fatal(err)
				}
				fixture.observe(t)
				projection, err := fixture.input.Documents.Ownership(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				alias := fixture.observation.Aliases[0]
				registryBefore, err := os.ReadFile(fixture.registryPath)
				if err != nil {
					t.Fatal(err)
				}
				reads, budget := fixture.reader.reads, fixture.observation.Budget.RemainingBytes()
				participants, err := projection.Participants(t.Context(), alias)
				if err != nil || participants.Validate() != nil || !participants.Owner().Equal(fixture.observation.Owner) || len(participants.BeforeSubjects()) != 0 || len(participants.Aliases()) != 1 || participants.Aliases()[0] != alias || len(participants.CurrentBindings()) != len(expected) {
					t.Fatal("first missing document lost actual participant facts", err)
				}
				if hasForeign && !participants.CurrentBindings()[0].Equal(expected[0]) {
					t.Fatal("provisional participant lookup changed stored foreign identity")
				}
				participants.Aliases()[0] = aggregate.DocumentAddress{}
				if participants.Aliases()[0] != alias {
					t.Fatal("participant aliases were externally mutable")
				}
				if scope == target.ScopeGlobal {
					observed, err := projection.Global(t.Context(), alias, output.WholePathFootprint())
					if err != nil {
						t.Fatal(err)
					}
					if _, provisional := observed.ProvisionalPath(); provisional {
						if _, exact := observed.ExactAddress(); exact {
							t.Fatal("participant metadata minted current exact claim authority")
						}
						if _, err := projection.Document(t.Context(), alias); err == nil {
							t.Fatal("first provisional capture invented an exact claim comparison")
						}
					}
				}
				registryAfter, err := os.ReadFile(fixture.registryPath)
				if err != nil || !bytes.Equal(registryBefore, registryAfter) || fixture.reader.reads != reads || fixture.observation.Budget.RemainingBytes() != budget || fixture.observation.Budget.RequireCapturePending() != nil {
					t.Fatal("participant projection reread/mutated Registry or allocation", err)
				}
				if _, err := os.Lstat(fixture.hostPath); !os.IsNotExist(err) {
					t.Fatal("metadata observation recreated the missing document", err)
				}
				if value, err := projection.Participants(t.Context(), aggregate.DocumentAddress{}); err == nil || value.Validate() == nil {
					t.Fatal("unknown alias yielded a usable participant prefix")
				}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if value, err := projection.Participants(ctx, alias); err == nil || value.Validate() == nil {
					t.Fatal("canceled participant projection yielded usable facts")
				}
				if err := fixture.observation.Budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}); err != nil {
					t.Fatal(err)
				}
				if value, err := projection.Participants(t.Context(), alias); err == nil || value.Validate() == nil {
					t.Fatal("admitted allocation yielded new participant facts")
				}
			})
		}
	}
}
