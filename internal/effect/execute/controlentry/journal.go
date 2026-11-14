package controlentry

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// JournalEntries owns either a complete journal pair or no entries; its zero value is empty.
type JournalEntries struct {
	directory *rootedpath.EntryAuthority
	record    *rootedpath.EntryAuthority
}

func (entries *JournalEntries) Bind(root *rootedpath.CapturedRoot, selectedRoot, directory string, phase *physicalwork.TraversalPhase) error {
	return entries.bindEntries(directory, func(path string) (*rootedpath.EntryAuthority, error) {
		return bindSelectedEntry(root, selectedRoot, path, phase)
	})
}

func (entries *JournalEntries) bindEntries(directory string, bindEntry func(string) (*rootedpath.EntryAuthority, error)) error {
	if entries.directory != nil || entries.record != nil {
		return fmt.Errorf("recovery journal authority is already bound")
	}
	boundDirectory, err := bindEntry(directory)
	if err != nil {
		return fmt.Errorf("bind recovery journal: %w", err)
	}
	boundRecord, err := bindEntry(filepath.Join(directory, "journal.json"))
	if err != nil {
		return errors.Join(fmt.Errorf("bind recovery journal record: %w", err), boundDirectory.Close())
	}
	entries.directory, entries.record = boundDirectory, boundRecord
	return nil
}

// Directory and Record return borrowed views; callers must not close them independently.
func (entries *JournalEntries) Directory() *rootedpath.EntryAuthority {
	if entries == nil {
		return nil
	}
	return entries.directory
}

func (entries *JournalEntries) Record() *rootedpath.EntryAuthority {
	if entries == nil {
		return nil
	}
	return entries.record
}

// CloseInto preserves the caller's error fold, including prior identity when no close fails.
func (entries *JournalEntries) CloseInto(prior error) error {
	if entries == nil {
		return prior
	}
	if err := entries.directory.Close(); err != nil {
		prior = errors.Join(prior, fmt.Errorf("close recovery journal authority: %w", err))
	}
	entries.directory = nil
	if err := entries.record.Close(); err != nil {
		prior = errors.Join(prior, fmt.Errorf("close recovery journal record authority: %w", err))
	}
	entries.record = nil
	return prior
}
