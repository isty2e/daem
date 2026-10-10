package mcp

import (
	"fmt"

	"github.com/isty2e/daem/internal/target"
)

// Backend identifies the recorded implementation of a binding, not a host observation.
type Backend string

const (
	BackendNative  Backend = "native"
	BackendAdapter Backend = "adapter"
)

// ParseBackend normalizes declaration intent. Omitted Pi intent remains legacy adapter intent.
func ParseBackend(selected target.Target, value string) (Backend, error) {
	if value == "" {
		if selected == target.TargetPi {
			return BackendAdapter, nil
		}
		return BackendNative, nil
	}
	if selected != target.TargetPi {
		return "", fmt.Errorf("MCP backend selection is supported only for Pi")
	}
	switch Backend(value) {
	case BackendNative, BackendAdapter:
		return Backend(value), nil
	default:
		return "", fmt.Errorf("unsupported Pi MCP backend %q", value)
	}
}
