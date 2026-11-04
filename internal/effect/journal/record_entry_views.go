package journal

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type recordedPathEntry struct {
	path             string
	scope            string
	contentPath      string
	projection       *recordedAggregateEntry
	binding          recordedGlobalBindingKey
	hasGlobalBinding bool
	backup           recordedOrdinaryBackup
	initialized      bool
}

// Views share this owned, immutable snapshot. Transitions must replace it,
// never write through its pointer or compared-field slice.
type recordedAggregateEntry struct {
	subject      persistedSubjectRef
	target       string
	aggregate    recoveryAggregateContract
	hasAggregate bool
}

type recordedGlobalBindingKey struct {
	binding recoveryGlobalPathBinding
}

type recordedOrdinaryBackup struct {
	path        string
	kind        string
	digest      string
	initialized bool
	existed     bool
}

func recordedPathEntries(entries []recoveryEntry) []recordedPathEntry {
	result := make([]recordedPathEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, recordedPathEntryView(entry))
	}
	return result
}

func recordedPathEntryView(entry recoveryEntry) recordedPathEntry {
	view := recordedPathEntry{
		path: entry.Path, scope: entry.Scope, binding: recordedGlobalBindingIdentity(entry.GlobalPathBinding),
		contentPath:      entry.ContentPath,
		hasGlobalBinding: entry.GlobalPathBinding != nil,
		backup:           recordedOrdinaryBackup{path: entry.Before.BackupPath, kind: entry.Before.Kind, digest: entry.Before.ContentHash, existed: entry.Before.Existed, initialized: true},
		initialized:      true,
	}
	if entry.Aggregate != nil || entry.ContentPath != "" {
		projection := recordedAggregateEntry{subject: entry.Subject, target: entry.Target}
		if entry.Aggregate != nil {
			projection.aggregate, projection.hasAggregate = *entry.Aggregate, true
			projection.aggregate.ComparedFields = slices.Clone(entry.Aggregate.ComparedFields)
		}
		view.projection = &projection
	}
	return view
}

func (entry recordedPathEntry) Path() string                   { return entry.path }
func (entry recordedPathEntry) ContentPath() string            { return entry.contentPath }
func (entry recordedPathEntry) Scope() target.Scope            { return target.Scope(entry.scope) }
func (entry recordedPathEntry) Backup() recordedOrdinaryBackup { return entry.backup }
func (backup recordedOrdinaryBackup) Existed() bool            { return backup.existed }

func (entry recordedPathEntry) Validate() error {
	if !entry.initialized {
		return fmt.Errorf("recorded path entry is required")
	}
	return nil
}

func (entry recordedPathEntry) AggregateContract() (*aggregate.ProjectionContract, error) {
	if err := entry.Validate(); err != nil {
		return nil, err
	}
	value := recoveryEntry{Path: entry.path, Scope: entry.scope, ContentPath: entry.contentPath}
	if entry.projection != nil {
		value.Subject, value.Target = entry.projection.subject, entry.projection.target
		if entry.projection.hasAggregate {
			value.Aggregate = &entry.projection.aggregate
		}
	}
	return canonicalRecoveryAggregateContract(value)
}

func (entry recordedPathEntry) GlobalBinding() (string, rootedpath.AuthorityProvenance, error) {
	if !entry.HasGlobalBinding() {
		return "", rootedpath.AuthorityProvenance{}, fmt.Errorf("recorded global path binding is required")
	}
	return entry.binding.binding.canonical()
}

func recordedGlobalBindingIdentity(binding *recoveryGlobalPathBinding) recordedGlobalBindingKey {
	if binding == nil {
		return recordedGlobalBindingKey{}
	}
	return recordedGlobalBindingKey{binding: *binding}
}

func (journal nativeJournalSnapshots) recordedEntries() ([]recordedPathEntry, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	return recordedPathEntries(journal.record.Entries), nil
}

func (journal nativeJournalSnapshots) ordinaryEntries(ctx context.Context) ([]recovery.Entry, error) {
	if err := journal.requireNativeValue(); err != nil {
		return nil, err
	}
	entries := make([]recovery.Entry, 0, len(journal.record.Entries))
	for _, record := range journal.record.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry, err := canonicalRecoveryEntry(record)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (material ordinaryCaptureMaterial) recordedEntries() ([]recordedPathEntry, error) {
	if !material.initialized {
		return nil, fmt.Errorf("completed ordinary capture evidence is required")
	}
	return recordedPathEntries(material.entries), nil
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
