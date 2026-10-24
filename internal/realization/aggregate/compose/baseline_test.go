package compose_test

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
)

func TestLoadedDocumentReconstructsCrossCodecBaselinesAndPreservesFreshOutside(t *testing.T) {
	for _, original := range []aggregate.Document{
		aggregate.AbsentDocument(),
		aggregate.ExistingDocument([]byte("# original\nmodel='local-edit'\nunmanaged=2026-10-24\n[mcp_servers.foreign]\ncommand='keep'\nargs=[]\n")),
	} {
		native := nativePort(t)
		model := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "model"), `"review"`)
		mcp, server := mcpSet(t, "search")
		modelSelection, serverSelection := selectSets(t, model), selectSets(t, server)
		participants := []compose.Participant{
			participant(t, native, modelSelection, path(t, "model")),
			participant(t, mcp, serverSelection, path(t, "mcp_servers")),
		}
		baselines, err := compose.ReadBaselines(t.Context(), original, limits, participants)
		if err != nil {
			t.Fatal(err)
		}
		if len(baselines) != 2 || !baselines[0].Snapshot().Equal(observe(t, native, original, modelSelection)) ||
			!baselines[1].Snapshot().Equal(observe(t, mcp, original, serverSelection)) {
			t.Fatal("loaded original did not retain each codec's actual complete baseline")
		}
		candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{
			edit(t, native, desiredPlan(t, baselines[0].Snapshot(), map[output.KeyPath]*aggregate.ContributionSet{path(t, "model"): &model}), path(t, "model")),
			edit(t, mcp, desiredPlan(t, baselines[1].Snapshot(), map[output.KeyPath]*aggregate.ContributionSet{server.Address().KeyPath(): &server}), path(t, "mcp_servers")),
		})
		if err != nil {
			t.Fatal(err)
		}
		expected, err := compose.ReadBaselines(t.Context(), candidate.Document(), limits, participants)
		if err != nil || !expected[0].Snapshot().Equal(candidate.Expected()[0]) || !expected[1].Snapshot().Equal(candidate.Expected()[1]) {
			t.Fatalf("loaded candidate did not reconstruct actual expected observations: %v", err)
		}

		current := aggregate.ExistingDocument(append([]byte("# fresh outside\nfresh=true\n"), candidate.Document().Content()...))
		restored, err := compose.Restore(t.Context(), current, limits, baselines)
		if err != nil {
			t.Fatal(err)
		}
		if !nativeValue(t, restored.Document(), path(t, "fresh")).Equal(value(t, "true")) {
			t.Fatal("selected recovery replaced the whole document and lost fresh outside data")
		}
		actual := observe(t, native, restored.Document(), modelSelection).States()[0]
		if !actual.SelectedValueEqual(baselines[0].Snapshot().States()[0]) {
			t.Fatal("selected native recovery used desired/managed data instead of actual original data")
		}
		if original.Exists() && !nativeValue(t, restored.Document(), path(t, "model")).Equal(value(t, `"local-edit"`)) {
			t.Fatal("replacement recovery did not restore the actual pre-replacement local value")
		}
		assertComments(t, current, restored.Document())
	}
}

func TestLoadedNativeBaselinesRetainRootBlockerAndLogicalAliasFacts(t *testing.T) {
	native := nativePort(t)
	parent := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), `"desired"`)
	child := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a", "b"), "true")
	alias := nativeSet(t, native, target.ScopeGlobal, "@codex-home/config.toml", path(t, "other"), "42")
	participants := []compose.Participant{
		participant(t, native, selectSets(t, parent, child), path(t, "a")),
		participant(t, native, selectSets(t, alias), path(t, "other")),
	}
	for _, original := range []aggregate.Document{
		aggregate.AbsentDocument(),
		aggregate.ExistingDocument(nil),
		aggregate.ExistingDocument([]byte("a='actual'\nother=1\n")),
		aggregate.ExistingDocument([]byte("a={b=true, nested=2026-10-24}\nother=1\n")),
		aggregate.ExistingDocument([]byte("a={}\nother=1\n")),
	} {
		baselines, err := compose.ReadBaselines(t.Context(), original, limits, participants)
		if err != nil {
			t.Fatal(err)
		}
		for index, baseline := range baselines {
			if !baseline.Snapshot().Equal(observe(t, native, original, participants[index].Selection())) || baseline.Snapshot().DocumentExisted() != original.Exists() {
				t.Fatal("reconstructed baseline lost root presence, selected value/blocker or logical identity")
			}
		}
		if participants[0].Selection().DocumentAddress() == participants[1].Selection().DocumentAddress() {
			t.Fatal("fixture collapsed distinct logical aliases")
		}
	}
	areas := participants[0].RewriteAreas()
	areas[0] = path(t, "unselected")
	if participants[0].RewriteAreas()[0] != path(t, "a") || participants[0].Syntax() != configdocument.TOML {
		t.Fatal("caller mutation changed descriptor facts")
	}
}

func TestReadBaselinesRefusesInvalidMembersAndReturnsNoPartialGroup(t *testing.T) {
	native := nativePort(t)
	a := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), "1")
	b := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "b"), "2")
	aSelection, bSelection := selectSets(t, a), selectSets(t, b)
	aParticipant := participant(t, native, aSelection, path(t, "a"))
	bParticipant := participant(t, native, bSelection, path(t, "b"))
	original := aggregate.ExistingDocument([]byte("a=0\nb=0\n"))
	for _, participants := range [][]compose.Participant{nil, {{}}, {aParticipant, aParticipant}} {
		assertBaselineRefused(t, t.Context(), original, limits, participants)
	}
	assertBaselineRefused(t, nil, original, limits, []compose.Participant{aParticipant})
	assertBaselineRefused(t, t.Context(), original, configdocument.Limits{MaximumBytes: 1, MaximumDepth: 1}, []compose.Participant{aParticipant})
	assertBaselineRefused(t, t.Context(), aggregate.ExistingDocument([]byte("a=")), limits, []compose.Participant{aParticipant})
	assertBaselineRefused(t, t.Context(), aggregate.ExistingDocument([]byte("a='blocked'")), limits, []compose.Participant{
		participant(t, native, selectSets(t, nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a", "b"), "true")), path(t, "a", "b")),
	})
	jsonParticipant, err := compose.NewParticipant(native, bSelection, configdocument.JSON, []output.KeyPath{path(t, "b")})
	if err != nil {
		t.Fatal(err)
	}
	assertBaselineRefused(t, t.Context(), original, limits, []compose.Participant{aParticipant, jsonParticipant})

	wrong := wrongBaselineCodec{Codec: native, snapshot: observe(t, native, original, aSelection)}
	assertBaselineRefused(t, t.Context(), original, limits, []compose.Participant{aParticipant, participant(t, wrong, bSelection, path(t, "b"))})
	missing := wrongBaselineCodec{Codec: native, snapshot: observe(t, native, aggregate.AbsentDocument(), bSelection)}
	assertBaselineRefused(t, t.Context(), original, limits, []compose.Participant{aParticipant, participant(t, missing, bSelection, path(t, "b"))})

	ctx, cancel := context.WithCancel(t.Context())
	canceling := cancelBaselineRead{Codec: native, cancel: cancel}
	baselines, err := compose.ReadBaselines(ctx, original, limits, []compose.Participant{participant(t, canceling, aSelection, path(t, "a")), bParticipant})
	if !errors.Is(err, context.Canceled) || baselines != nil {
		t.Fatal("cancellation between participants published a partial baseline group")
	}
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	assertBaselineRefused(t, ctx, original, limits, []compose.Participant{aParticipant})
}

type wrongBaselineCodec struct {
	aggregate.Codec
	snapshot aggregate.Snapshot
}

func (codec wrongBaselineCodec) Read(aggregate.Document, aggregate.Selection) (aggregate.Snapshot, *aggregate.CodecFailure) {
	return codec.snapshot, nil
}

type cancelBaselineRead struct {
	aggregate.Codec
	cancel context.CancelFunc
}

func (codec cancelBaselineRead) Read(document aggregate.Document, selection aggregate.Selection) (aggregate.Snapshot, *aggregate.CodecFailure) {
	snapshot, failure := codec.Codec.Read(document, selection)
	codec.cancel()
	return snapshot, failure
}

func participant(t testing.TB, codec aggregate.Codec, selection aggregate.Selection, areas ...output.KeyPath) compose.Participant {
	t.Helper()
	participant, err := compose.NewParticipant(codec, selection, configdocument.TOML, areas)
	if err != nil {
		t.Fatal(err)
	}
	return participant
}

func assertBaselineRefused(t testing.TB, ctx context.Context, original aggregate.Document, bounds configdocument.Limits, participants []compose.Participant) {
	t.Helper()
	baselines, err := compose.ReadBaselines(ctx, original, bounds, participants)
	if err == nil || baselines != nil {
		t.Fatal("invalid reconstruction published a complete or partial baseline group")
	}
}

func TestReadBaselinesRejectsIncompleteSelectionInsideOneRewriteArea(t *testing.T) {
	native := nativePort(t)
	parent := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), `"desired"`)
	child := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a", "b"), "true")
	original := aggregate.ExistingDocument([]byte("a={b=false}\n"))
	partial := wrongBaselineCodec{Codec: native, snapshot: observe(t, native, original, selectSets(t, parent))}
	owner := participant(t, partial, selectSets(t, parent, child), path(t, "a"))
	assertBaselineRefused(t, t.Context(), original, limits, []compose.Participant{owner})
}

func TestReadBaselinesChecksCancellationAfterFinalParticipant(t *testing.T) {
	native := nativePort(t)
	set := nativeSet(t, native, target.ScopeProject, ".codex/config.toml", path(t, "a"), "1")
	ctx, cancel := context.WithCancel(t.Context())
	codec := cancelBaselineRead{Codec: native, cancel: cancel}
	owner := participant(t, codec, selectSets(t, set), path(t, "a"))
	baselines, err := compose.ReadBaselines(ctx, aggregate.ExistingDocument([]byte("a=0\n")), limits, []compose.Participant{owner})
	if !errors.Is(err, context.Canceled) || baselines != nil {
		t.Fatal("cancellation in the last codec read published a baseline group")
	}
}
