package journal

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type recordedPathEntry struct {
	path             string
	scope            string
	binding          recordedGlobalBindingKey
	hasGlobalBinding bool
	backup           recordedOrdinaryBackup
	initialized      bool
}

type recordedGlobalBindingKey struct {
	binding recoveryGlobalPathBinding
}

type recordedOrdinaryBackup struct {
	path        string
	kind        string
	digest      string
	initialized bool
}

func recordedPathEntries(entries []recoveryEntry) []recordedPathEntry {
	result := make([]recordedPathEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, recordedPathEntryView(entry))
	}
	return result
}

func recordedPathEntryView(entry recoveryEntry) recordedPathEntry {
	return recordedPathEntry{
		path: entry.Path, scope: entry.Scope, binding: recordedGlobalBindingIdentity(entry.GlobalPathBinding),
		hasGlobalBinding: entry.GlobalPathBinding != nil,
		backup:           recordedOrdinaryBackup{path: entry.Before.BackupPath, kind: entry.Before.Kind, digest: entry.Before.ContentHash, initialized: true},
		initialized:      true,
	}
}

func recordedGlobalBindingIdentity(binding *recoveryGlobalPathBinding) recordedGlobalBindingKey {
	if binding == nil {
		return recordedGlobalBindingKey{}
	}
	return recordedGlobalBindingKey{binding: *binding}
}

func (journal nativeJournalSnapshots) recordedEntries() []recordedPathEntry {
	return recordedPathEntries(journal.record.Entries)
}

func (material ordinaryCaptureMaterial) recordedEntries() []recordedPathEntry {
	return recordedPathEntries(material.entries)
}

func (entry recordedPathEntry) WriterAddress() (target.Scope, output.Destination, error) {
	if !entry.initialized {
		return "", output.Destination{}, fmt.Errorf("recorded path entry is required")
	}
	destination, err := output.Parse(entry.path)
	if err != nil {
		return "", output.Destination{}, err
	}
	return target.Scope(entry.scope), destination, nil
}

func (entry recordedPathEntry) BindingIdentity() recordedGlobalBindingKey {
	if !entry.HasGlobalBinding() {
		return recordedGlobalBindingKey{}
	}
	return entry.binding
}

func (entry recordedPathEntry) HasGlobalBinding() bool {
	return entry.initialized && entry.hasGlobalBinding
}

func (entry recordedPathEntry) GlobalPath() string {
	if !entry.HasGlobalBinding() {
		return ""
	}
	return entry.binding.binding.ResolvedPath
}

func (entry recordedPathEntry) MatchGlobalRoot(root rootedpath.Authority) error {
	if !entry.HasGlobalBinding() {
		return fmt.Errorf("recorded global path binding is required")
	}
	expected, err := entry.binding.binding.RootProvenance.canonical()
	if err != nil {
		return err
	}
	return expected.Match(root)
}

func recordedBackupReferences(ctx context.Context, entries []recordedPathEntry) (map[string]recordedOrdinaryBackup, error) {
	references := make(map[string]recordedOrdinaryBackup)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.initialized {
			return nil, fmt.Errorf("recorded path entry is required")
		}
		before := entry.backup
		if before.Path() == "" {
			continue
		}
		name, present := strings.CutPrefix(before.Path(), "files/")
		if !present || name == "." || name == ".." || name == "" || path.Base(name) != name {
			return nil, fmt.Errorf("ordinary builder returned a noncanonical backup reference")
		}
		if _, duplicate := references[name]; duplicate {
			return nil, fmt.Errorf("ordinary builder returned duplicate backup references")
		}
		references[name] = before
	}
	return references, nil
}

func (backup recordedOrdinaryBackup) Path() string { return backup.path }

func (backup recordedOrdinaryBackup) Kind() string { return backup.kind }

func (backup recordedOrdinaryBackup) Matches(kind, digest string) bool {
	return backup.initialized && backup.kind == kind && backup.digest == digest
}
