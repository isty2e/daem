package journal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
)

type selectedGroupCaptureFixture struct {
	roots     journalnamespace.Roots
	fixture   nativeRecordFixture
	namespace journalnamespace.DocumentNamespace
	input     selectedDocumentCaptureInput
	subjects  documentCaptureSubjects
	reader    *nativeNamespaceReader
	path      string
	original  aggregate.Document
}

func (capture selectedGroupCaptureFixture) recordForGroups(t *testing.T, groups []journalrecord.SealedDocumentGroup) journalrecord.Native {
	t.Helper()
	authority, err := capture.roots.Manifest.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := journalrecord.NewNativeCapture(capture.input.OperationID(), time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), capture.input.Owner(), provenance, capture.fixture.before, capture.fixture.after, groups)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func (capture selectedGroupCaptureFixture) recordedGroup(t *testing.T, group journalrecord.SealedDocumentGroup) journalrecord.Document {
	t.Helper()
	recorded, err := capture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group}).DocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	return recorded
}

func selectedCaptureFixture(t *testing.T, fixture nativeRecordFixture, original aggregate.Document, edits []compose.ProjectionEdit, order *pipackage.OrderObservation, roots journalnamespace.Roots) selectedGroupCaptureFixture {
	t.Helper()
	alias := fixture.claims.alias
	if roots.Manifest.Root == nil {
		roots.Manifest = nativeNamespaceRoot(t, t.TempDir())
	}
	root := roots.Manifest
	if alias.Scope() == target.ScopeGlobal {
		if alias.Target() == target.TargetPi {
			if roots.PiAgent.Root == nil {
				roots.PiAgent = nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "pi"))
			}
			root = roots.PiAgent
		} else {
			if roots.CodexHome.Root == nil {
				roots.CodexHome = nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "codex"))
			}
			root = roots.CodexHome
		}
	}
	path := filepath.Join(root.SelectedPath, filepath.FromSlash(alias.AggregateRoot().RelativePath()))
	if original.Exists() {
		requireWholeGroupWrite(t, path, original.Content(), 0o644)
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, reader, budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := namespace.Observation().Exact()
	if !known {
		t.Fatal("fixture path requires normalization-stable exact authority")
	}
	input := selectedDocumentCaptureInput{edits: edits, order: order, CaptureInput: journalobservation.NewCaptureInput(journalobservation.CaptureInputSpec{Owner: fixture.claims.owner, OperationID: fixture.journal.OperationID(), GroupIndex: 2, Limits: fixture.read.limits, Budget: budget})}
	indexes, err := journaldocument.IndexSnapshots(t.Context(), fixture.before, fixture.after)
	if err != nil {
		t.Fatal(err)
	}
	var claims [2][]ownership.Claim
	var bindings [2]ownership.DocumentBindingValue
	for side, index := range []journaldocument.SnapshotIndex{indexes.Before(), indexes.After()} {
		footprints, err := index.Footprints(t.Context(), namespace.Addresses())
		if err != nil {
			t.Fatal(err)
		}
		for footprint := range footprints {
			claims[side] = append(claims[side], documentActiveClaim(t, documentClaimFixture{owner: input.Owner(), exact: exact}, footprint))
		}
		destinations, err := index.NativeDestinations(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, present := destinations[alias.AggregateRoot()]; present {
			binding, err := ownership.NewDocumentBinding(input.Owner(), alias.AggregateRoot(), exact)
			if err != nil {
				t.Fatal(err)
			}
			bindings[side], err = ownership.PresentDocumentBinding(binding)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(claims[0])+len(claims[1]) != 0 {
		transition, err := ownershipmutation.NewDocumentClaimTransition(claims[0], claims[1], input.OperationID())
		if err != nil {
			t.Fatal(err)
		}
		input.CaptureInput = reviseCaptureInput(input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Claims = &transition })
	}
	if !bindings[0].Equal(ownership.NoDocumentBinding()) || !bindings[1].Equal(ownership.NoDocumentBinding()) {
		intent, err := ownership.NewExactDocumentBindingIntent(input.Owner(), alias.AggregateRoot(), bindings[0], bindings[1])
		if err != nil {
			t.Fatal(err)
		}
		input.CaptureInput = reviseCaptureInput(input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) {
			spec.Bindings = []ownership.DocumentBindingIntent{intent}
		})
	}
	subjects, err := indexDocumentCaptureSubjects(t.Context(), fixture.before, fixture.after)
	if err != nil {
		t.Fatal(err)
	}
	return selectedGroupCaptureFixture{roots: roots, fixture: fixture, namespace: namespace, input: input, subjects: subjects, reader: reader, path: path, original: original}
}

func selectedCaptureEdits(t *testing.T, fixture nativeRecordFixture, original aggregate.Document, contracts []aggregate.ProjectionContract, desired []durable.ManagedAggregateState) []compose.ProjectionEdit {
	t.Helper()
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	codec, found := fixture.read.codecs.Lookup(selection.CodecContractID())
	if !found {
		t.Fatal("selected fixture codec missing")
	}
	baseline, failure := codec.Read(original, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	var intents []aggregate.ProjectionIntent
	for _, state := range baseline.States() {
		var values []aggregate.SubjectContribution
		for _, row := range desired {
			if row.Contribution().Contract().Equal(state.Contract()) {
				value, err := aggregate.NewSubjectContribution(row.Subject(), row.Contribution())
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, value)
			}
		}
		var set *aggregate.ContributionSet
		if len(values) != 0 {
			value, err := aggregate.NewContributionSet(values)
			if err != nil {
				t.Fatal(err)
			}
			set = &value
		}
		intent, err := aggregate.NewProjectionIntent(state, set)
		if err != nil {
			t.Fatal(err)
		}
		intents = append(intents, intent)
	}
	plan, err := aggregate.NewPlan(baseline, intents)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := journaldocument.Participant(selection, fixture.read.codecs)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, participant.Syntax(), participant.RewriteAreas())
	if err != nil {
		t.Fatal(err)
	}
	return []compose.ProjectionEdit{edit}
}
