package retirement

import (
	"fmt"
	"io/fs"
	"strings"
)

// EntryKind is a no-follow structural observation at the journal boundary.
type EntryKind string

const (
	EntryRegular   EntryKind = "regular"
	EntryDirectory EntryKind = "directory"
	EntrySymlink   EntryKind = "symlink"
	EntrySpecial   EntryKind = "special"
)

// EntryEvidence is one normalized no-follow directory-entry observation.
type EntryEvidence struct {
	name  string
	kind  EntryKind
	mode  fs.FileMode
	owned bool
	size  int64
}

// NewEntryEvidence normalizes boundary facts without interpreting artifact
// semantics.
func NewEntryEvidence(
	name string,
	kind EntryKind,
	mode fs.FileMode,
	owned bool,
	size int64,
) (EntryEvidence, error) {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\x00') {
		return EntryEvidence{}, fmt.Errorf("entry name %q is not a safe directory component", name)
	}
	switch kind {
	case EntryRegular, EntryDirectory, EntrySymlink, EntrySpecial:
	default:
		return EntryEvidence{}, fmt.Errorf("entry %q has unsupported kind %q", name, kind)
	}
	if mode&^fs.ModePerm != 0 {
		return EntryEvidence{}, fmt.Errorf("entry %q mode must contain permission bits only", name)
	}
	if size < 0 {
		return EntryEvidence{}, fmt.Errorf("entry %q size must not be negative", name)
	}
	return EntryEvidence{name: name, kind: kind, mode: mode, owned: owned, size: size}, nil
}

func validatePrivateDirectory(evidence EntryEvidence, subject string) error {
	if evidence.kind != EntryDirectory {
		return fmt.Errorf("%s %q must be a no-follow directory", subject, evidence.name)
	}
	if !evidence.owned {
		return fmt.Errorf("%s %q is not owned by the invoking user", subject, evidence.name)
	}
	if evidence.mode != DirectoryMode {
		return fmt.Errorf(
			"%s %q permissions are %04o, want %04o",
			subject,
			evidence.name,
			evidence.mode,
			DirectoryMode,
		)
	}
	return nil
}

func validatePrivateRecordFile(evidence EntryEvidence, subject string) error {
	if evidence.kind != EntryRegular {
		return fmt.Errorf("%s %q must be a no-follow regular file", subject, evidence.name)
	}
	if !evidence.owned {
		return fmt.Errorf("%s %q is not owned by the invoking user", subject, evidence.name)
	}
	if evidence.mode != RecordMode {
		return fmt.Errorf(
			"%s %q permissions are %04o, want %04o",
			subject,
			evidence.name,
			evidence.mode,
			RecordMode,
		)
	}
	if evidence.size > MaximumRecordBytes {
		return fmt.Errorf("%s %q exceeds %d bytes", subject, evidence.name, MaximumRecordBytes)
	}
	return nil
}
