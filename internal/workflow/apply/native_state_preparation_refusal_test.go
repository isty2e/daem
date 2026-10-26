package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
)

func nativeStateAdoptionFixture(t *testing.T, current string) (nativeProjectSourceFixture, execute.NativeApplyStateInput, string) {
	t.Helper()
	root := t.TempDir()
	old := nativeProjectStateSource(t, root, "settings", `{"a":1,"b":2}`, hostconfig.OwnershipKeys)
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: nativeStateFromObservedKeys(t, old.observed)})
	if err != nil {
		t.Fatal(err)
	}
	whole := nativeProjectStateSource(t, root, "settings", `{"a":1,"b":2}`, hostconfig.OwnershipDocument)
	placement, _ := whole.observed.SourceContract().HostConfigPlacement()
	hostPath := filepath.Join(root, filepath.FromSlash(placement.Destination().RelativePath()))
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostPath, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	input := execute.NativeApplyStateInput{
		Before: before, ManagedPaths: nativeProjectWholeDecisions(t, whole, before, true),
		WholeSources: []lock.LockedSubjectContract{whole.observed.SourceContract()}, StateCodec: nativePreparationStateCodec(t),
	}
	return whole, input, hostPath
}

func nativeStateDecisionInput(decision reconcile.ManagedPathDecision) reconcile.ManagedPathDecisionInput {
	input := reconcile.ManagedPathDecisionInput{
		Kind: decision.Kind(), Subject: decision.Subject(), ConsumerTargets: decision.ConsumerTargets(), Scope: decision.Scope(),
		Destination: decision.Destination(), DesiredHash: decision.DesiredHash(), LiveHash: decision.LiveHash(), ContentKind: decision.ContentKind(),
		PlacementMode: decision.PlacementMode(), PermissionPolicy: decision.PermissionPolicy(), DesiredFileMode: decision.DesiredFileMode(),
		LiveFileMode: decision.LiveFileMode(), Reason: decision.Reason(), Detail: decision.Detail(),
	}
	if previous, present := decision.PreviousState(); present {
		input.Previous = &previous
	}
	return input
}

func TestNativeApplyStateAdoptionRequiresActualMatchingRecordAndSource(t *testing.T) {
	for _, name := range []string{"no manage-existing", "different bytes", "missing file", "missing source", "wrong source", "duplicate source", "lost reason", "duplicate record"} {
		t.Run(name, func(t *testing.T) {
			whole, input, hostPath := nativeStateAdoptionFixture(t, `{"a":1,"b":2}`)
			expectedCurrent := `{"a":1,"b":2}`
			switch name {
			case "no manage-existing":
				input.ManagedPaths = nativeProjectWholeDecisions(t, whole, input.Before, false)
			case "different bytes":
				expectedCurrent = `{"a":99}`
				if err := os.WriteFile(hostPath, []byte(`{"a":99}`), 0o600); err != nil {
					t.Fatal(err)
				}
				input.ManagedPaths = nativeProjectWholeDecisions(t, whole, input.Before, true)
			case "missing file":
				if err := os.Remove(hostPath); err != nil {
					t.Fatal(err)
				}
				input.ManagedPaths = nativeProjectWholeDecisions(t, whole, input.Before, true)
				if len(input.ManagedPaths) != 1 || input.ManagedPaths[0].Kind() != reconcile.ManagedPathCreate {
					t.Fatal("missing formerly keyed file did not exercise the create refusal")
				}
			case "missing source":
				input.WholeSources = nil
			case "wrong source":
				other := nativeProjectStateSource(t, t.TempDir(), "settings", `{"a":9}`, hostconfig.OwnershipDocument)
				input.WholeSources = []lock.LockedSubjectContract{other.observed.SourceContract()}
			case "duplicate source":
				input.WholeSources = append(input.WholeSources, input.WholeSources[0])
			case "lost reason":
				facts := nativeStateDecisionInput(input.ManagedPaths[0])
				facts.Reason = reconcile.ReasonStateStale
				decision, err := reconcile.NewManagedPathDecision(facts)
				if err != nil {
					t.Fatal(err)
				}
				input.ManagedPaths = []reconcile.ManagedPathDecision{decision}
			case "duplicate record":
				input.ManagedPaths = append(input.ManagedPaths, input.ManagedPaths[0])
			}
			if prepared, err := execute.PrepareNativeApplyState(t.Context(), input); err == nil || prepared.Validate() == nil {
				t.Fatal("invalid adoption exposed a usable state preparation")
			}
			if name == "missing file" {
				if _, err := os.Stat(hostPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refused preparation recreated the missing host file", err)
				}
			} else if actual, err := os.ReadFile(hostPath); err != nil || string(actual) != expectedCurrent {
				t.Fatal("refused preparation modified the current document", err)
			}
		})
	}
}

func TestNativeApplyStateAdoptionPreservesOtherPlacementAndRefusesForeignKeys(t *testing.T) {
	whole, input, _ := nativeStateAdoptionFixture(t, `{"a":1,"b":2}`)
	global := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, `{"a":1}`, `{"a":1}`, hostconfig.OwnershipKeys)
	otherStates := nativeStateFromObservedKeys(t, global.Source)
	keys := append(input.Before.ManagedAggregates(), otherStates...)
	before, err := input.Before.WithManagedAggregates(keys)
	if err != nil {
		t.Fatal(err)
	}
	input.Before = before
	prepared, err := execute.PrepareNativeApplyState(t.Context(), input)
	if err != nil || prepared.Validate() != nil {
		t.Fatal("same declaration in another scope was treated as this adoption", err)
	}
	if rows := prepared.AfterState().ManagedAggregates(); len(rows) != 1 || !rows[0].Equal(otherStates[0]) {
		t.Fatal("adoption retired a different placement's prior key state")
	}
	foreign := nativeProjectStateSource(t, t.TempDir(), "foreign", `{"z":3}`, hostconfig.OwnershipKeys)
	foreignRows := append(before.ManagedAggregates(), nativeStateFromObservedKeys(t, foreign.observed)...)
	input.Before, err = before.WithManagedAggregates(foreignRows)
	if err != nil {
		t.Fatal(err)
	}
	input.ManagedPaths = nativeProjectWholeDecisions(t, whole, input.Before, true)
	if prepared, err := execute.PrepareNativeApplyState(t.Context(), input); err == nil || prepared.Validate() == nil {
		t.Fatal("whole adoption covered a different native declaration at the same document")
	}
}

func TestNativeApplyStateRetainsRecordWorkEvenWithEqualSnapshots(t *testing.T) {
	_, input, _ := nativeStateAdoptionFixture(t, `{"a":1,"b":2}`)
	initial, err := execute.PrepareNativeApplyState(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Before = initial.AfterState()
	facts := nativeStateDecisionInput(input.ManagedPaths[0])
	previous := input.Before.ManagedPaths()[0]
	facts.Previous, facts.Reason = &previous, reconcile.ReasonStateStale
	decision, err := reconcile.NewManagedPathDecision(facts)
	if err != nil {
		t.Fatal(err)
	}
	input.ManagedPaths = []reconcile.ManagedPathDecision{decision}
	prepared, err := execute.PrepareNativeApplyState(t.Context(), input)
	if err != nil || prepared.Validate() != nil || !prepared.BeforeState().Equal(prepared.AfterState()) || len(prepared.ManagedPathEffects()) != 1 {
		t.Fatal("equal snapshots erased actual record work", err)
	}
	input.Before = durable.EmptySnapshot()
	if prepared, err := execute.PrepareNativeApplyState(t.Context(), input); err == nil || prepared.Validate() == nil {
		t.Fatal("whole effect was accepted without its exact previous state")
	}
}

type nativeStateFinalCodec struct {
	inner durable.SnapshotCodec
	calls int
	final func() error
}

func (codec *nativeStateFinalCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	content, err := codec.inner.Encode(snapshot)
	if err != nil {
		return nil, err
	}
	codec.calls++
	if codec.calls == 2 {
		if err := codec.final(); err != nil {
			return nil, err
		}
	}
	return content, nil
}

func (codec *nativeStateFinalCodec) Decode(content []byte) (durable.Snapshot, error) {
	return codec.inner.Decode(content)
}

func TestNativeApplyStateDiscardsLateCodecAndCancellationResults(t *testing.T) {
	for _, cancelFinal := range []bool{false, true} {
		_, input, _ := nativeStateAdoptionFixture(t, `{"a":1,"b":2}`)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		failure := errors.New("selected state codec refused the derived state")
		want := failure
		if cancelFinal {
			want = context.Canceled
		}
		codec := &nativeStateFinalCodec{inner: input.StateCodec, final: func() error {
			if cancelFinal {
				cancel()
				return nil
			}
			return failure
		}}
		input.StateCodec = codec
		prepared, err := execute.PrepareNativeApplyState(ctx, input)
		if codec.calls != 2 || !errors.Is(err, want) || prepared.Validate() == nil {
			t.Fatal("late refusal exposed a partially prepared result", err)
		}
	}
}
