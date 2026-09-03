package apply

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestStatefileEffectAuthorityConsumesOneReservedLifecycle(t *testing.T) {
	t.Parallel()

	entry := &rootedpath.EntryAuthority{}
	bound := &recordingBoundStatefileAuthority{entry: entry}
	reservation := &recordingStatefileReservation{bound: bound}
	authority, err := newStatefileEffectAuthorityFromReservation(
		statefileEffectPlan{validations: 2, fileCommits: 1},
		reservation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if reservation.bindCalls != 1 || bound.validationCalls != 0 {
		t.Fatalf(
			"bind/validation calls after first ensure = %d/%d, want 1/0",
			reservation.bindCalls,
			bound.validationCalls,
		)
	}
	if err := authority.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := authority.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if bound.validationCalls != 2 {
		t.Fatalf("validation calls = %d, want 2", bound.validationCalls)
	}
	if err := authority.Validate(t.Context()); err == nil {
		t.Fatal("validation beyond the reserved plan succeeded")
	}
	committedEntry, err := authority.EntryForCommit()
	if err != nil {
		t.Fatal(err)
	}
	if committedEntry != entry {
		t.Fatalf("commit entry = %p, want %p", committedEntry, entry)
	}
	if _, err := authority.EntryForCommit(); err == nil {
		t.Fatal("second commit entry exceeded the reserved plan without error")
	}
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
	if bound.closeCalls != 1 {
		t.Fatalf("bound close calls = %d, want 1", bound.closeCalls)
	}
	if err := authority.Ensure(t.Context()); err == nil {
		t.Fatal("closed authority accepted another ensure")
	}
}

type recordingStatefileReservation struct {
	bound     boundStatefileEffectAuthority
	bindCalls int
}

func (reservation *recordingStatefileReservation) Bind(
	context.Context,
) (boundStatefileEffectAuthority, error) {
	reservation.bindCalls++
	return reservation.bound, nil
}

type recordingBoundStatefileAuthority struct {
	entry           *rootedpath.EntryAuthority
	validationCalls int
	closeCalls      int
}

func (authority *recordingBoundStatefileAuthority) Validate(context.Context) error {
	authority.validationCalls++
	return nil
}

func (authority *recordingBoundStatefileAuthority) Entry() *rootedpath.EntryAuthority {
	return authority.entry
}

func (authority *recordingBoundStatefileAuthority) Close() error {
	authority.closeCalls++
	return nil
}
