package mcpcodec

import (
	"bytes"
	"fmt"
)

type mcpProjectionMutationKind uint8

const (
	mcpProjectionMutationInsert mcpProjectionMutationKind = iota + 1
	mcpProjectionMutationUpsert
	mcpProjectionMutationRemove
)

// MCPProjectionMutation is one validated, immutable projection change inside an
// MCP host aggregate. Insert refuses an existing same-name entry; upsert may
// replace one because the caller has already established replacement authority.
type MCPProjectionMutation struct {
	kind      mcpProjectionMutationKind
	serverID  string
	canonical []byte
}

// NewMCPProjectionInsert creates a mutation that requires serverID to be absent.
func NewMCPProjectionInsert(serverID string, canonical []byte) (MCPProjectionMutation, error) {
	return newMCPProjectionWriteMutation(mcpProjectionMutationInsert, serverID, canonical)
}

// NewMCPProjectionUpsert creates a mutation allowed to replace serverID.
func NewMCPProjectionUpsert(serverID string, canonical []byte) (MCPProjectionMutation, error) {
	return newMCPProjectionWriteMutation(mcpProjectionMutationUpsert, serverID, canonical)
}

// NewMCPProjectionRemoval creates a mutation that removes serverID when present.
func NewMCPProjectionRemoval(serverID string) (MCPProjectionMutation, error) {
	mutation := MCPProjectionMutation{
		kind:     mcpProjectionMutationRemove,
		serverID: serverID,
	}
	if err := mutation.validate(); err != nil {
		return MCPProjectionMutation{}, err
	}
	return mutation, nil
}

func newMCPProjectionWriteMutation(
	kind mcpProjectionMutationKind,
	serverID string,
	canonical []byte,
) (MCPProjectionMutation, error) {
	mutation := MCPProjectionMutation{
		kind:      kind,
		serverID:  serverID,
		canonical: bytes.Clone(canonical),
	}
	if err := mutation.validate(); err != nil {
		return MCPProjectionMutation{}, err
	}
	return mutation, nil
}

func (mutation MCPProjectionMutation) validate() error {
	if err := validateServerID(mutation.serverID); err != nil {
		return err
	}
	switch mutation.kind {
	case mcpProjectionMutationInsert, mcpProjectionMutationUpsert:
		if len(bytes.TrimSpace(mutation.canonical)) == 0 {
			return fmt.Errorf("MCP projection mutation %q requires canonical entry bytes", mutation.serverID)
		}
	case mcpProjectionMutationRemove:
		if len(mutation.canonical) != 0 {
			return fmt.Errorf("MCP projection removal %q cannot carry canonical entry bytes", mutation.serverID)
		}
	default:
		return fmt.Errorf("MCP projection mutation %q has unsupported kind %d", mutation.serverID, mutation.kind)
	}
	return nil
}

func validateMCPProjectionMutations(mutations []MCPProjectionMutation) error {
	if len(mutations) == 0 {
		return fmt.Errorf("MCP projection mutation batch is empty")
	}
	seen := make(map[string]struct{}, len(mutations))
	for index, mutation := range mutations {
		if err := mutation.validate(); err != nil {
			return fmt.Errorf("MCP projection mutation %d: %w", index, err)
		}
		if _, ok := seen[mutation.serverID]; ok {
			return fmt.Errorf("MCP projection mutation batch repeats server id %q", mutation.serverID)
		}
		seen[mutation.serverID] = struct{}{}
	}
	return nil
}

func mcpProjectionReplacementAuthorityError(label string, serverID string) error {
	return fmt.Errorf(
		"%s projection %q requires managed subject ownership evidence before replacing same-name entry",
		label,
		serverID,
	)
}
