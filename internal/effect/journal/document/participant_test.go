package document

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/target"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestDocumentParticipantUsesMCPGrammarAndParentRewriteArea(t *testing.T) {
	codecs := aggregatecodec.Catalog()
	for _, placement := range aggregate.ImplementedMCPContracts() {
		t.Run(string(placement.ID())+"/"+string(placement.CodecContractID()), func(t *testing.T) {
			contract, err := placement.ProjectionContract("peer")
			if err != nil {
				t.Fatal(err)
			}
			selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
			if err != nil {
				t.Fatal(err)
			}
			prefix := string(placement.ContentPathPrefix())
			syntax := configdocument.JSON
			entry := `{"command":"managed","args":[],"env":{}}`
			switch placement.Target() {
			case target.TargetClaudeCode:
				entry = `{"type":"stdio","command":"managed","args":[],"env":{}}`
			case target.TargetOpenCode:
				entry = `{"type":"local","command":["managed"]}`
			case target.TargetAntigravityCLI:
				entry = `{"command":"managed","args":[]}`
			case target.TargetPi:
				if placement.CodecContractID() == aggregate.MCPCodecPiNativeStdio {
					entry = `{"command":"managed","args":[],"env":{},"enabled":true,"exposure":"codemode"}`
				} else {
					entry = `{"command":"managed","args":[],"env":{},"lifecycle":"lazy","disabled":false}`
				}
			}
			foreign := `"manual" : ` + strings.ReplaceAll(entry, "managed", "keep")
			original := `{"unmanaged":1e+02,"` + strings.TrimPrefix(prefix, "/") + `":{` + foreign + `}}`
			desiredDocument := `{"` + strings.TrimPrefix(prefix, "/") + `":{"peer":` + entry + `}}`
			if placement.Target() == target.TargetCodex {
				syntax = configdocument.TOML
				foreign = "# keep sibling\ncommand='keep'\nargs=[]\n"
				original = "# keep root\nunmanaged = 0x1f\n[" + strings.TrimPrefix(prefix, "/") + ".manual]\n" + foreign
				desiredDocument = "[" + strings.TrimPrefix(prefix, "/") + ".peer]\ncommand='managed'\nargs=[]\n"
			} else if placement.Target() == target.TargetOpenCode {
				syntax = configdocument.JSONC
				foreign = "// keep sibling\n" + foreign
				original = "// keep root\n" + `{"unmanaged":1e+02,"` + strings.TrimPrefix(prefix, "/") + `":{` + foreign + `}}`
			}
			codec, _ := codecs.Lookup(selection.CodecContractID())
			wanted, failure := codec.Read(aggregate.ExistingDocument([]byte(desiredDocument)), selection)
			if failure != nil {
				t.Fatal(failure)
			}
			state, _ := wanted.State(contract)
			contribution, err := placement.Contribution("peer", state.CanonicalProjection())
			if err != nil {
				t.Fatal(err)
			}
			subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), "peer")
			if err != nil {
				t.Fatal(err)
			}
			item, err := aggregate.NewSubjectContribution(subject, contribution)
			if err != nil {
				t.Fatal(err)
			}
			set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
			if err != nil {
				t.Fatal(err)
			}
			assertParticipantRoundTrip(t, codecs, aggregate.ExistingDocument([]byte(original)), set, syntax, prefix, foreign, state.CanonicalProjection())
		})
	}
}

func TestDocumentParticipantUsesHookGrammarAndRewriteArea(t *testing.T) {
	codecs := aggregatecodec.Catalog()
	for _, placement := range aggregate.ImplementedHookPlacements() {
		t.Run(string(placement.ID()), func(t *testing.T) {
			canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{Event: "Stop", Type: "command", Command: "true"})
			if err != nil {
				t.Fatal(err)
			}
			contribution, err := placement.Contribution(canonical)
			if err != nil {
				t.Fatal(err)
			}
			id, err := entity.New(entity.KindHook, "managed")
			if err != nil {
				t.Fatal(err)
			}
			subject, err := topologyhook.ProjectionSubjectID(id, placement.Target(), placement.Scope())
			if err != nil {
				t.Fatal(err)
			}
			item, err := aggregate.NewSubjectContribution(subject, contribution)
			if err != nil {
				t.Fatal(err)
			}
			set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
			if err != nil {
				t.Fatal(err)
			}
			original := aggregate.ExistingDocument([]byte(`{"unmanaged":1e+02,"hooks":{}}`))
			selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
			if err != nil {
				t.Fatal(err)
			}
			codec, _ := codecs.Lookup(selection.CodecContractID())
			wanted, failure := codec.Read(aggregate.ExistingDocument([]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"true"}]}]}}`)), selection)
			if failure != nil {
				t.Fatal(failure)
			}
			state, _ := wanted.State(contribution.Contract())
			assertParticipantRoundTrip(t, codecs, original, set, configdocument.JSON, aggregate.HooksContentPath, `"unmanaged":1e+02`, state.CanonicalProjection())
		})
	}
}

func assertParticipantRoundTrip(t *testing.T, codecs aggregate.CodecCatalog, original aggregate.Document, set aggregate.ContributionSet, syntax configdocument.Syntax, prefix, foreign, wanted string) {
	t.Helper()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{set.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := Participant(selection, codecs)
	if err != nil {
		t.Fatal(err)
	}
	areas := participant.RewriteAreas()
	if participant.Syntax() != syntax || len(areas) != 1 || areas[0].String() != prefix {
		t.Fatal("codec selection supplied a different native rewrite envelope", participant.Syntax(), areas)
	}
	codec, _ := codecs.Lookup(selection.CodecContractID())
	baseline, failure := codec.Read(original, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	selected, err := compose.NewProjectionBaseline(codec, baseline, participant.Syntax(), areas)
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: codec.MaximumDocumentBytes(), MaximumDepth: 64}
	restored, err := compose.Restore(t.Context(), original, limits, []compose.ProjectionBaseline{selected})
	if err != nil || !restored.Document().Equal(original) {
		t.Fatal("participant reconstruction changed an unchanged native document", err)
	}
	actual, failure := codec.Read(restored.Document(), selection)
	if failure != nil || !actual.Equal(baseline) {
		t.Fatal("participant reconstruction changed its managed projection", failure)
	}

	state, _ := baseline.State(set.Contract())
	intent, err := aggregate.NewProjectionIntent(state, &set)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(baseline, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, participant.Syntax(), areas)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{edit})
	if err != nil {
		t.Fatal(err)
	}
	actual, failure = codec.Read(candidate.Document(), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	changed, _ := actual.State(set.Contract())
	if !changed.Present() || changed.CanonicalProjection() != wanted || !strings.Contains(string(candidate.Document().Content()), foreign) {
		t.Fatal("declared rewrite layout did not apply the selection while preserving foreign syntax")
	}
	restored, err = compose.Restore(t.Context(), candidate.Document(), limits, []compose.ProjectionBaseline{selected})
	if err != nil {
		t.Fatal(err)
	}
	actual, failure = codec.Read(restored.Document(), selection)
	if failure != nil || !actual.Equal(baseline) || !strings.Contains(string(restored.Document().Content()), foreign) {
		t.Fatal("declared rewrite layout did not restore its managed projection and foreign syntax", failure)
	}
}
