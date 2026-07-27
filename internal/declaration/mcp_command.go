package declaration

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// MCPCommandKind identifies one admitted MCP command boundary form.
type MCPCommandKind uint8

const (
	MCPCommandKindUnspecified MCPCommandKind = iota
	MCPCommandKindAmbient
	MCPCommandKindAbsolutePath
)

// MCPCommand preserves whether an MCP command was authored as an ambient
// executable token or an explicit absolute executable path.
type MCPCommand struct {
	kind  MCPCommandKind
	value string
}

// NewMCPAmbientCommand constructs the boundary form for a PATH-resolved token.
// Canonical command validation belongs to the desired MCP model.
func NewMCPAmbientCommand(value string) MCPCommand {
	return MCPCommand{kind: MCPCommandKindAmbient, value: value}
}

func newMCPAbsolutePathCommand(value string) MCPCommand {
	return MCPCommand{kind: MCPCommandKindAbsolutePath, value: value}
}

// MCPCommandFromExecutable selects the lossless manifest form for one observed
// canonical argv[0] value.
func MCPCommandFromExecutable(value string) MCPCommand {
	if filepath.IsAbs(value) {
		return newMCPAbsolutePathCommand(value)
	}
	return NewMCPAmbientCommand(value)
}

// Kind returns the authored command form.
func (command MCPCommand) Kind() MCPCommandKind {
	return command.kind
}

// Value returns the authored executable token or path.
func (command MCPCommand) Value() string {
	return command.value
}

// UnmarshalTOML admits either a portable command string or a command object
// containing exactly one path key.
func (command *MCPCommand) UnmarshalTOML(value any) error {
	decoded, err := mcpCommandFromTOMLValue(value)
	if err != nil {
		return err
	}
	*command = decoded
	return nil
}

// MarshalTOML preserves the selected command boundary form.
func (command MCPCommand) MarshalTOML() ([]byte, error) {
	switch command.kind {
	case MCPCommandKindAmbient:
		return []byte(strconv.Quote(command.value)), nil
	case MCPCommandKindAbsolutePath:
		return []byte("{ path = " + strconv.Quote(command.value) + " }"), nil
	default:
		return nil, fmt.Errorf("MCP command is not initialized")
	}
}

func mcpCommandFromTOMLValue(value any) (MCPCommand, error) {
	switch typed := value.(type) {
	case string:
		return NewMCPAmbientCommand(typed), nil
	case map[string]any:
		rawPath, ok := typed["path"]
		if len(typed) != 1 || !ok {
			return MCPCommand{}, fmt.Errorf(`command object must contain exactly one key named "path"`)
		}
		path, ok := rawPath.(string)
		if !ok {
			return MCPCommand{}, fmt.Errorf("command path must be a string")
		}
		return newMCPAbsolutePathCommand(path), nil
	default:
		return MCPCommand{}, fmt.Errorf("command must be a portable token string or an object containing exactly one path key")
	}
}
