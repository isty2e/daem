package pipackage

import (
	"fmt"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ScopedRelation pairs one exact daem relation expectation with the command
// root from which Pi interprets an operator-provided local source.
type ScopedRelation struct {
	key           observerelation.CorrelationKey
	scope         target.Scope
	commandRoot   string
	sourceContext *extensiontopology.LocalSourceContext
}

// NewScopedRelation validates one Pi settings correlation request.
func NewScopedRelation(
	key observerelation.CorrelationKey,
	scope target.Scope,
	commandRoot string,
) (ScopedRelation, error) {
	if err := key.Validate(); err != nil {
		return ScopedRelation{}, fmt.Errorf("Pi relation correlation key: %w", err)
	}
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return ScopedRelation{}, fmt.Errorf("Pi relation scope: %w", err)
	}
	switch parsedScope {
	case target.ScopeProject, target.ScopeGlobal:
	default:
		return ScopedRelation{}, fmt.Errorf("Pi relation scope %q is not observable", parsedScope)
	}
	root, err := cleanAbsoluteRoot("Pi package command root", commandRoot)
	if err != nil {
		return ScopedRelation{}, err
	}
	return ScopedRelation{key: key, scope: parsedScope, commandRoot: root}, nil
}

// NewScopedRelationWithContext binds lexical source interpretation without
// selecting roots from the process environment.
func NewScopedRelationWithContext(key observerelation.CorrelationKey, scope target.Scope, context extensiontopology.LocalSourceContext) (ScopedRelation, error) {
	if err := context.Validate(); err != nil {
		return ScopedRelation{}, err
	}
	relation, err := NewScopedRelation(key, scope, context.BaseRoot())
	if err != nil {
		return ScopedRelation{}, err
	}
	relation.sourceContext = &context
	return relation, nil
}

func (relation ScopedRelation) CorrelationKey() observerelation.CorrelationKey { return relation.key }

func (relation ScopedRelation) Scope() target.Scope { return relation.scope }

func (relation ScopedRelation) SourceContext() (extensiontopology.LocalSourceContext, bool) {
	if relation.sourceContext == nil {
		return extensiontopology.LocalSourceContext{}, false
	}
	return *relation.sourceContext, true
}

// Correlate classifies one scope-specific settings inventory against an exact
// locked source. Pi-equivalent but textually different rows are deliberately
// surfaced as unkeyed same-subject rows; they can never receive the exact
// expected relation key.
func Correlate(
	expected ScopedRelation,
	inventory Inventory,
) (observerelation.CorrelationResult, error) {
	if inventory.scope != expected.scope {
		return observerelation.CorrelationResult{}, fmt.Errorf(
			"Pi settings inventory scope %q does not match expected scope %q",
			inventory.scope,
			expected.scope,
		)
	}
	expectedRelation := expected.key.ExpectedRelation()
	lockedSource := string(expectedRelation.SubjectKey())
	storedSource, err := expectedSettingsSource(expected, inventory.settingsBase)
	if err != nil {
		return observerelation.CorrelationResult{}, fmt.Errorf("derive expected Pi settings source: %w", err)
	}
	expectedIdentity, err := sourceIdentityForRelation(expected)
	if err != nil {
		return observerelation.CorrelationResult{}, fmt.Errorf("parse expected Pi source: %w", err)
	}

	documentEntries := inventory.document.Entries()
	rows := make([]observerelation.Row, 0, len(documentEntries))
	for index, entry := range documentEntries {
		source := entry.Source()
		observedIdentity, err := inventory.sourceIdentity(source)
		if err != nil {
			return observerelation.CorrelationResult{}, fmt.Errorf(
				"parse Pi settings package source[%d]: %w",
				index,
				err,
			)
		}
		exact := source == storedSource
		equivalent := observedIdentity == expectedIdentity
		subjectKey := source
		if exact || equivalent {
			subjectKey = lockedSource
		}
		row, err := observerelation.NewRow(observerelation.RowSpec{
			SubjectKey:            subjectKey,
			HasManagedInstanceKey: exact,
			ManagedInstanceKey: func() string {
				if exact {
					return string(expectedRelation.ManagedInstanceKey())
				}
				return ""
			}(),
		})
		if err != nil {
			return observerelation.CorrelationResult{}, fmt.Errorf(
				"normalize Pi settings package source[%d]: %w",
				index,
				err,
			)
		}
		rows = append(rows, row)
	}
	relationInventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         rows,
	})
	if err != nil {
		return observerelation.CorrelationResult{}, err
	}
	return observerelation.Correlate(expectedRelation, relationInventory), nil
}
