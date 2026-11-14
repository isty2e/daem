package pathreferencecodec

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func EncodeTable(table pathauthority.ReferenceTable) ([]Record, error) {
	if err := table.Validate(); err != nil {
		return nil, err
	}
	values := table.References()
	records := make([]Record, len(values))
	for index, reference := range values {
		var err error
		records[index], err = Encode(reference)
		if err != nil {
			return nil, err
		}
	}
	return records, nil
}

// DecodeTable requires unique wire identities; builders may merge equal evidence.
func DecodeTable(records []Record) ([]pathauthority.Reference, error) {
	if records == nil {
		return nil, fmt.Errorf("path reference table is required")
	}
	references := make([]pathauthority.Reference, len(records))
	seen := make(map[pathauthority.Exact]struct{}, len(records))
	for index, record := range records {
		var err error
		references[index], err = Decode(record)
		if err != nil {
			return nil, fmt.Errorf("path reference[%d]: %w", index, err)
		}
		identity := references[index].Identity()
		if _, present := seen[identity]; present {
			return nil, fmt.Errorf("path reference[%d] repeats identity %q", index, identity.Key())
		}
		seen[identity] = struct{}{}
	}
	return references, nil
}
