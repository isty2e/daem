// Package opencodeorder interprets supplied OpenCode plugin documents and
// their independently selected physical orders. It does not discover files.
package opencodeorder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"

	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
)

// MaximumConfigBytes bounds one selected OpenCode config observation and its
// corresponding rooted mutation read.
const MaximumConfigBytes = 4 << 20

// Document is one immutable selected OpenCode config observation.
type Document struct {
	kind     opencodeconfig.ConfigKind
	path     string
	exists   bool
	revision string
	content  []byte
	parsed   opencodeconfig.Document
}

// Kind returns the selected server or TUI config family.
func (document Document) Kind() opencodeconfig.ConfigKind { return document.kind }

// Path returns the selected config authority path.
func (document Document) Path() string { return document.path }

// Exists reports whether the selected config file existed.
func (document Document) Exists() bool { return document.exists }

// Revision returns a content digest suitable for compare-and-swap evidence.
func (document Document) Revision() string { return document.revision }

// Entries returns a defensive copy in physical config order.
func (document Document) Entries() []opencodeconfig.Entry {
	return document.parsed.Entries()
}

// ExactSourceCount returns the number of exact rows in this selected document.
func (document Document) ExactSourceCount(source string) int {
	return document.parsed.ExactSourceCount(source)
}

// NewDocument binds one supplied config image to its declaring candidate path.
func NewDocument(
	kind opencodeconfig.ConfigKind,
	path string,
	content []byte,
	exists bool,
) (Document, error) {
	if len(content) > MaximumConfigBytes {
		return Document{}, fmt.Errorf("OpenCode %s config exceeds the config byte limit", kind)
	}
	if !exists && len(content) != 0 {
		return Document{}, fmt.Errorf("absent OpenCode %s config has content", kind)
	}
	names, err := opencodeconfig.CandidateNames(kind)
	if err != nil {
		return Document{}, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !slices.Contains(names, filepath.Base(path)) {
		return Document{}, fmt.Errorf("OpenCode %s config path %q must be a clean absolute candidate path", kind, path)
	}
	document := Document{
		kind:     kind,
		path:     path,
		exists:   exists,
		revision: contentRevision(content),
		content:  append([]byte(nil), content...),
	}
	if !exists {
		return document, nil
	}
	parsed, err := opencodeconfig.ParseAt(content, path)
	if err != nil {
		return Document{}, fmt.Errorf("observe OpenCode %s config %q: %w", kind, path, err)
	}
	document.parsed = parsed
	return document, nil
}

func contentRevision(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}
