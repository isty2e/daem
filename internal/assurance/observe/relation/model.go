package relation

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/realization/relation"
)

// InventoryAvailability records whether passive relation inventory is supported.
type InventoryAvailability string

const (
	InventorySupported   InventoryAvailability = "supported"
	InventoryUnsupported InventoryAvailability = "unsupported"
	InventoryUnavailable InventoryAvailability = "unavailable"
)

// EvidenceFreshness records whether passive inventory may be used for correlation.
type EvidenceFreshness string

const (
	EvidenceFresh EvidenceFreshness = "fresh"
	EvidenceStale EvidenceFreshness = "stale"
)

// CorrelationState classifies passive relation correlation.
type CorrelationState string

const (
	StateExactCorrelation    CorrelationState = "exact_correlation"
	StateMissing             CorrelationState = "missing"
	StateUnkeyedSameSubject  CorrelationState = "unkeyed_same_subject"
	StateSameSubjectShadow   CorrelationState = "same_name_shadow"
	StateManagedKeyDrift     CorrelationState = "managed_key_drift"
	StateAmbiguous           CorrelationState = "ambiguous"
	StateStaleEvidence       CorrelationState = "stale_evidence"
	StateUnsupported         CorrelationState = "unsupported"
	StateUnavailableEvidence CorrelationState = "unavailable_evidence"
)

// ReasonCode is a stable machine-readable passive inventory reason.
type ReasonCode string

const (
	ReasonNone                 ReasonCode = ""
	ReasonUnsupportedInventory ReasonCode = "unsupported_passive_inventory"
	ReasonStaleEvidence        ReasonCode = "stale_evidence"
	ReasonMissing              ReasonCode = "managed_relation_missing"
	ReasonUnkeyedSameSubject   ReasonCode = "unkeyed_same_subject"
	ReasonSameSubjectShadow    ReasonCode = "same_name_shadow"
	ReasonManagedKeyDrift      ReasonCode = "managed_key_drift"
	ReasonAmbiguous            ReasonCode = "ambiguous_relation"
	ReasonUnavailableEvidence  ReasonCode = "relation_evidence_unavailable"
)

// Watchpoint is follow-up guidance surfaced without granting authority.
type Watchpoint string

const (
	WatchpointReplacementAuthorityRequired Watchpoint = "replacement_authority_required"
	WatchpointFreshInventoryRequired       Watchpoint = "fresh_inventory_required"
	WatchpointPassiveInventoryRequired     Watchpoint = "passive_inventory_required"
	WatchpointRelationEvidenceRequired     Watchpoint = "relation_evidence_required"
)

// RowSpec contains one normalized passive relation inventory row.
type RowSpec struct {
	SubjectKey            string
	HasManagedInstanceKey bool
	ManagedInstanceKey    string
}

// Row is one passive relation observation.
type Row struct {
	subjectKey         hostrelation.SubjectKey
	managedInstanceKey hostrelation.ManagedInstanceKey
	hasManagedKey      bool
}

// NewRow validates and constructs a passive relation inventory row.
func NewRow(spec RowSpec) (Row, error) {
	if err := validateFreeText("subject key", spec.SubjectKey); err != nil {
		return Row{}, err
	}
	subjectKey, err := hostrelation.NewSubjectKey(spec.SubjectKey)
	if err != nil {
		return Row{}, err
	}
	var managedKey hostrelation.ManagedInstanceKey
	if spec.HasManagedInstanceKey {
		if err := validateFreeText("managed instance key", spec.ManagedInstanceKey); err != nil {
			return Row{}, err
		}
		managedKey, err = hostrelation.NewManagedInstanceKey(spec.ManagedInstanceKey)
		if err != nil {
			return Row{}, err
		}
	} else if spec.ManagedInstanceKey != "" {
		return Row{}, fmt.Errorf("managed instance key requires HasManagedInstanceKey")
	}
	return Row{
		subjectKey:         subjectKey,
		managedInstanceKey: managedKey,
		hasManagedKey:      spec.HasManagedInstanceKey,
	}, nil
}

// SubjectKey returns the host-visible relation key.
func (row Row) SubjectKey() hostrelation.SubjectKey { return row.subjectKey }

// ManagedInstanceKey returns the observed structural correlation key, if present.
func (row Row) ManagedInstanceKey() (hostrelation.ManagedInstanceKey, bool) {
	return row.managedInstanceKey, row.hasManagedKey
}

// InventorySpec contains passive inventory evidence for one observation batch.
type InventorySpec struct {
	Availability InventoryAvailability
	Freshness    EvidenceFreshness
	Rows         []Row
}

// Inventory is a passive relation inventory evidence batch.
type Inventory struct {
	availability InventoryAvailability
	freshness    EvidenceFreshness
	rows         []Row
}

// UnsupportedInventory returns a passive evidence batch for unsupported
// relation inventory observers.
func UnsupportedInventory() Inventory {
	return Inventory{
		availability: InventoryUnsupported,
		freshness:    EvidenceFresh,
	}
}

// NewInventory validates and constructs a passive inventory batch.
func NewInventory(spec InventorySpec) (Inventory, error) {
	if err := validateAvailability(spec.Availability); err != nil {
		return Inventory{}, err
	}
	if err := validateFreshness(spec.Freshness); err != nil {
		return Inventory{}, err
	}
	if spec.Availability != InventorySupported && len(spec.Rows) > 0 {
		return Inventory{}, fmt.Errorf("%s inventory must not include rows", spec.Availability)
	}
	return Inventory{
		availability: spec.Availability,
		freshness:    spec.Freshness,
		rows:         cloneRows(spec.Rows),
	}, nil
}

// Availability returns passive inventory support status.
func (inventory Inventory) Availability() InventoryAvailability { return inventory.availability }

// Freshness returns passive inventory freshness status.
func (inventory Inventory) Freshness() EvidenceFreshness { return inventory.freshness }

// Rows returns passive inventory rows.
func (inventory Inventory) Rows() []Row { return cloneRows(inventory.rows) }

// CorrelationResult records passive correlation without granting mutation authority.
type CorrelationResult struct {
	state                CorrelationState
	reason               ReasonCode
	evidenceAvailability InventoryAvailability
	evidenceFreshness    EvidenceFreshness
	rows                 []Row
	sameSubjectRows      []Row
	managedKeyRows       []Row
	watchpoints          []Watchpoint
}

// Correlate classifies passive inventory against one selected expected relation.
func Correlate(
	subject hostrelation.ExpectedRelation,
	inventory Inventory,
) CorrelationResult {
	rows := inventory.Rows()
	if inventory.Availability() == InventoryUnsupported {
		return newResult(
			inventory.Availability(),
			inventory.Freshness(),
			StateUnsupported,
			ReasonUnsupportedInventory,
			rows,
			nil,
			nil,
			[]Watchpoint{WatchpointPassiveInventoryRequired},
		)
	}
	if inventory.Freshness() == EvidenceStale {
		return newResult(
			inventory.Availability(),
			inventory.Freshness(),
			StateStaleEvidence,
			ReasonStaleEvidence,
			rows,
			nil,
			nil,
			[]Watchpoint{WatchpointFreshInventoryRequired},
		)
	}
	if inventory.Availability() == InventoryUnavailable {
		return newResult(
			inventory.Availability(),
			inventory.Freshness(),
			StateUnavailableEvidence,
			ReasonUnavailableEvidence,
			rows,
			nil,
			nil,
			[]Watchpoint{WatchpointRelationEvidenceRequired},
		)
	}

	expectedSubjectKey := subject.SubjectKey()
	expectedManagedKey := subject.ManagedInstanceKey()
	var exactRows []Row
	var sameSubjectRows []Row
	var managedKeyRows []Row
	for _, row := range rows {
		managedKey, hasManagedKey := row.ManagedInstanceKey()
		sameSubject := row.SubjectKey() == expectedSubjectKey
		sameManagedKey := hasManagedKey && managedKey == expectedManagedKey
		if sameSubject {
			sameSubjectRows = append(sameSubjectRows, row)
		}
		if sameManagedKey {
			managedKeyRows = append(managedKeyRows, row)
		}
		if sameSubject && sameManagedKey {
			exactRows = append(exactRows, row)
		}
	}

	switch {
	case len(exactRows) == 1 && len(sameSubjectRows) == 1 && len(managedKeyRows) == 1:
		return newResult(inventory.Availability(), inventory.Freshness(), StateExactCorrelation, ReasonNone, rows, sameSubjectRows, managedKeyRows, nil)
	case len(managedKeyRows) > 1 || len(exactRows) > 1:
		return newResult(inventory.Availability(), inventory.Freshness(), StateAmbiguous, ReasonAmbiguous, rows, sameSubjectRows, managedKeyRows, []Watchpoint{WatchpointReplacementAuthorityRequired})
	case len(exactRows) == 1 && len(sameSubjectRows) > 1:
		return newResult(inventory.Availability(), inventory.Freshness(), StateSameSubjectShadow, ReasonSameSubjectShadow, rows, sameSubjectRows, managedKeyRows, []Watchpoint{WatchpointReplacementAuthorityRequired})
	case len(managedKeyRows) == 1:
		if len(sameSubjectRows) > 0 {
			return newResult(inventory.Availability(), inventory.Freshness(), StateAmbiguous, ReasonAmbiguous, rows, sameSubjectRows, managedKeyRows, []Watchpoint{WatchpointReplacementAuthorityRequired})
		}
		return newResult(inventory.Availability(), inventory.Freshness(), StateManagedKeyDrift, ReasonManagedKeyDrift, rows, sameSubjectRows, managedKeyRows, []Watchpoint{WatchpointReplacementAuthorityRequired})
	case len(sameSubjectRows) > 1:
		return newResult(inventory.Availability(), inventory.Freshness(), StateSameSubjectShadow, ReasonSameSubjectShadow, rows, sameSubjectRows, managedKeyRows, []Watchpoint{WatchpointReplacementAuthorityRequired})
	case len(sameSubjectRows) == 1:
		if _, hasManagedKey := sameSubjectRows[0].ManagedInstanceKey(); hasManagedKey {
			return newResult(inventory.Availability(), inventory.Freshness(), StateSameSubjectShadow, ReasonSameSubjectShadow, rows, sameSubjectRows, managedKeyRows, []Watchpoint{WatchpointReplacementAuthorityRequired})
		}
		return newResult(inventory.Availability(), inventory.Freshness(), StateUnkeyedSameSubject, ReasonUnkeyedSameSubject, rows, sameSubjectRows, managedKeyRows, nil)
	default:
		return newResult(inventory.Availability(), inventory.Freshness(), StateMissing, ReasonMissing, rows, sameSubjectRows, managedKeyRows, nil)
	}
}

// State returns the passive correlation state.
func (result CorrelationResult) State() CorrelationState { return result.state }

// Reason returns the passive correlation reason.
func (result CorrelationResult) Reason() ReasonCode { return result.reason }

// EvidenceAvailability returns the passive inventory availability consumed by
// this correlation result.
func (result CorrelationResult) EvidenceAvailability() InventoryAvailability {
	return result.evidenceAvailability
}

// EvidenceFreshness returns the passive inventory freshness consumed by this
// correlation result.
func (result CorrelationResult) EvidenceFreshness() EvidenceFreshness {
	return result.evidenceFreshness
}

// Rows returns the passive rows used for classification.
func (result CorrelationResult) Rows() []Row { return cloneRows(result.rows) }

// SameSubjectRows returns rows sharing the expected host-visible relation key.
func (result CorrelationResult) SameSubjectRows() []Row {
	return cloneRows(result.sameSubjectRows)
}

// ManagedKeyRows returns rows sharing the expected daem managed-instance key.
func (result CorrelationResult) ManagedKeyRows() []Row {
	return cloneRows(result.managedKeyRows)
}

// Watchpoints returns follow-up guidance that grants no mutation authority.
func (result CorrelationResult) Watchpoints() []Watchpoint {
	return append([]Watchpoint(nil), result.watchpoints...)
}

func newResult(
	evidenceAvailability InventoryAvailability,
	evidenceFreshness EvidenceFreshness,
	state CorrelationState,
	reason ReasonCode,
	rows []Row,
	sameSubjectRows []Row,
	managedKeyRows []Row,
	watchpoints []Watchpoint,
) CorrelationResult {
	return CorrelationResult{
		state:                state,
		reason:               reason,
		evidenceAvailability: evidenceAvailability,
		evidenceFreshness:    evidenceFreshness,
		rows:                 cloneRows(rows),
		sameSubjectRows:      cloneRows(sameSubjectRows),
		managedKeyRows:       cloneRows(managedKeyRows),
		watchpoints:          append([]Watchpoint(nil), watchpoints...),
	}
}

func cloneRows(rows []Row) []Row {
	return append([]Row(nil), rows...)
}

func validateAvailability(availability InventoryAvailability) error {
	switch availability {
	case InventorySupported, InventoryUnsupported, InventoryUnavailable:
		return nil
	default:
		return fmt.Errorf("unsupported inventory availability %q", availability)
	}
}

func validateFreshness(freshness EvidenceFreshness) error {
	switch freshness {
	case EvidenceFresh, EvidenceStale:
		return nil
	default:
		return fmt.Errorf("unsupported evidence freshness %q", freshness)
	}
}

func validateFreeText(label string, value string) error {
	if strings.TrimSpace(value) != value || value == "" {
		return fmt.Errorf("%s must be non-empty and trimmed", label)
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return fmt.Errorf("%s must not contain control characters", label)
		}
	}
	return nil
}
