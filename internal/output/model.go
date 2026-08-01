package output

import (
	"fmt"
	"strings"
	"unicode"
)

// ContentPath identifies the managed projection within a destination.
// The empty value means the whole destination path is managed.
type ContentPath string

// Validate rejects a non-canonical managed projection path. The empty path
// denotes the whole destination.
func (path ContentPath) Validate() error {
	value := string(path)
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("managed address content path must not have surrounding whitespace")
	}
	if value == "/" || !strings.HasPrefix(value, "/") {
		return fmt.Errorf("managed address content path %q must identify an absolute non-root projection", value)
	}
	if strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return fmt.Errorf("managed address content path %q must be canonical", value)
	}
	if strings.IndexFunc(value, func(r rune) bool { return r == '\x00' || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("managed address content path contains a control character")
	}
	for segment := range strings.SplitSeq(strings.TrimPrefix(value, "/"), "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("managed address content path %q contains a relative segment", value)
		}
	}
	return nil
}

// Overlaps reports whether two projection paths intersect within one physical
// destination. The empty path owns the whole destination.
func (path ContentPath) Overlaps(other ContentPath) bool {
	if path == "" || other == "" {
		return true
	}
	return contentPathContains(path, other) || contentPathContains(other, path)
}

func contentPathContains(parent ContentPath, child ContentPath) bool {
	return parent == child || strings.HasPrefix(string(child), string(parent)+"/")
}
