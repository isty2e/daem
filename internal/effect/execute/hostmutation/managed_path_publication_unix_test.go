//go:build darwin || linux

package hostmutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/payload"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/reconcile"
)

func TestManagedPathPublicationDistinguishesPreEffectAndMutationFailures(t *testing.T) {
	unknown := errors.New("selected mutation failure")
	uncommitted := classifiedMutationFailure{kind: mutationfs.FailureUncommitted}
	for _, test := range []struct {
		name       string
		payload    bool
		commitErr  error
		verifyErr  error
		want       EffectProgress
		wantWrites int
	}{
		{name: "missing payload", want: EffectNotStarted},
		{name: "unclassified write failure", payload: true, commitErr: unknown, want: EffectIndeterminate, wantWrites: 1},
		{name: "uncommitted write", payload: true, commitErr: uncommitted, want: EffectNotStarted, wantWrites: 1},
		{name: "post-write observation", payload: true, verifyErr: unknown, want: EffectIndeterminate, wantWrites: 1},
		{name: "verified publication", payload: true, want: EffectExpectedAfter, wantWrites: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			content := []byte("publication")
			effect := managedPublicationEffect(t, reconcile.ManagedPathCreate, "managed", nil, content)
			schedule, err := PlanManagedPaths([]effectpreparation.ManagedPathEffect{effect})
			if err != nil {
				t.Fatal(err)
			}
			store := &managedPublicationStore{Store: storagecommit.Adapter{}, commitErr: test.commitErr, verifyErr: test.verifyErr}
			operation := projectMutationOperation(t, root, store)
			var payloads payload.PayloadSet
			if test.payload {
				value, err := payload.NewFilePayload(effect.Subject(), content, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				payloads, err = payload.NewPayloadSet([]payload.Payload{value}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			steps := slices.Collect(schedule.Operations(ManagedPathPublishPhase))
			progress, err := operation.ApplyManagedPathStep(t.Context(), steps[0], payloads)
			if progress != test.want || store.writes != test.wantWrites || (err == nil) != (test.want == EffectExpectedAfter) {
				t.Fatalf("progress=%v writes=%d error=%v", progress, store.writes, err)
			}
			if test.commitErr != nil && !errors.Is(err, test.commitErr) || test.verifyErr != nil && !errors.Is(err, test.verifyErr) {
				t.Fatal("publication lost the selected failure cause")
			}
			if test.verifyErr != nil && !mutationfs.HasIndeterminateEffect(err) {
				t.Fatal("post-effect observation failure lost its uncertainty marker")
			}
			actual, readErr := os.ReadFile(filepath.Join(root, "managed"))
			if test.payload && test.commitErr == nil {
				if readErr != nil || string(actual) != string(content) {
					t.Fatalf("written content=%q error=%v", actual, readErr)
				}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("pre-effect failure changed destination absence: %v", readErr)
			}
		})
	}
}

func TestManagedPathRecordObservesWithoutWriting(t *testing.T) {
	root := t.TempDir()
	content := []byte("retained")
	if err := os.WriteFile(filepath.Join(root, "managed"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	previous := managedPublicationState(t, "managed", content)
	effect := managedPublicationEffect(t, reconcile.ManagedPathRecord, "managed", &previous, content)
	schedule, err := PlanManagedPaths([]effectpreparation.ManagedPathEffect{effect})
	if err != nil {
		t.Fatal(err)
	}
	store := &managedPublicationStore{Store: storagecommit.Adapter{}}
	operation := projectMutationOperation(t, root, store)
	for step := range schedule.Operations(ManagedPathPublishPhase) {
		progress, err := operation.ApplyManagedPathStep(t.Context(), step, payload.PayloadSet{})
		if err != nil || progress != EffectExpectedAfter || store.writes != 0 {
			t.Fatalf("record progress=%v writes=%d error=%v", progress, store.writes, err)
		}
	}
}

func TestManagedPathRelocationPublishesBeforeRetiringPrevious(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "old"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := managedPublicationState(t, "old", []byte("before"))
	content := []byte("after")
	effect := managedPublicationEffect(t, reconcile.ManagedPathReplace, "new", &previous, content)
	schedule, err := PlanManagedPaths([]effectpreparation.ManagedPathEffect{effect})
	if err != nil {
		t.Fatal(err)
	}
	value, err := payload.NewFilePayload(effect.Subject(), content, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := payload.NewPayloadSet([]payload.Payload{value}, nil)
	if err != nil {
		t.Fatal(err)
	}
	operation := projectMutationOperation(t, root, storagecommit.Adapter{})
	for step := range schedule.Operations(ManagedPathPublishPhase) {
		progress, err := operation.ApplyManagedPathStep(t.Context(), step, payloads)
		if err != nil || progress != EffectExpectedAfter {
			t.Fatalf("relocation publication progress=%v error=%v", progress, err)
		}
	}
	for name, expected := range map[string]string{"old": "before", "new": "after"} {
		actual, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(actual) != expected {
			t.Fatalf("%s content=%q error=%v", name, actual, err)
		}
	}
}

type managedPublicationStore struct {
	mutationfs.Store
	writes    int
	commitErr error
	verifyErr error
}

func (store *managedPublicationStore) CreateRootedFile(ctx context.Context, capability rootedpath.CommitCapability, content []byte, mode os.FileMode) error {
	store.writes++
	if store.commitErr != nil {
		_ = capability.Close()
		return store.commitErr
	}
	return store.Store.CreateRootedFile(ctx, capability, content, mode)
}

func (store *managedPublicationStore) ReadRootedRegularFile(ctx context.Context, capability rootedpath.CommitCapability) ([]byte, os.FileMode, mutationfs.EntryIdentity, error) {
	if store.writes != 0 && store.verifyErr != nil {
		return nil, 0, nil, store.verifyErr
	}
	return store.Store.ReadRootedRegularFile(ctx, capability)
}
