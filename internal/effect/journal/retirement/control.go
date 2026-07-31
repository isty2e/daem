package retirement

import (
	"fmt"
	"strings"
)

// ControlEvidence carries one complete, stable control-tree observation.
type ControlEvidence struct {
	Directory     EntryEvidence
	Children      []EntryEvidence
	RecordContent []byte
}

// Control is one fully validated journal-retirement control.
type Control struct {
	record Record
}

// ValidateControl converts stable filesystem evidence into a canonical
// retirement control.
func ValidateControl(evidence ControlEvidence) (Control, error) {
	name := InspectName(evidence.Directory.name)
	if name.kind != NameControl {
		return Control{}, fmt.Errorf("entry %q is not a valid retirement control name", evidence.Directory.name)
	}
	if err := validatePrivateDirectory(evidence.Directory, "retirement control"); err != nil {
		return Control{}, err
	}

	recordCount := 0
	seenChildren := make(map[string]struct{}, len(evidence.Children))
	for _, child := range evidence.Children {
		if _, duplicate := seenChildren[child.name]; duplicate {
			return Control{}, fmt.Errorf(
				"retirement control contains duplicate child %q",
				child.name,
			)
		}
		seenChildren[child.name] = struct{}{}
		switch {
		case child.name == RecordFileName:
			recordCount++
			if err := validatePrivateRecordFile(child, "retirement record"); err != nil {
				return Control{}, err
			}
			if child.size != int64(len(evidence.RecordContent)) {
				return Control{}, fmt.Errorf(
					"retirement record size is %d, observed content has %d bytes",
					child.size,
					len(evidence.RecordContent),
				)
			}
		case strings.HasPrefix(child.name, temporaryRecordPrefix) &&
			len(child.name) > len(temporaryRecordPrefix):
			if err := validatePrivateRecordFile(child, "retirement record temporary"); err != nil {
				return Control{}, err
			}
		default:
			return Control{}, fmt.Errorf(
				"retirement control contains unexpected child %q",
				child.name,
			)
		}
	}
	if recordCount != 1 {
		return Control{}, fmt.Errorf(
			"retirement control requires exactly one %s entry, found %d",
			RecordFileName,
			recordCount,
		)
	}

	record, err := Decode(evidence.RecordContent)
	if err != nil {
		return Control{}, err
	}
	if !record.matchesControlName(name) {
		return Control{}, fmt.Errorf(
			"retirement record identity does not match control name %q",
			name.value,
		)
	}
	return Control{record: record}, nil
}

// Record returns the validated durable control record.
func (control Control) Record() Record {
	return control.record
}
