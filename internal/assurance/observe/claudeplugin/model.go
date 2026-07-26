package claudeplugin

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

// HostScope is the scope vocabulary reported by Claude Code plugin inventory.
// It is a host observation boundary fact, not daem's canonical target scope.
type HostScope string

const (
	HostScopeUnknown HostScope = "unknown"
	HostScopeProject HostScope = "project"
	HostScopeUser    HostScope = "user"
	HostScopeLocal   HostScope = "local"
	HostScopeManaged HostScope = "managed"
)

// RowSpec contains one normalized passive Claude plugin inventory row.
type RowSpec struct {
	SubjectKey            string
	HasManagedInstanceKey bool
	ManagedInstanceKey    string
	Scope                 HostScope
}

// Row is one passive Claude plugin carrier relation observation.
type Row struct {
	relation    relation.Row
	scope       HostScope
	sourceExact bool
}

// NewRow validates and constructs a passive Claude plugin inventory row.
func NewRow(spec RowSpec) (Row, error) {
	relationRow, err := relation.NewRow(relation.RowSpec{
		SubjectKey:            spec.SubjectKey,
		HasManagedInstanceKey: spec.HasManagedInstanceKey,
		ManagedInstanceKey:    spec.ManagedInstanceKey,
	})
	if err != nil {
		return Row{}, err
	}
	scope, err := normalizeHostScope(spec.Scope)
	if err != nil {
		return Row{}, fmt.Errorf("scope: %w", err)
	}
	return Row{
		relation: relationRow,
		scope:    scope,
	}, nil
}

func newSourceExactRow(subjectKey string, scope HostScope) (Row, error) {
	row, err := NewRow(RowSpec{SubjectKey: subjectKey, Scope: scope})
	if err != nil {
		return Row{}, err
	}
	row.sourceExact = true
	return row, nil
}

// HostScope returns the Claude host inventory scope for this row.
func (row Row) HostScope() HostScope { return row.scope }

// InventorySpec contains passive inventory evidence for one observation batch.
type InventorySpec struct {
	Availability relation.InventoryAvailability
	Freshness    relation.EvidenceFreshness
	Rows         []Row
}

// Inventory is a passive Claude plugin inventory evidence batch.
type Inventory struct {
	relation relation.Inventory
	rows     []Row
}

// ScopedRelation is one exact daem relation expectation paired with the
// Claude inventory scope needed to select host rows.
type ScopedRelation struct {
	key   relation.CorrelationKey
	scope target.Scope
}

// NewScopedRelation validates one Claude inventory correlation request.
func NewScopedRelation(
	key relation.CorrelationKey,
	scope target.Scope,
) (ScopedRelation, error) {
	if err := key.Validate(); err != nil {
		return ScopedRelation{}, fmt.Errorf("Claude relation correlation key: %w", err)
	}
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return ScopedRelation{}, fmt.Errorf("Claude relation scope: %w", err)
	}
	switch parsedScope {
	case target.ScopeProject, target.ScopeGlobal:
	default:
		return ScopedRelation{}, fmt.Errorf("Claude relation scope %q is not observable", parsedScope)
	}
	return ScopedRelation{key: key, scope: parsedScope}, nil
}

// CorrelationKey returns the exact batch identity for this request.
func (expected ScopedRelation) CorrelationKey() relation.CorrelationKey {
	return expected.key
}

// Scope returns the canonical scope mapped to Claude's host vocabulary.
func (expected ScopedRelation) Scope() target.Scope { return expected.scope }

// ExpectedRelation returns the selected host-visible structural correlation pair.
func (expected ScopedRelation) ExpectedRelation() hostrelation.ExpectedRelation {
	return expected.key.ExpectedRelation()
}

// NewInventory validates and constructs a passive inventory batch.
func NewInventory(spec InventorySpec) (Inventory, error) {
	relationInventory, err := newRelationInventory(spec.Availability, spec.Freshness, spec.Rows)
	if err != nil {
		return Inventory{}, err
	}
	return Inventory{
		relation: relationInventory,
		rows:     cloneRows(spec.Rows),
	}, nil
}

// Availability returns passive inventory support status.
func (inventory Inventory) Availability() relation.InventoryAvailability {
	return inventory.relation.Availability()
}

// Freshness returns passive inventory freshness status.
func (inventory Inventory) Freshness() relation.EvidenceFreshness {
	return inventory.relation.Freshness()
}

// Correlate classifies passive inventory against one selected Claude plugin relation.
func Correlate(
	expected relationExpectation,
	inventory Inventory,
) relation.CorrelationResult {
	scopedRows := rowsForRelationScope(expected, inventory.rows)
	scopedInventory := mustRelationInventory(inventory.Availability(), inventory.Freshness(), scopedRows)
	return relation.Correlate(expected.ExpectedRelation(), scopedInventory)
}

func cloneRows(rows []Row) []Row {
	return append([]Row(nil), rows...)
}

type relationExpectation interface {
	Scope() target.Scope
	ExpectedRelation() hostrelation.ExpectedRelation
}

func rowsForRelationScope(expected relationExpectation, rows []Row) []Row {
	expectedScope, ok := hostScopeForRelation(expected)
	if !ok {
		return nil
	}
	expectedRelation := expected.ExpectedRelation()
	filtered := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.HostScope() != expectedScope {
			continue
		}
		projected := row
		if row.sourceExact &&
			row.relation.SubjectKey() == expectedRelation.SubjectKey() {
			relationRow, err := relation.NewRow(relation.RowSpec{
				SubjectKey:            string(expectedRelation.SubjectKey()),
				HasManagedInstanceKey: true,
				ManagedInstanceKey:    string(expectedRelation.ManagedInstanceKey()),
			})
			if err != nil {
				panic(fmt.Sprintf("invalid Claude source-exact relation row: %v", err))
			}
			projected.relation = relationRow
		}
		filtered = append(filtered, projected)
	}
	return filtered
}

func hostScopeForRelation(expected relationExpectation) (HostScope, bool) {
	switch expected.Scope() {
	case target.ScopeProject:
		return HostScopeProject, true
	case target.ScopeGlobal:
		return HostScopeUser, true
	default:
		return "", false
	}
}

func newRelationInventory(
	availability relation.InventoryAvailability,
	freshness relation.EvidenceFreshness,
	rows []Row,
) (relation.Inventory, error) {
	relationRows := make([]relation.Row, 0, len(rows))
	for _, row := range rows {
		relationRows = append(relationRows, row.relation)
	}
	return relation.NewInventory(relation.InventorySpec{
		Availability: availability,
		Freshness:    freshness,
		Rows:         relationRows,
	})
}

func mustRelationInventory(
	availability relation.InventoryAvailability,
	freshness relation.EvidenceFreshness,
	rows []Row,
) relation.Inventory {
	inventory, err := newRelationInventory(availability, freshness, rows)
	if err != nil {
		panic(fmt.Sprintf("invalid Claude plugin scoped relation inventory: %v", err))
	}
	return inventory
}

func normalizeHostScope(scope HostScope) (HostScope, error) {
	switch scope {
	case "":
		return HostScopeUnknown, nil
	case HostScopeUnknown, HostScopeProject, HostScopeUser, HostScopeLocal, HostScopeManaged:
		return scope, nil
	default:
		return "", fmt.Errorf("unsupported host scope %q", scope)
	}
}
