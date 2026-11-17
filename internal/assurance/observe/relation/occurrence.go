package relation

import "fmt"

// Occurrence identifies an actual inventory row within one observation epoch.
// It is neither a managed-instance key nor durable ownership evidence.
type Occurrence struct {
	origin  string
	locator string
}

func NewOccurrence(origin, locator string) (Occurrence, error) {
	if err := validateFreeText("inventory occurrence origin", origin); err != nil {
		return Occurrence{}, err
	}
	if err := validateFreeText("inventory occurrence locator", locator); err != nil {
		return Occurrence{}, err
	}
	return Occurrence{origin: origin, locator: locator}, nil
}

func (occurrence Occurrence) Validate() error {
	_, err := NewOccurrence(occurrence.origin, occurrence.locator)
	return err
}

// WithOccurrences binds the row to its inventory origins without changing its
// request-specific correlation keys.
func (row Row) WithOccurrences(occurrences []Occurrence) (Row, error) {
	for _, occurrence := range occurrences {
		if err := occurrence.Validate(); err != nil {
			return Row{}, err
		}
	}
	row.occurrences = append([]Occurrence(nil), occurrences...)
	return row, nil
}

func (row Row) Occurrences() []Occurrence {
	return append([]Occurrence(nil), row.occurrences...)
}

// CoveredOccurrences returns actual rows explaining an admitted correlation.
func (result CorrelationResult) CoveredOccurrences() []Occurrence {
	var occurrences []Occurrence
	for _, row := range result.sameSubjectRows {
		occurrences = append(occurrences, row.occurrences...)
	}
	return occurrences
}

// WithoutOccurrences produces a prospective view, never a new physical read.
func (batch Batch) WithoutOccurrences(retired []Occurrence) (Batch, error) {
	selected := make(map[Occurrence]struct{}, len(retired))
	for _, occurrence := range retired {
		if err := occurrence.Validate(); err != nil {
			return Batch{}, fmt.Errorf("retired occurrence: %w", err)
		}
		selected[occurrence] = struct{}{}
	}
	projected := batch.Clone()
	for key, current := range batch.correlations {
		var rows []Row
		for _, row := range current.rows {
			remaining := make([]Occurrence, 0, len(row.occurrences))
			for _, occurrence := range row.occurrences {
				if _, removed := selected[occurrence]; !removed {
					remaining = append(remaining, occurrence)
				}
			}
			if len(row.occurrences) != 0 && len(remaining) == 0 {
				continue
			}
			row.occurrences = remaining
			rows = append(rows, row)
		}
		inventory, err := NewInventory(InventorySpec{
			Availability: current.evidenceAvailability,
			Freshness:    current.evidenceFreshness,
			Rows:         rows,
		})
		if err != nil {
			return Batch{}, err
		}
		projected.correlations[key] = Correlate(key.ExpectedRelation(), inventory)
	}
	return projected, nil
}
