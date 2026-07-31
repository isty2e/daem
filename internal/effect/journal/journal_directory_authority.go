package journal

import (
	"context"
	"errors"
	"fmt"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// ActiveJournalAuthority is operation-local physical evidence for the active
// journal directory selected by one recovery plan. It is never durable
// authority and cannot select a path.
type ActiveJournalAuthority struct {
	identity mutationfs.EntryIdentity
}

// LegacyJournalAuthority is operation-local physical evidence for the
// released v0.1 tombstone selected by one cleanup plan. It is never durable
// authority and cannot select a path.
type LegacyJournalAuthority struct {
	identity mutationfs.EntryIdentity
}

func newActiveJournalAuthority(
	identity mutationfs.EntryIdentity,
) (ActiveJournalAuthority, error) {
	if identity == nil || identity.Kind() != mutationfs.EntryKindDirectory {
		return ActiveJournalAuthority{}, fmt.Errorf(
			"active journal authority requires a directory identity",
		)
	}
	return ActiveJournalAuthority{identity: identity}, nil
}

func newLegacyJournalAuthority(
	identity mutationfs.EntryIdentity,
) (LegacyJournalAuthority, error) {
	if identity == nil || identity.Kind() != mutationfs.EntryKindDirectory {
		return LegacyJournalAuthority{}, fmt.Errorf(
			"legacy journal authority requires a directory identity",
		)
	}
	return LegacyJournalAuthority{identity: identity}, nil
}

// CaptureActiveJournalAuthority binds current physical evidence to an
// already-selected active journal entry.
func CaptureActiveJournalAuthority(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	authority *rootedpath.EntryAuthority,
) (ActiveJournalAuthority, error) {
	if ctx == nil {
		return ActiveJournalAuthority{}, fmt.Errorf(
			"active journal authority context is required",
		)
	}
	if filesystem == nil {
		return ActiveJournalAuthority{}, fmt.Errorf(
			"active journal authority filesystem is required",
		)
	}
	if authority == nil {
		return ActiveJournalAuthority{}, fmt.Errorf(
			"active journal entry authority is required",
		)
	}
	capability, err := authority.Acquire()
	if err != nil {
		return ActiveJournalAuthority{}, err
	}
	identity, captureErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if captureErr != nil {
		return ActiveJournalAuthority{}, errors.Join(captureErr, closeErr)
	}
	if closeErr != nil {
		return ActiveJournalAuthority{}, closeErr
	}
	return newActiveJournalAuthority(identity)
}

// ValidateActiveJournalAuthority compares current physical evidence for an
// already-selected entry with the authority carried from planning.
func ValidateActiveJournalAuthority(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	entry *rootedpath.EntryAuthority,
	expected ActiveJournalAuthority,
) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	current, err := CaptureActiveJournalAuthority(ctx, filesystem, entry)
	if err != nil {
		return err
	}
	if !expected.equal(current) {
		return fmt.Errorf("active recovery journal identity changed before effects")
	}
	return nil
}

func (authority ActiveJournalAuthority) valid() bool {
	return authority.identity != nil &&
		authority.identity.Kind() == mutationfs.EntryKindDirectory
}

// Validate rejects zero or non-directory physical evidence.
func (authority ActiveJournalAuthority) Validate() error {
	if !authority.valid() {
		return fmt.Errorf("active journal authority is uninitialized")
	}
	return nil
}

func (authority ActiveJournalAuthority) equal(
	other ActiveJournalAuthority,
) bool {
	return authority.valid() &&
		other.valid() &&
		authority.identity.Equal(other.identity)
}

func (authority ActiveJournalAuthority) matches(
	identity mutationfs.EntryIdentity,
) bool {
	return authority.valid() &&
		identity != nil &&
		authority.identity.Equal(identity)
}

// Validate rejects zero or non-directory legacy physical evidence.
func (authority LegacyJournalAuthority) Validate() error {
	if !authority.valid() {
		return fmt.Errorf("legacy journal authority is uninitialized")
	}
	return nil
}

func (authority LegacyJournalAuthority) valid() bool {
	return authority.identity != nil &&
		authority.identity.Kind() == mutationfs.EntryKindDirectory
}

func (authority LegacyJournalAuthority) equal(
	other LegacyJournalAuthority,
) bool {
	return authority.valid() &&
		other.valid() &&
		authority.identity.Equal(other.identity)
}

func (authority LegacyJournalAuthority) matches(
	identity mutationfs.EntryIdentity,
) bool {
	return authority.valid() &&
		identity != nil &&
		authority.identity.Equal(identity)
}

func sameOptionalLegacyJournalAuthority(
	left LegacyJournalAuthority,
	right LegacyJournalAuthority,
) bool {
	if left.valid() != right.valid() {
		return false
	}
	return !left.valid() || left.equal(right)
}
