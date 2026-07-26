package mcpcodec

import "fmt"

func observeCodexProjectMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeCodexMCPProjections(existing, serverIDs)
}

func observeCodexGlobalMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
	return observeCodexMCPProjections(existing, serverIDs)
}

func observeCodexMCPProjections(existing []byte, serverIDs []string) (MCPProjectionObservation, error) {
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
		entry, err := decodeCodexProjectMCPServerEntryValue(value, serverID)
		if err != nil {
			return MCPProjectionObservation{}, err
		}
		content, err := encodeCodexProjectMCPServerEntry(entry)
		if err != nil {
			return MCPProjectionObservation{}, err
		}
		canonical[serverID] = content
	}
	_, parentPresent := config.top[codexProjectMCPManagedField]
	return newMCPProjectionObservation(parentPresent, serverIDs, canonical)
}

func foldCodexProjectMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldCodexMCPProjectionMutations(existing, mutations, "Codex project MCP")
}

func foldCodexGlobalMCPProjectionMutations(existing []byte, mutations []MCPProjectionMutation) ([]byte, error) {
	return foldCodexMCPProjectionMutations(existing, mutations, "Codex global MCP")
}

func foldCodexMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	label string,
) ([]byte, error) {
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, err
	}
	if err := applyCodexMCPProjectionMutations(&config, mutations, label); err != nil {
		return nil, err
	}
	return config.encode()
}

func restoreCodexMCPProjectionMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
	label string,
) ([]byte, bool, error) {
	config, err := decodeCodexProjectMCPConfig(existing)
	if err != nil {
		return nil, false, err
	}
	if err := applyCodexMCPProjectionMutations(&config, mutations, label); err != nil {
		return nil, false, err
	}
	return config.encodePreservingMCPParent(parentExistedBefore)
}

func applyCodexMCPProjectionMutations(
	config *codexProjectMCPConfig,
	mutations []MCPProjectionMutation,
	label string,
) error {
	for _, mutation := range mutations {
		existingValue, exists := config.servers[mutation.serverID]
		if exists {
			if _, err := decodeCodexProjectMCPServerEntryValue(existingValue, mutation.serverID); err != nil {
				return err
			}
		}

		switch mutation.kind {
		case mcpProjectionMutationRemove:
			delete(config.servers, mutation.serverID)
		case mcpProjectionMutationInsert, mcpProjectionMutationUpsert:
			desired, err := decodeCodexProjectMCPServerEntry(mutation.canonical, mutation.serverID)
			if err != nil {
				return err
			}
			if exists && mutation.kind == mcpProjectionMutationInsert {
				return mcpProjectionReplacementAuthorityError(label, mutation.serverID)
			}
			config.servers[mutation.serverID] = codexProjectMCPServerEntryMap(desired)
		default:
			return fmt.Errorf("unsupported MCP projection mutation kind %d", mutation.kind)
		}
	}
	return nil
}

func verifyCodexProjectMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyCodexMCPProjectionMutations(content, mutations, "Codex project MCP")
}

func verifyCodexGlobalMCPProjectionMutations(content []byte, mutations []MCPProjectionMutation) error {
	return verifyCodexMCPProjectionMutations(content, mutations, "Codex global MCP")
}

func verifyCodexMCPProjectionMutations(
	content []byte,
	mutations []MCPProjectionMutation,
	label string,
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
		existingEntry, err := decodeCodexProjectMCPServerEntryValue(existingValue, mutation.serverID)
		if err != nil {
			return err
		}
		desiredEntry, err := decodeCodexProjectMCPServerEntry(mutation.canonical, mutation.serverID)
		if err != nil {
			return err
		}
		if !codexProjectMCPServerEntriesEqual(existingEntry, desiredEntry) {
			return fmt.Errorf("%s projection %q postcondition failed: entry differs", label, mutation.serverID)
		}
	}
	return nil
}
