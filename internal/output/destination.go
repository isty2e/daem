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
	homeDestinationPrefix      = "~/"
	dataDestinationPrefix      = "@data/"
	piAgentDestinationPrefix   = "@pi-agent/"
	codexHomeDestinationPrefix = "@codex-home/"
)

// RootRole identifies the portable root selected by a destination.
type RootRole string

const (
	RootProject RootRole = "project"
	RootHome    RootRole = "home"
	RootData    RootRole = "data"
	// RootPiAgent identifies Pi's host-selected global agent root.
	RootPiAgent RootRole = "pi_agent"
	// RootCodexHome identifies Codex's host-selected global home root.
	RootCodexHome RootRole = "codex_home"
)

// RootReference names a selected root and its versioned locator contract. It is
// neither a portable root spelling nor evidence of filesystem authority.
type RootReference struct {
	identity      string
	locatorPolicy string
}

func NewRootReference(identity, locatorPolicy string) (RootReference, error) {
	if err := validateDestinationText("root identity", identity); err != nil {
		return RootReference{}, err
	}
	if err := validateDestinationText("root locator policy", locatorPolicy); err != nil {
		return RootReference{}, err
	}
	return RootReference{identity: identity, locatorPolicy: locatorPolicy}, nil
}

func (reference RootReference) Identity() string      { return reference.identity }
func (reference RootReference) LocatorPolicy() string { return reference.locatorPolicy }

func (reference RootReference) Validate() error {
	_, err := NewRootReference(reference.identity, reference.locatorPolicy)
	return err
}

// Destination is one canonical portable output root plus a slash-separated
// relative path. Its zero value is invalid.
type Destination struct {
	root     RootRole
	relative string
}

// Parse parses the one canonical persisted spelling of a destination.
func Parse(value string) (Destination, error) {
	if err := validateDestinationText("destination", value); err != nil {
		return Destination{}, err
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
	case strings.HasPrefix(value, piAgentDestinationPrefix):
		root = RootPiAgent
		relative = strings.TrimPrefix(value, piAgentDestinationPrefix)
	case strings.HasPrefix(value, codexHomeDestinationPrefix):
		root = RootCodexHome
		relative = strings.TrimPrefix(value, codexHomeDestinationPrefix)
	case strings.HasPrefix(value, "~"):
		return Destination{}, fmt.Errorf("destination home-relative path must begin with %s", homeDestinationPrefix)
	case strings.HasPrefix(value, "@"):
		return Destination{}, fmt.Errorf("destination has unknown reserved root role")
	}

	destination, err := NewDestination(root, relative)
	if err != nil {
		return Destination{}, fmt.Errorf("destination: %w", err)
	}
	if destination.String() != value {
		return Destination{}, fmt.Errorf("destination must use its canonical portable spelling")
	}
	return destination, nil
}

// NewDestination composes a canonical role and relative path without parsing
// another package's copy of portable root spellings.
func NewDestination(root RootRole, relative string) (Destination, error) {
	if !root.valid() {
		return Destination{}, fmt.Errorf("unsupported destination root role %q", root)
	}
	if err := validateRelativeDestinationPath(relative); err != nil {
		return Destination{}, err
	}
	if root == RootProject && (strings.HasPrefix(relative, "~") || strings.HasPrefix(relative, "@")) {
		return Destination{}, fmt.Errorf("project relative path must not use a reserved root prefix")
	}
	return Destination{root: root, relative: relative}, nil
}

// RootRole returns the destination's root role.
func (destination Destination) RootRole() RootRole { return destination.root }

// RelativePath returns the canonical path relative to the selected root.
func (destination Destination) RelativePath() string { return destination.relative }

// String returns the canonical persisted spelling.
func (destination Destination) String() string {
	switch destination.root {
	case RootProject:
		return destination.relative
	case RootHome:
		return homeDestinationPrefix + destination.relative
	case RootData:
		return dataDestinationPrefix + destination.relative
	case RootPiAgent:
		return piAgentDestinationPrefix + destination.relative
	case RootCodexHome:
		return codexHomeDestinationPrefix + destination.relative
	default:
		return ""
	}
}

// Validate rejects a zero, forged, or non-canonical destination.
func (destination Destination) Validate() error {
	canonical, err := NewDestination(destination.root, destination.relative)
	if err != nil {
		return err
	}
	if canonical != destination {
		return fmt.Errorf("destination is not canonical")
	}
	return nil
}

// ValidateScope rejects root roles that contradict project/global placement.
func (destination Destination) ValidateScope(scope target.Scope) error {
	if err := destination.Validate(); err != nil {
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
		if destination.root != RootHome && destination.root != RootData &&
			destination.root != RootPiAgent && destination.root != RootCodexHome {
			return fmt.Errorf("global destination must use a global root role")
		}
	}
	return nil
}

func (root RootRole) valid() bool {
	return root == RootProject || root == RootHome || root == RootData ||
		root == RootPiAgent || root == RootCodexHome
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
