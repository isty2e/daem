package mcpcodec

import "fmt"

type codexMCPEntryContract[T any] struct {
	decodeCanonical func([]byte, string) (T, error)
	decodeValue     func(any, string) (T, error)
	encode          func(T, string) ([]byte, error)
	entryMap        func(T) map[string]any
	equal           func(T, T) bool
}

var codexProjectMCPEntryContract = codexMCPEntryContract[CodexProjectMCPServerEntry]{
	decodeCanonical: decodeCodexProjectMCPServerEntry,
	decodeValue:     decodeCodexProjectMCPServerEntryValue,
	encode:          encodeCodexProjectMCPServerEntry,
	entryMap:        codexProjectMCPServerEntryMap,
	equal:           codexProjectMCPServerEntriesEqual,
}

var codexGlobalMCPEntryContract = codexMCPEntryContract[CodexGlobalMCPServerEntry]{
	decodeCanonical: decodeCodexGlobalMCPServerEntry,
	decodeValue:     decodeCodexGlobalMCPServerEntryValue,
	encode:          encodeCodexGlobalMCPServerEntry,
	entryMap:        codexGlobalMCPServerEntryMap,
	equal:           codexGlobalMCPServerEntriesEqual,
}

func observeCodexProjectMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeCodexMCPProjections(existing, serverIDs, codexProjectMCPEntryContract)
}

func observeCodexGlobalMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeCodexMCPProjections(existing, serverIDs, codexGlobalMCPEntryContract)
}

func observeCodexMCPProjections[T any](
	existing []byte,
	serverIDs []string,
	contract codexMCPEntryContract[T],
) (MCPProjectionObservation, error) {
	for _, serverID := range serverIDs {
		if err := validateCodexMCPServerID(serverID); err != nil {
			return MCPProjectionObservation{}, err
		}
	}
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return MCPProjectionObservation{}, err
	}
	canonical := make(map[string][]byte, len(serverIDs))
	for _, serverID := range serverIDs {
		value, present := config.servers[serverID]
		if !present {
			continue
		}
		entry, err := contract.decodeValue(value, serverID)
		if err != nil {
			return MCPProjectionObservation{}, err
		}
		content, err := contract.encode(entry, serverID)
		if err != nil {
			return MCPProjectionObservation{}, err
		}
		canonical[serverID] = content
	}
	_, parentPresent := config.top[codexProjectMCPManagedField]
	return newMCPProjectionObservation(parentPresent, serverIDs, canonical)
}

func foldCodexProjectMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldCodexMCPProjectionMutations(
		existing,
		mutations,
		"Codex project MCP",
		codexProjectMCPEntryContract,
	)
}

func foldCodexGlobalMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldCodexMCPProjectionMutations(
		existing,
		mutations,
		"Codex global MCP",
		codexGlobalMCPEntryContract,
	)
}

func foldCodexMCPProjectionMutations[T any](
	existing []byte,
	mutations []MCPProjectionMutation,
	label string,
	contract codexMCPEntryContract[T],
) ([]byte, error) {
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, err
	}
	if err := applyCodexMCPProjectionMutations(&config, mutations, label, contract); err != nil {
		return nil, err
	}
	return config.encode()
}

func restoreCodexMCPProjectionMutations[T any](
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
	label string,
	contract codexMCPEntryContract[T],
) ([]byte, bool, error) {
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, false, err
	}
	if err := applyCodexMCPProjectionMutations(&config, mutations, label, contract); err != nil {
		return nil, false, err
	}
	return config.encodePreservingMCPParent(parentExistedBefore)
}

func applyCodexMCPProjectionMutations[T any](
	config *codexProjectMCPConfig,
	mutations []MCPProjectionMutation,
	label string,
	contract codexMCPEntryContract[T],
) error {
	for _, mutation := range mutations {
		existingValue, exists := config.servers[mutation.serverID]
		if exists {
			if _, err := contract.decodeValue(existingValue, mutation.serverID); err != nil {
				return err
			}
		}

		switch mutation.kind {
		case mcpProjectionMutationRemove:
			delete(config.servers, mutation.serverID)
		case mcpProjectionMutationInsert, mcpProjectionMutationUpsert:
			desired, err := contract.decodeCanonical(mutation.canonical, mutation.serverID)
			if err != nil {
				return err
			}
			if exists && mutation.kind == mcpProjectionMutationInsert {
				return mcpProjectionReplacementAuthorityError(label, mutation.serverID)
			}
			config.servers[mutation.serverID] = contract.entryMap(desired)
		default:
			return fmt.Errorf("unsupported MCP projection mutation kind %d", mutation.kind)
		}
	}
	return nil
}

func verifyCodexProjectMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyCodexMCPProjectionMutations(
		content,
		mutations,
		"Codex project MCP",
		codexProjectMCPEntryContract,
	)
}

func verifyCodexGlobalMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyCodexMCPProjectionMutations(
		content,
		mutations,
		"Codex global MCP",
		codexGlobalMCPEntryContract,
	)
}

func verifyCodexMCPProjectionMutations[T any](
	content []byte,
	mutations []MCPProjectionMutation,
	label string,
	contract codexMCPEntryContract[T],
) error {
	config, err := decodeCodexProjectMCPConfig(content)
	if err != nil {
		return err
	}
	for _, mutation := range mutations {
		existingValue, present := config.servers[mutation.serverID]
		if mutation.kind == mcpProjectionMutationRemove {
			if present {
				return fmt.Errorf("%s projection %q postcondition failed: entry remains present", label, mutation.serverID)
			}
			continue
		}
		if !present {
			return fmt.Errorf("%s projection %q postcondition failed: entry is absent", label, mutation.serverID)
		}
		existingEntry, err := contract.decodeValue(existingValue, mutation.serverID)
		if err != nil {
			return err
		}
		desiredEntry, err := contract.decodeCanonical(mutation.canonical, mutation.serverID)
		if err != nil {
			return err
		}
		if !contract.equal(existingEntry, desiredEntry) {
			return fmt.Errorf("%s projection %q postcondition failed: entry differs", label, mutation.serverID)
		}
	}
	return nil
}
