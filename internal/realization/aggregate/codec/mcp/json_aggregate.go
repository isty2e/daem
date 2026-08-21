package mcpcodec

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

func canonicalMCPJSONError(subject string, operation string, err error) error {
	return newMCPProjectionError(
		MCPProjectionReasonCanonicalInvalid,
		subject,
		fmt.Sprintf("%s: %v", operation, err),
	)
}

func canonicalJSON(value any) ([]byte, error) {
	expectedBytes, err := canonicalJSONEncodedSize(value)
	if err != nil {
		return nil, canonicalMCPJSONError("", "encode canonical MCP JSON", err)
	}
	return marshalPreflightedCanonicalJSON(value, expectedBytes)
}

func marshalPreflightedCanonicalJSON(value any, expectedBytes int64) ([]byte, error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, canonicalMCPJSONError("", "encode canonical MCP JSON", err)
	}
	content = append(content, '\n')
	if err := validateMCPDocumentSize(content); err != nil {
		return nil, canonicalMCPJSONError("", "encode canonical MCP JSON", err)
	}
	if int64(len(content)) != expectedBytes {
		return nil, canonicalMCPJSONError(
			"",
			"encode canonical MCP JSON",
			fmt.Errorf("size preflight measured %d bytes, encoder produced %d", expectedBytes, len(content)),
		)
	}
	if err := jsonstrict.Validate(content, "canonical MCP JSON", maximumMCPJSONDepth); err != nil {
		return nil, canonicalMCPJSONError("", "validate canonical MCP JSON", err)
	}
	return content, nil
}

type mcpJSONServerEntryDecoder[E any] func(json.RawMessage, string) (E, error)

type mcpJSONServerEntryEqual[E any] func(E, E) bool

func encodeMCPJSONServerEntry[E any](
	entry E,
	serverID string,
	spec mcpConfigSpec,
) ([]byte, error) {
	if err := validateServerID(serverID); err != nil {
		return nil, err
	}
	entryBytes, err := canonicalJSONEncodedSize(entry)
	if err != nil {
		return nil, canonicalMCPJSONError("", "encode canonical MCP JSON", err)
	}
	hostBytes, err := canonicalMCPJSONTypedEntryHostEncodedSize(
		entry,
		serverID,
		spec.serversKey,
	)
	if err != nil {
		return nil, canonicalMCPJSONError(
			spec.serversPath+"/"+serverID,
			"render canonical MCP JSON entry",
			canonicalMCPJSONError("", "encode canonical MCP JSON", err),
		)
	}
	canonical, err := marshalPreflightedCanonicalJSON(entry, entryBytes)
	if err != nil {
		return nil, err
	}
	encodedHostBytes, err := canonicalMCPJSONConfigEncodedSize(
		nil,
		spec.serversKey,
		map[string]json.RawMessage{serverID: canonical},
	)
	if err != nil {
		return nil, canonicalMCPJSONError(
			spec.serversPath+"/"+serverID,
			"render canonical MCP JSON entry",
			canonicalMCPJSONError("", "encode canonical MCP JSON", err),
		)
	}
	if encodedHostBytes != hostBytes {
		return nil, canonicalMCPJSONError(
			spec.serversPath+"/"+serverID,
			"render canonical MCP JSON entry",
			fmt.Errorf(
				"typed host preflight measured %d bytes, canonical entry measured %d",
				hostBytes,
				encodedHostBytes,
			),
		)
	}
	return canonical, nil
}

func decodeCanonicalMCPJSONServerEntry[E any](
	canonical []byte,
	serverID string,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) (E, error) {
	var zero E
	subject := spec.serversPath + "/" + serverID
	if err := validateMCPDocumentSize(canonical); err != nil {
		return zero, canonicalMCPJSONError(subject, "admit canonical MCP JSON entry", err)
	}
	if err := jsonstrict.Validate(
		canonical,
		"canonical MCP JSON entry",
		maximumMCPJSONDepth,
	); err != nil {
		return zero, canonicalMCPJSONError(subject, "admit canonical MCP JSON entry", err)
	}
	entry, err := decodeEntry(json.RawMessage(canonical), serverID)
	if err != nil {
		return zero, canonicalMCPJSONError(subject, "validate canonical MCP JSON entry", err)
	}
	expected, err := encodeMCPJSONServerEntry(entry, serverID, spec)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(canonical, expected) {
		return zero, newMCPProjectionError(
			MCPProjectionReasonCanonicalInvalid,
			subject,
			"canonical MCP JSON entry bytes do not match the codec encoding",
		)
	}
	return entry, nil
}

func observeMCPJSONServerProjections[E any](
	existing []byte,
	serverIDs []string,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) (MCPProjectionObservation, error) {
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return MCPProjectionObservation{}, err
	}
	canonical := make(map[string][]byte, len(serverIDs))
	for _, serverID := range serverIDs {
		raw, present := config.servers[serverID]
		if !present {
			continue
		}
		entry, err := decodeEntry(raw, serverID)
		if err != nil {
			return MCPProjectionObservation{}, err
		}
		content, err := encodeMCPJSONServerEntry(entry, serverID, spec)
		if err != nil {
			return MCPProjectionObservation{}, err
		}
		canonical[serverID] = content
	}
	_, parentPresent := config.top[spec.serversKey]
	return newMCPProjectionObservation(parentPresent, serverIDs, canonical)
}

func foldMCPJSONServerMutations[E any](
	existing []byte,
	mutations []MCPProjectionMutation,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) ([]byte, error) {
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return nil, err
	}
	if err := applyMCPJSONServerMutations(&config, mutations, spec, decodeEntry); err != nil {
		return nil, err
	}
	return config.encode()
}

func restoreMCPJSONServerMutations[E any](
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) ([]byte, bool, error) {
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return nil, false, err
	}
	if err := applyMCPJSONServerMutations(&config, mutations, spec, decodeEntry); err != nil {
		return nil, false, err
	}
	return config.encodePreservingMCPParent(parentExistedBefore)
}

func applyMCPJSONServerMutations[E any](
	config *mcpConfig,
	mutations []MCPProjectionMutation,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) error {
	for _, mutation := range mutations {
		existingRaw, exists := config.servers[mutation.serverID]
		if exists {
			if _, err := decodeEntry(existingRaw, mutation.serverID); err != nil {
				return err
			}
		}

		switch mutation.kind {
		case mcpProjectionMutationRemove:
			delete(config.servers, mutation.serverID)
		case mcpProjectionMutationInsert, mcpProjectionMutationUpsert:
			if _, err := decodeCanonicalMCPJSONServerEntry(
				mutation.canonical,
				mutation.serverID,
				spec,
				decodeEntry,
			); err != nil {
				return err
			}
			if exists && mutation.kind == mcpProjectionMutationInsert {
				return mcpProjectionReplacementAuthorityError(spec.label, mutation.serverID)
			}
			config.servers[mutation.serverID] = json.RawMessage(bytes.Clone(mutation.canonical))
		default:
			return fmt.Errorf("unsupported MCP projection mutation kind %d", mutation.kind)
		}
	}
	return nil
}

func verifyMCPJSONServerMutations[E any](
	content []byte,
	mutations []MCPProjectionMutation,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
	equal mcpJSONServerEntryEqual[E],
) error {
	config, err := decodeMCPConfig(content, spec)
	if err != nil {
		return err
	}
	for _, mutation := range mutations {
		existingRaw, present := config.servers[mutation.serverID]
		if mutation.kind == mcpProjectionMutationRemove {
			if present {
				return fmt.Errorf("%s projection %q postcondition failed: entry remains present", spec.label, mutation.serverID)
			}
			continue
		}
		if !present {
			return fmt.Errorf("%s projection %q postcondition failed: entry is absent", spec.label, mutation.serverID)
		}
		existingEntry, err := decodeEntry(existingRaw, mutation.serverID)
		if err != nil {
			return err
		}
		desiredEntry, err := decodeCanonicalMCPJSONServerEntry(
			mutation.canonical,
			mutation.serverID,
			spec,
			decodeEntry,
		)
		if err != nil {
			return err
		}
		if !equal(existingEntry, desiredEntry) {
			return fmt.Errorf("%s projection %q postcondition failed: entry differs", spec.label, mutation.serverID)
		}
	}
	return nil
}

func mergeMCPJSONServerCanonicalEntry[E any](
	existing []byte,
	serverID string,
	canonical []byte,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) ([]byte, error) {
	if err := validateServerID(serverID); err != nil {
		return nil, err
	}
	desired, err := decodeCanonicalMCPJSONServerEntry(canonical, serverID, spec, decodeEntry)
	if err != nil {
		return nil, err
	}
	return mergeMCPJSONServerEntry(existing, serverID, desired, spec, decodeEntry)
}

func mergeMCPJSONServerEntry[E any](
	existing []byte,
	serverID string,
	desired E,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) ([]byte, error) {
	if err := validateServerID(serverID); err != nil {
		return nil, err
	}
	desiredRaw, err := encodeMCPJSONServerEntry(desired, serverID, spec)
	if err != nil {
		return nil, err
	}

	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return nil, err
	}
	if existingRaw, exists := config.servers[serverID]; exists {
		if _, err := decodeEntry(existingRaw, serverID); err != nil {
			return nil, err
		}
	}
	config.servers[serverID] = json.RawMessage(desiredRaw)
	return config.encode()
}

func removeMCPJSONServerProjection[E any](
	existing []byte,
	serverID string,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) ([]byte, error) {
	if err := validateServerID(serverID); err != nil {
		return nil, err
	}
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return nil, err
	}
	if existingRaw, exists := config.servers[serverID]; exists {
		if _, err := decodeEntry(existingRaw, serverID); err != nil {
			return nil, err
		}
		delete(config.servers, serverID)
	}
	return config.encode()
}

func restoreRemoveMCPJSONServerProjection[E any](
	existing []byte,
	serverID string,
	parentExistedBefore bool,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) ([]byte, bool, error) {
	if err := validateServerID(serverID); err != nil {
		return nil, false, err
	}
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return nil, false, err
	}
	if existingRaw, exists := config.servers[serverID]; exists {
		if _, err := decodeEntry(existingRaw, serverID); err != nil {
			return nil, false, err
		}
		delete(config.servers, serverID)
	}
	content, keepFile, err := config.encodePreservingMCPParent(parentExistedBefore)
	if err != nil {
		return nil, false, err
	}
	return content, keepFile, nil
}

func extractMCPJSONServerProjection[E any](
	existing []byte,
	serverID string,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) (E, bool, error) {
	var zero E
	if err := validateServerID(serverID); err != nil {
		return zero, false, err
	}
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return zero, false, err
	}
	raw, ok := config.servers[serverID]
	if !ok {
		return zero, false, nil
	}
	entry, err := decodeEntry(raw, serverID)
	if err != nil {
		return zero, false, err
	}
	return entry, true, nil
}

func extractMCPJSONServerProjectionBytes[E any](
	existing []byte,
	serverID string,
	spec mcpConfigSpec,
	decodeEntry mcpJSONServerEntryDecoder[E],
) ([]byte, bool, error) {
	entry, present, err := extractMCPJSONServerProjection(existing, serverID, spec, decodeEntry)
	if err != nil || !present {
		return nil, present, err
	}
	content, err := encodeMCPJSONServerEntry(entry, serverID, spec)
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func mcpJSONServerEntryPresent(existing []byte, serverID string, spec mcpConfigSpec) (bool, error) {
	if err := validateServerID(serverID); err != nil {
		return false, err
	}
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return false, err
	}
	_, ok := config.servers[serverID]
	return ok, nil
}

func mcpJSONServersParentPresent(existing []byte, spec mcpConfigSpec) (bool, error) {
	config, err := decodeMCPConfig(existing, spec)
	if err != nil {
		return false, err
	}
	_, ok := config.top[spec.serversKey]
	return ok, nil
}
