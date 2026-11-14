package nativeoperation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit/stateimage"
)

func TestNativeApplySeparatesPhysicalStateAndJournalEncoding(t *testing.T) {
	input, _, _ := nativeOrderApplyInput(t, target.ScopeProject)
	selectImageState(t, &input)
	plan, err := PrepareNativeApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	options := nativeOrderApplyOptions(t)
	validate := options.ValidatePhysicalAuthorityRequests
	observed := false
	options.ValidatePhysicalAuthorityRequests = func(ctx context.Context, requests []mutation.PhysicalAuthorityRequest) error {
		if err := validate(ctx, requests); err != nil {
			return err
		}
		if !observed {
			observed = stateimage.CheckPublishedJournal(t, input.Paths.RecoveryDir, input.StateCodec, input.CurrentState, input.Preparation.AfterState())
		}
		return nil
	}
	result, err := ApplyNativeWithEffectPlan(t.Context(), input, plan, options)
	if err != nil || !observed {
		t.Fatal("native apply mixed its State image and journal roles", err)
	}
	content, err := os.ReadFile(input.Paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	image, err := statefile.UnmarshalImage(content)
	if err != nil || !image.Snapshot().Equal(result.State) {
		t.Fatal("native publication lost its selected image codec", err)
	}
	entries, err := os.ReadDir(input.Paths.RecoveryDir)
	if err != nil || len(entries) != 0 {
		t.Fatal("native image publication did not retire its canonical journal", err)
	}
}

func TestNativeApplyRequiresPhysicalCodecOnlyForChangedState(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "no-op"
		if changed {
			name = "changed"
		}
		t.Run(name, func(t *testing.T) {
			input, host, original := nativeOrderApplyInput(t, target.ScopeProject)
			if !changed {
				prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{Before: input.CurrentState, StateCodec: input.StateCodec})
				if err != nil {
					t.Fatal(err)
				}
				input.Preparation = prepared
			}
			before, err := os.ReadFile(input.Paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			input.StatefileCodec = statefile.PhysicalCodec{}
			plan, err := PrepareNativeApplyEffectPlan(input)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ApplyNativeWithEffectPlan(t.Context(), input, plan, nativeOrderApplyOptions(t))
			if (err != nil) != changed || result.ExecutionAttempted {
				t.Fatalf("changed=%t: unexpected native execution or codec admission: %v", changed, err)
			}
			actual, err := os.ReadFile(host)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatal("codec admission changed the native host document", err)
			}
			actual, err = os.ReadFile(input.Paths.StatefilePath)
			if err != nil || !bytes.Equal(actual, before) {
				t.Fatal("codec admission changed the physical State", err)
			}
			requireNoStateCodecJournal(t, input.Paths.RecoveryDir)
		})
	}
}

func TestCancelledNativeApplyRebuildsPhysicalCodecForSettlement(t *testing.T) {
	input, host, original := nativeOrderApplyInput(t, target.ScopeProject)
	selectImageState(t, &input)
	before, err := os.ReadFile(input.Paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input.StatefileCodec = stateimage.Physical(t, cancelBoundStateCodec{ctx, input.StatefileCodec})
	freshCodecs := input.RecoveryCodecs
	rebuilt := 0
	input.RecoveryCodecs = func(ctx context.Context) (aggregate.CodecCatalog, durable.SnapshotCodec, statefile.PhysicalCodec, error) {
		if err := ctx.Err(); err != nil {
			t.Fatal("settlement inherited the canceled forward context", err)
		}
		rebuilt++
		return freshCodecs(ctx)
	}
	plan, err := PrepareNativeApplyEffectPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	options := nativeOrderApplyOptions(t)
	accept := options.AcceptVisibilityChanges
	reached := false
	options.AcceptVisibilityChanges = func(ctx context.Context) error {
		if err := accept(ctx); err != nil {
			return err
		}
		actual, err := os.ReadFile(host)
		if err != nil {
			return err
		}
		if !reached && !bytes.Equal(actual, original) {
			reached = true
			cancel()
			return context.Canceled
		}
		return nil
	}
	_, err = ApplyNativeWithEffectPlan(ctx, input, plan, options)
	if !reached || !errors.Is(err, context.Canceled) || rebuilt != 1 {
		t.Fatalf("native cancellation did not enter independent settlement: rebuilt=%d, err=%v", rebuilt, err)
	}
	actual, err := os.ReadFile(host)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("canceled native publication was not restored", err)
	}
	actual, err = os.ReadFile(input.Paths.StatefilePath)
	if err != nil || !bytes.Equal(actual, before) {
		t.Fatal("settlement rewrote the uncommitted State image", err)
	}
	requireNoStateCodecJournal(t, input.Paths.RecoveryDir)
}

type cancelBoundStateCodec struct {
	ctx   context.Context
	codec statefile.PhysicalCodec
}

func (codec cancelBoundStateCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	if err := codec.ctx.Err(); err != nil {
		return nil, err
	}
	return codec.codec.EncodeState(snapshot)
}

func (codec cancelBoundStateCodec) Decode(content []byte) (durable.Snapshot, error) {
	if err := codec.ctx.Err(); err != nil {
		return durable.Snapshot{}, err
	}
	return codec.codec.DecodeState(content)
}

func selectImageState(t *testing.T, input *NativeApplyInput) {
	t.Helper()
	physical := stateimage.NewCodec(t, input.Paths.StatefilePath)
	content, err := physical.EncodeState(input.CurrentState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Paths.StatefilePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	input.StatefileCodec = physical
	catalog, journalCodec := input.Codecs, input.StateCodec
	input.RecoveryCodecs = func(context.Context) (aggregate.CodecCatalog, durable.SnapshotCodec, statefile.PhysicalCodec, error) {
		return catalog, journalCodec, physical, nil
	}
}

func requireNoStateCodecJournal(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) || len(entries) != 0 {
		t.Fatal("State codec operation left recovery work", err)
	}
}
