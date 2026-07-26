package opencode

import "fmt"

// ConfigKind identifies one OpenCode plugin contribution document.
type ConfigKind string

const (
	ConfigServer ConfigKind = "server"
	ConfigTUI    ConfigKind = "tui"
)

// CandidateNames returns the host-selected config filenames in precedence
// order. The first candidate is also the creation/default-absence path.
func CandidateNames(kind ConfigKind) ([]string, error) {
	switch kind {
	case ConfigServer:
		return []string{"opencode.json", "opencode.jsonc"}, nil
	case ConfigTUI:
		return []string{"tui.json", "tui.jsonc"}, nil
	default:
		return nil, fmt.Errorf("unsupported OpenCode config kind %q", kind)
	}
}

// SelectName mirrors OpenCode install selection: first existing candidate, or
// the JSON candidate when neither file exists.
func SelectName(kind ConfigKind, exists func(string) (bool, error)) (string, error) {
	if exists == nil {
		return "", fmt.Errorf("OpenCode config existence observer is required")
	}
	candidates, err := CandidateNames(kind)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		present, err := exists(candidate)
		if err != nil {
			return "", fmt.Errorf("inspect OpenCode config candidate %q: %w", candidate, err)
		}
		if present {
			return candidate, nil
		}
	}
	return candidates[0], nil
}
