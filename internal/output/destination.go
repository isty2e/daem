package output

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/target"
)

const (
	homeDestinationPrefix = "~/"
	dataDestinationPrefix = "@data/"
)

// RootRole identifies the portable root selected by a destination.
type RootRole string

const (
	RootProject RootRole = "project"
	RootHome    RootRole = "home"
	RootData    RootRole = "data"
)

// Portable is one canonical root role plus a slash-separated relative path.
type Portable struct {
	root     RootRole
	relative string
}

// Parse parses the one canonical persisted spelling of a destination.
func Parse(value string) (Portable, error) {
	if err := validateDestinationText("destination", value); err != nil {
		return Portable{}, err
	}

	root := RootProject
	relative := value
	switch {
	case strings.HasPrefix(value, homeDestinationPrefix):
		root = RootHome
		relative = strings.TrimPrefix(value, homeDestinationPrefix)
	case strings.HasPrefix(value, dataDestinationPrefix):
		root = RootData
		relative = strings.TrimPrefix(value, dataDestinationPrefix)
	case strings.HasPrefix(value, "~"):
		return Portable{}, fmt.Errorf("destination home-relative path must begin with %s", homeDestinationPrefix)
	case strings.HasPrefix(value, "@"):
		return Portable{}, fmt.Errorf("destination has unknown reserved root role")
	}

	destination, err := newPortable(root, relative)
	if err != nil {
		return Portable{}, fmt.Errorf("destination: %w", err)
	}
	if destination.String() != value {
		return Portable{}, fmt.Errorf("destination must use its canonical portable spelling")
	}
	return destination, nil
}

func newPortable(root RootRole, relative string) (Portable, error) {
	if !root.valid() {
		return Portable{}, fmt.Errorf("unsupported destination root role %q", root)
	}
	if err := validateRelativeDestinationPath(relative); err != nil {
		return Portable{}, err
	}
	if root == RootProject && (strings.HasPrefix(relative, "~") || strings.HasPrefix(relative, "@")) {
		return Portable{}, fmt.Errorf("project relative path must not use a reserved root prefix")
	}
	return Portable{root: root, relative: relative}, nil
}

// RootRole returns the destination's portable root role.
func (destination Portable) RootRole() RootRole { return destination.root }

// RelativePath returns the canonical path relative to the selected root.
func (destination Portable) RelativePath() string { return destination.relative }

// String returns the canonical persisted spelling.
func (destination Portable) String() string {
	switch destination.root {
	case RootProject:
		return destination.relative
	case RootHome:
		return homeDestinationPrefix + destination.relative
	case RootData:
		return dataDestinationPrefix + destination.relative
	default:
		return ""
	}
}

// ValidateScope rejects root roles that contradict project/global placement.
func (destination Portable) ValidateScope(scope target.Scope) error {
	if err := destination.validate(); err != nil {
		return err
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return err
	}
	switch scope {
	case target.ScopeProject:
		if destination.root != RootProject {
			return fmt.Errorf("project destination must be relative to the selected project root")
		}
	case target.ScopeGlobal:
		if destination.root != RootHome && destination.root != RootData {
			return fmt.Errorf("global destination must be home-relative or data-root-relative")
		}
	}
	return nil
}

func (destination Portable) validate() error {
	canonical, err := newPortable(destination.root, destination.relative)
	if err != nil {
		return err
	}
	if canonical != destination {
		return fmt.Errorf("portable destination is not canonical")
	}
	return nil
}

func (root RootRole) valid() bool {
	return root == RootProject || root == RootHome || root == RootData
}

// Validate rejects a non-portable or non-canonical destination.
func (destination Destination) Validate() error {
	_, err := Parse(string(destination))
	return err
}

// ValidateScope rejects a destination whose root role contradicts scope.
func (destination Destination) ValidateScope(scope target.Scope) error {
	portable, err := Parse(string(destination))
	if err != nil {
		return err
	}
	return portable.ValidateScope(scope)
}

func validateRelativeDestinationPath(value string) error {
	if err := validateDestinationText("relative path", value); err != nil {
		return err
	}
	if strings.Contains(value, `\`) {
		return fmt.Errorf("relative path must use slash separators")
	}
	if path.IsAbs(value) || windowsDriveDestinationPath(value) {
		return fmt.Errorf("relative path must not be absolute")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != value {
		return fmt.Errorf("relative path must be canonical and stay inside its selected root")
	}
	return nil
}

func validateDestinationText(label string, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be non-empty and trimmed", label)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("%s must not contain control characters", label)
	}
	return nil
}

func windowsDriveDestinationPath(value string) bool {
	return len(value) >= 2 &&
		((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) &&
		value[1] == ':'
}
