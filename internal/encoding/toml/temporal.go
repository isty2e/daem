package tomlsyntax

import (
	"fmt"
	"time"
)

// MarshalTOML keeps native temporal meaning when a parsed map is re-encoded.
func (temporal Temporal) MarshalTOML() ([]byte, error) {
	var layout string
	switch temporal.Kind {
	case "offset-date-time":
		layout = time.RFC3339Nano
	case "local-date-time":
		layout = "2006-01-02T15:04:05.999999999"
	case "local-date":
		layout = "2006-01-02"
	case "local-time":
		layout = "15:04:05.999999999"
	default:
		return nil, fmt.Errorf("unknown TOML temporal kind")
	}
	if _, err := time.Parse(layout, temporal.Text); err != nil {
		return nil, fmt.Errorf("invalid TOML temporal value: %w", err)
	}
	return []byte(temporal.Text), nil
}
