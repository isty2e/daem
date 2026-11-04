package journal_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestPrepareWholeDocumentDerivesAllMutationImages(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				fixture := newWholePreparationFixture(t, selected, scope)
				for _, kind := range []string{"create", "replace", "record", "remove"} {
					t.Run(kind, func(t *testing.T) {
						input := fixture.input()
						set := fixture.payloads(t, fixture.expected, 0o600)
						expected, expectedMode := aggregate.ExistingDocument(fixture.expected), recovery.NewPermissionMode(0o600)
						switch kind {
						case "create":
							input.Before, input.BeforeMode = aggregate.AbsentDocument(), nil
						case "record":
							set = payload.PayloadSet{}
							expected, expectedMode = input.Before, input.BeforeMode
						case "remove":
							set = payload.PayloadSet{}
							expected, expectedMode = aggregate.AbsentDocument(), nil
						}
						comparison, err := journal.PrepareWholeDocument(t.Context(), input, fixture.mutation(t, kind), set)
						if err != nil {
							t.Fatal(err)
						}
						before, err := comparison.Match(t.Context(), input.Before, input.BeforeMode)
						if err != nil || !before.MatchesBefore() || !before.Recoverable() {
							t.Fatalf("actual original was not retained: %+v, %v", before, err)
						}
						after, err := comparison.Match(t.Context(), expected, expectedMode)
						if err != nil || !after.MatchesExpected() || !after.Recoverable() {
							t.Fatalf("derived expected image differs: %+v, %v", after, err)
						}
						third, err := comparison.Match(t.Context(), aggregate.ExistingDocument([]byte("external third bytes")), recovery.NewPermissionMode(0o600))
						if err != nil || third.Recoverable() {
							t.Fatalf("whole comparison accepted external bytes: %+v, %v", third, err)
						}
					})
				}
			})
		}
	}
}

func TestPrepareWholeDocumentRejectsMisjoinedFactsWithoutComparison(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*testing.T, *wholePreparationFixture, *journal.WholeDocumentPreparationInput, *journal.ManagedPathMutation, *payload.PayloadSet)
	}{
		{"zero mutation", func(_ *testing.T, _ *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			*mutation = journal.ManagedPathMutation{}
		}},
		{"create over present", func(t *testing.T, f *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			*mutation = f.mutation(t, "create")
		}},
		{"replace absent", func(_ *testing.T, _ *wholePreparationFixture, input *journal.WholeDocumentPreparationInput, _ *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			input.Before, input.BeforeMode = aggregate.AbsentDocument(), nil
		}},
		{"missing original mode", func(_ *testing.T, _ *wholePreparationFixture, input *journal.WholeDocumentPreparationInput, _ *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			input.BeforeMode = nil
		}},
		{"wrong live bytes", func(_ *testing.T, _ *wholePreparationFixture, input *journal.WholeDocumentPreparationInput, _ *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			input.Before = aggregate.ExistingDocument([]byte("stale observation"))
		}},
		{"wrong live executable class", func(_ *testing.T, _ *wholePreparationFixture, input *journal.WholeDocumentPreparationInput, _ *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			input.BeforeMode = recovery.NewPermissionMode(0o744)
		}},
		{"missing payload", func(_ *testing.T, _ *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, _ *journal.ManagedPathMutation, set *payload.PayloadSet) {
			*set = payload.PayloadSet{}
		}},
		{"foreign subject payload", func(t *testing.T, f *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, _ *journal.ManagedPathMutation, set *payload.PayloadSet) {
			f.subject = newWholePreparationFixture(t, target.TargetCodex, target.ScopeGlobal).subject
			*set = f.payloads(t, f.expected, 0o600)
		}},
		{"wrong desired bytes", func(t *testing.T, f *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, _ *journal.ManagedPathMutation, set *payload.PayloadSet) {
			*set = f.payloads(t, []byte("wrong payload"), 0o600)
		}},
		{"wrong desired mode", func(t *testing.T, f *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, _ *journal.ManagedPathMutation, set *payload.PayloadSet) {
			*set = f.payloads(t, f.expected, 0o644)
		}},
		{"record desired mismatch", func(t *testing.T, f *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			*mutation = requireWholeMutation(t)(journal.NewManagedPathRecordMutation(f.subject, f.consumers(), f.scope, f.destination, artifact.HashFileContent(f.expected), artifact.HashFileContent(f.before), realization.PathProjectionFile, 0o644, &f.previous))
		}},
		{"record mode mismatch", func(t *testing.T, f *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			*mutation = requireWholeMutation(t)(journal.NewManagedPathRecordMutation(f.subject, f.consumers(), f.scope, f.destination, artifact.HashFileContent(f.before), artifact.HashFileContent(f.before), realization.PathProjectionFile, 0o600, &f.previous))
		}},
		{"removal baseline mode mismatch", func(t *testing.T, f *wholePreparationFixture, input *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			*mutation = f.mutation(t, "remove")
			input.BeforeMode = recovery.NewPermissionMode(0o640)
		}},
		{"removal baseline hash mismatch", func(t *testing.T, f *wholePreparationFixture, input *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			input.Before = aggregate.ExistingDocument([]byte("changed since baseline"))
			*mutation = requireWholeMutation(t)(journal.NewManagedPathRemoveMutation(f.previous, artifact.HashFileContent(input.Before.Content())))
		}},
		{"unsplit retarget", func(t *testing.T, f *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, _ *payload.PayloadSet) {
			other, err := output.Parse(".pi/prompts/other.md")
			if err != nil {
				t.Fatal(err)
			}
			*mutation = requireWholeMutation(t)(journal.NewManagedPathReplaceMutation(f.subject, f.consumers(), f.scope, other, artifact.HashFileContent(f.expected), artifact.HashFileContent(f.before), realization.PathProjectionFile, 0o600, f.previous))
		}},
		{"foreign subject family", func(t *testing.T, f *wholePreparationFixture, input *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, set *payload.PayloadSet) {
			other, err := topology.NewSubjectID(topology.SubjectProjection, "skill", "oracle")
			if err != nil {
				t.Fatal(err)
			}
			*mutation = requireWholeMutation(t)(journal.NewManagedPathCreateMutation(other, f.consumers(), f.scope, f.destination, artifact.HashFileContent(f.expected), realization.PathProjectionFile, 0o600, nil))
			input.Before, input.BeforeMode = aggregate.AbsentDocument(), nil
			f.subject = other
			*set = f.payloads(t, f.expected, 0o600)
		}},
		{"executable native output", func(t *testing.T, f *wholePreparationFixture, _ *journal.WholeDocumentPreparationInput, mutation *journal.ManagedPathMutation, set *payload.PayloadSet) {
			*mutation = requireWholeMutation(t)(journal.NewManagedPathReplaceMutation(f.subject, f.consumers(), f.scope, f.destination, artifact.HashFileContentWithExecutable(f.expected, true), artifact.HashFileContent(f.before), realization.PathProjectionFile, 0o700, f.previous))
			*set = f.payloads(t, f.expected, 0o700)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWholePreparationFixture(t, target.TargetPi, target.ScopeProject)
			input, mutation, set := fixture.input(), fixture.mutation(t, "replace"), fixture.payloads(t, fixture.expected, 0o600)
			test.edit(t, &fixture, &input, &mutation, &set)
			comparison, err := journal.PrepareWholeDocument(t.Context(), input, mutation, set)
			if err == nil || comparison.Validate() == nil {
				t.Fatalf("invalid capture input produced usable comparison: %v", err)
			}
		})
	}
}

func TestPrepareWholeDocumentPreservesDriftedOriginalAndEmptyFile(t *testing.T) {
	fixture := newWholePreparationFixture(t, target.TargetPi, target.ScopeProject)
	fixture.expected = nil
	fixture.before = []byte("drifted original\x00\xff\r\n")
	comparison, err := journal.PrepareWholeDocument(t.Context(), fixture.input(), fixture.mutation(t, "replace"), fixture.payloads(t, nil, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	match, err := comparison.Match(t.Context(), aggregate.ExistingDocument(nil), recovery.NewPermissionMode(0o600))
	if err != nil || !match.MatchesExpected() {
		t.Fatalf("empty existing expected image was lost: %+v, %v", match, err)
	}
	absent, err := comparison.Match(t.Context(), aggregate.AbsentDocument(), nil)
	if err != nil || absent.Recoverable() {
		t.Fatalf("empty file was collapsed into absence: %+v, %v", absent, err)
	}
	before, err := comparison.Match(t.Context(), fixture.input().Before, fixture.input().BeforeMode)
	if err != nil || !before.MatchesBefore() {
		t.Fatalf("actual drifted original was replaced by old managed hash: %+v, %v", before, err)
	}

	input := fixture.input()
	input.BeforeMode = recovery.NewPermissionMode(0o744)
	mutation := requireWholeMutation(t)(journal.NewManagedPathReplaceMutation(fixture.subject, fixture.consumers(), fixture.scope, fixture.destination, artifact.HashFileContent(nil), artifact.HashFileContentWithExecutable(fixture.before, true), realization.PathProjectionFile, 0o600, fixture.previous))
	executableDrift, err := journal.PrepareWholeDocument(t.Context(), input, mutation, fixture.payloads(t, nil, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	before, err = executableDrift.Match(t.Context(), input.Before, input.BeforeMode)
	if err != nil || !before.MatchesBefore() {
		t.Fatalf("actual original executable-class drift was discarded: %+v, %v", before, err)
	}
}

func TestPrepareWholeDocumentRejectsOversizeBeforeCopyAndCancellation(t *testing.T) {
	fixture := newWholePreparationFixture(t, target.TargetPi, target.ScopeProject)
	fixture.expected = bytes.Repeat([]byte{'x'}, 8<<20)
	input := fixture.input()
	input.Before, input.BeforeMode = aggregate.AbsentDocument(), nil
	mutation, set := fixture.mutation(t, "create"), fixture.payloads(t, fixture.expected, 0o600)
	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			comparison, err := journal.PrepareWholeDocument(b.Context(), input, mutation, set)
			if err == nil || comparison.Validate() == nil {
				b.Fatal("oversized payload returned usable comparison")
			}
		}
	})
	t.Logf("oversized payload refusal: %d B/op", result.AllocedBytesPerOp())
	if result.AllocedBytesPerOp() >= input.Limits.MaximumBytes {
		t.Fatal("payload was copied before byte-limit refusal")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	comparison, err := journal.PrepareWholeDocument(ctx, input, mutation, set)
	if !errors.Is(err, context.Canceled) || comparison.Validate() == nil {
		t.Fatalf("cancellation returned usable comparison: %v", err)
	}
	comparison, err = journal.PrepareWholeDocument(nil, input, mutation, set)
	if err == nil || comparison.Validate() == nil {
		t.Fatal("missing context returned usable comparison")
	}
}

type wholePreparationFixture struct {
	subject          topology.SubjectID
	selected         target.Target
	scope            target.Scope
	destination      output.Destination
	before, expected []byte
	previous         durable.ManagedPathState
}

func newWholePreparationFixture(t *testing.T, selected target.Target, scope target.Scope) wholePreparationFixture {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "document")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, selected, scope)
	if err != nil {
		t.Fatal(err)
	}
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: selected, Scope: scope, Path: "prompts/review.md", Ownership: "document"})
	if err != nil {
		t.Fatal(err)
	}
	before := []byte("# original\r\nopaque \x00\xff\n")
	previous, err := durable.NewManagedPathState(subject, []target.Target{selected}, scope, placement.Destination(), artifact.HashFileContent(before), realization.PathProjectionFile, realization.PathPermissionsExact, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return wholePreparationFixture{subject: subject, selected: selected, scope: scope, destination: placement.Destination(), before: before, expected: []byte("# exact expected\r\n\x00\xff\n"), previous: previous}
}

func (fixture wholePreparationFixture) input() journal.WholeDocumentPreparationInput {
	return journal.WholeDocumentPreparationInput{Before: aggregate.ExistingDocument(fixture.before), BeforeMode: recovery.NewPermissionMode(0o644), Limits: configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}}
}

func (fixture wholePreparationFixture) consumers() []target.Target {
	return []target.Target{fixture.selected}
}

func (fixture wholePreparationFixture) mutation(t *testing.T, kind string) journal.ManagedPathMutation {
	t.Helper()
	switch kind {
	case "create":
		return requireWholeMutation(t)(journal.NewManagedPathCreateMutation(fixture.subject, fixture.consumers(), fixture.scope, fixture.destination, artifact.HashFileContent(fixture.expected), realization.PathProjectionFile, 0o600, nil))
	case "replace":
		return requireWholeMutation(t)(journal.NewManagedPathReplaceMutation(fixture.subject, fixture.consumers(), fixture.scope, fixture.destination, artifact.HashFileContent(fixture.expected), artifact.HashFileContent(fixture.before), realization.PathProjectionFile, 0o600, fixture.previous))
	case "record":
		return requireWholeMutation(t)(journal.NewManagedPathRecordMutation(fixture.subject, fixture.consumers(), fixture.scope, fixture.destination, artifact.HashFileContent(fixture.before), artifact.HashFileContent(fixture.before), realization.PathProjectionFile, 0o644, &fixture.previous))
	case "remove":
		return requireWholeMutation(t)(journal.NewManagedPathRemoveMutation(fixture.previous, artifact.HashFileContent(fixture.before)))
	default:
		t.Fatalf("unknown fixture mutation %q", kind)
		return journal.ManagedPathMutation{}
	}
}

func (fixture wholePreparationFixture) payloads(t *testing.T, content []byte, mode os.FileMode) payload.PayloadSet {
	t.Helper()
	value, err := payload.NewFilePayload(fixture.subject, content, mode)
	if err != nil {
		t.Fatal(err)
	}
	set, err := payload.NewPayloadSet([]payload.Payload{value}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func requireWholeMutation(t *testing.T) func(journal.ManagedPathMutation, error) journal.ManagedPathMutation {
	t.Helper()
	return func(mutation journal.ManagedPathMutation, err error) journal.ManagedPathMutation {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return mutation
	}
}
