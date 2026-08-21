package mcpcodec

import (
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/tailscale/hujson"
)

const piMCPAdapterLifecycleLazy = "lazy"

var allowedPiMCPManagedFields = map[string]struct{}{
	"command":   {},
	"args":      {},
	"env":       {},
	"lifecycle": {},
	"disabled":  {},
}

type piMCPPlacementCodec struct {
	placementID aggregate.MCPPlacementID
	spec        mcpConfigSpec
}

func newPiMCPPlacementCodec(placementID aggregate.MCPPlacementID) piMCPPlacementCodec {
	return piMCPPlacementCodec{
		placementID: placementID,
		spec: mcpConfigSpecForPlacement(
			placementID,
			"pi-mcp-adapter MCP",
			mcpManagedServersField,
		).withDocumentAdmission(admitPiMCPDocument),
	}
}

func (codec piMCPPlacementCodec) operationsInput() mcpPlacementOperationsInput {
	return mcpPlacementOperationsInput{
		foldMutations:         codec.foldMutations,
		restoreMutations:      codec.restoreMutations,
		verifyMutations:       codec.verifyMutations,
		observeCanonical:      codec.observeCanonical,
		mergeCanonicalEntry:   codec.mergeCanonicalEntry,
		removeProjection:      codec.removeProjection,
		restoreRemove:         codec.restoreRemove,
		extractCanonicalEntry: codec.extractCanonicalEntry,
		compareCanonicalEntry: codec.compareCanonicalEntry,
		entryPresent:          codec.entryPresent,
		parentPresent:         codec.parentPresent,
	}
}

func (codec piMCPPlacementCodec) decodeEntry(
	raw json.RawMessage,
	serverID string,
) (PiMCPAdapterServerEntry, error) {
	return decodePiMCPAdapterServerEntry(raw, codec.placementID, serverID)
}

func (codec piMCPPlacementCodec) foldMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
) ([]byte, error) {
	return foldMCPJSONServerMutations(existing, mutations, codec.spec, codec.decodeEntry)
}

func (codec piMCPPlacementCodec) restoreMutations(
	existing []byte,
	mutations []MCPProjectionMutation,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreMCPJSONServerMutations(
		existing,
		mutations,
		parentExistedBefore,
		codec.spec,
		codec.decodeEntry,
	)
}

func (codec piMCPPlacementCodec) verifyMutations(
	content []byte,
	mutations []MCPProjectionMutation,
) error {
	return verifyMCPJSONServerMutations(
		content,
		mutations,
		codec.spec,
		codec.decodeEntry,
		piMCPAdapterServerEntriesEqual,
	)
}

func (codec piMCPPlacementCodec) observeCanonical(
	existing []byte,
	serverIDs []string,
) (MCPProjectionObservation, error) {
	return observeMCPJSONServerProjections(existing, serverIDs, codec.spec, codec.decodeEntry)
}

func (codec piMCPPlacementCodec) mergeCanonicalEntry(
	existing []byte,
	serverID string,
	canonical []byte,
) ([]byte, error) {
	return mergeMCPJSONServerCanonicalEntry(
		existing,
		serverID,
		canonical,
		codec.spec,
		codec.decodeEntry,
	)
}

func (codec piMCPPlacementCodec) removeProjection(
	existing []byte,
	serverID string,
) ([]byte, error) {
	return removeMCPJSONServerProjection(existing, serverID, codec.spec, codec.decodeEntry)
}

func (codec piMCPPlacementCodec) restoreRemove(
	existing []byte,
	serverID string,
	parentExistedBefore bool,
) ([]byte, bool, error) {
	return restoreRemoveMCPJSONServerProjection(
		existing,
		serverID,
		parentExistedBefore,
		codec.spec,
		codec.decodeEntry,
	)
}

func (codec piMCPPlacementCodec) extractCanonicalEntry(
	existing []byte,
	serverID string,
) ([]byte, bool, error) {
	return extractMCPJSONServerProjectionBytes(
		existing,
		serverID,
		codec.spec,
		codec.decodeEntry,
	)
}

func (codec piMCPPlacementCodec) compareCanonicalEntry(
	existing []byte,
	serverID string,
	canonical []byte,
) (MCPProjectionCanonicalComparison, error) {
	desired, err := decodeCanonicalMCPJSONServerEntry(
		canonical,
		serverID,
		codec.spec,
		codec.decodeEntry,
	)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	actual, present, err := extractMCPJSONServerProjection(
		existing,
		serverID,
		codec.spec,
		codec.decodeEntry,
	)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	comparison := MCPProjectionCanonicalComparison{
		ContentPath: piMCPContentPath(codec.placementID, serverID),
		Present:     present,
	}
	if present {
		comparison.Equivalent = piMCPAdapterServerEntriesEqual(actual, desired)
	}
	return comparison, nil
}

func (codec piMCPPlacementCodec) entryPresent(existing []byte, serverID string) (bool, error) {
	return mcpJSONServerEntryPresent(existing, serverID, codec.spec)
}

func (codec piMCPPlacementCodec) parentPresent(existing []byte) (bool, error) {
	return mcpJSONServersParentPresent(existing, codec.spec)
}

func admitPiMCPDocument(content []byte, spec mcpConfigSpec) error {
	strictErr := jsonstrict.Validate(content, spec.label+" config JSON", maximumMCPJSONDepth)
	if strictErr == nil {
		var top map[string]json.RawMessage
		if err := json.Unmarshal(content, &top); err == nil && top != nil {
			if _, aliasPresent := top["mcp-servers"]; aliasPresent {
				return newMCPProjectionError(
					MCPProjectionReasonUnsupportedManagedField,
					spec.configPath,
					`provider alias "mcp-servers" is not a losslessly managed top-level key`,
				)
			}
		}
		return nil
	}
	if !utf8.Valid(content) {
		return mcpJSONHostDocumentError(spec, strictErr)
	}
	if err := admitPiMCPJSONCNesting(content, maximumMCPJSONDepth); err != nil {
		return mcpJSONHostDocumentError(spec, err)
	}
	standardized, err := hujson.Standardize(content)
	if err != nil {
		return mcpJSONHostDocumentError(spec, strictErr)
	}
	if err := jsonstrict.Validate(
		standardized,
		spec.label+" standardized config JSON",
		maximumMCPJSONDepth,
	); err != nil {
		return mcpJSONHostDocumentError(spec, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(standardized, &top); err != nil || top == nil {
		return newMCPProjectionError(
			MCPProjectionReasonConfigMalformed,
			spec.configPath,
			spec.label+" standardized config JSON must be an object",
		)
	}
	return newMCPProjectionError(
		MCPProjectionReasonProviderDocumentLossy,
		spec.configPath,
		"provider-valid JSONC cannot be mutated without losing comments or trailing commas",
	)
}

func admitPiMCPJSONCNesting(content []byte, maximumDepth int) error {
	const (
		piJSONNormal uint8 = iota
		piJSONString
		piJSONLineComment
		piJSONBlockComment
	)
	state := piJSONNormal
	depth := 0
	for index := 0; index < len(content); index++ {
		character := content[index]
		switch state {
		case piJSONString:
			switch character {
			case '\\':
				index++
			case '"':
				state = piJSONNormal
			}
		case piJSONLineComment:
			if character == '\n' {
				state = piJSONNormal
			}
		case piJSONBlockComment:
			if character == '*' && index+1 < len(content) && content[index+1] == '/' {
				state = piJSONNormal
				index++
			}
		default:
			switch character {
			case '"':
				state = piJSONString
			case '/':
				if index+1 >= len(content) {
					continue
				}
				switch content[index+1] {
				case '/':
					state = piJSONLineComment
					index++
				case '*':
					state = piJSONBlockComment
					index++
				}
			case '{', '[':
				depth++
				if depth > maximumDepth {
					return fmt.Errorf(
						"%w: maximum=%d",
						jsonstrict.ErrMaximumDepthExceeded,
						maximumDepth,
					)
				}
			case '}', ']':
				if depth > 0 {
					depth--
				}
			}
		}
	}
	return nil
}

func decodePiMCPAdapterServerEntry(
	raw json.RawMessage,
	placementID aggregate.MCPPlacementID,
	serverID string,
) (PiMCPAdapterServerEntry, error) {
	subject := piMCPContentPath(placementID, serverID)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return PiMCPAdapterServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			subject,
			"managed MCP server entry is not a JSON object",
		)
	}

	fieldNames := make([]string, 0, len(fields))
	for fieldName := range fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		if _, ok := allowedPiMCPManagedFields[fieldName]; !ok {
			return PiMCPAdapterServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonUnsupportedManagedField,
				subject,
				fmt.Sprintf("unsupported managed MCP server field %q", fieldName),
			)
		}
	}

	entry := PiMCPAdapterServerEntry{
		Args:      []string{},
		Env:       map[string]string{},
		Lifecycle: piMCPAdapterLifecycleLazy,
	}
	if err := decodeRequiredString(fields, "command", subject, &entry.Command); err != nil {
		return PiMCPAdapterServerEntry{}, err
	}
	if err := validateMCPCommand(entry.Command); err != nil {
		return PiMCPAdapterServerEntry{}, err
	}
	if rawArgs, present := fields["args"]; present {
		if err := json.Unmarshal(rawArgs, &entry.Args); err != nil || entry.Args == nil {
			return PiMCPAdapterServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"args must be a JSON string array",
			)
		}
	}
	if rawEnv, present := fields["env"]; present {
		if err := json.Unmarshal(rawEnv, &entry.Env); err != nil || entry.Env == nil {
			return PiMCPAdapterServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"env must be a JSON string map",
			)
		}
		env, err := canonicalMCPEnv(entry.Env)
		if err != nil {
			return PiMCPAdapterServerEntry{}, err
		}
		entry.Env = env
	}
	if rawLifecycle, present := fields["lifecycle"]; present {
		if err := json.Unmarshal(rawLifecycle, &entry.Lifecycle); err != nil {
			return PiMCPAdapterServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"lifecycle must be a string",
			)
		}
	}
	if entry.Lifecycle != piMCPAdapterLifecycleLazy {
		return PiMCPAdapterServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonUnsupportedManagedField,
			subject+"/lifecycle",
			`only lifecycle "lazy" is admitted`,
		)
	}
	if rawDisabled, present := fields["disabled"]; present {
		if err := json.Unmarshal(rawDisabled, &entry.Disabled); err != nil {
			return PiMCPAdapterServerEntry{}, newMCPProjectionError(
				MCPProjectionReasonProjectionEquivalenceUndefined,
				subject,
				"disabled must be a boolean",
			)
		}
	}
	if entry.Disabled {
		return PiMCPAdapterServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonUnsupportedManagedField,
			subject+"/disabled",
			"disabled managed servers are outside the admitted profile",
		)
	}
	return entry, nil
}

func canonicalPiMCPAdapterServerEntry(
	projection PiMCPAdapterServerProjection,
) (PiMCPAdapterServerEntry, error) {
	if projection.AdapterContract != aggregate.PiMCPAdapterStdioV1 {
		return PiMCPAdapterServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported pi-mcp-adapter MCP contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return PiMCPAdapterServerEntry{}, err
	}
	if err := validateMCPCommand(projection.Command); err != nil {
		return PiMCPAdapterServerEntry{}, err
	}
	env, err := canonicalMCPEnv(projection.Env)
	if err != nil {
		return PiMCPAdapterServerEntry{}, err
	}
	return PiMCPAdapterServerEntry{
		Command:   projection.Command,
		Args:      append([]string{}, projection.Args...),
		Env:       env,
		Lifecycle: piMCPAdapterLifecycleLazy,
		Disabled:  false,
	}, nil
}

// CanonicalPiMCPAdapterServerEntry returns the canonical managed adapter entry bytes.
func CanonicalPiMCPAdapterServerEntry(projection PiMCPAdapterServerProjection) ([]byte, error) {
	entry, err := canonicalPiMCPAdapterServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return encodeMCPJSONServerEntry(entry, projection.ServerID, newPiMCPPlacementCodec(aggregate.MCPPlacementPiProject).spec)
}

func piMCPAdapterServerEntriesEqual(
	left PiMCPAdapterServerEntry,
	right PiMCPAdapterServerEntry,
) bool {
	return left.Lifecycle == right.Lifecycle &&
		left.Disabled == right.Disabled &&
		claudeMCPServerFieldsEqual(
			"",
			left.Command,
			left.Args,
			left.Env,
			"",
			right.Command,
			right.Args,
			right.Env,
		)
}

func piMCPContentPath(placementID aggregate.MCPPlacementID, serverID string) string {
	switch placementID {
	case aggregate.MCPPlacementPiProject:
		return PiProjectMCPContentPath(serverID)
	case aggregate.MCPPlacementPiGlobal:
		return PiGlobalMCPContentPath(serverID)
	default:
		return string(placementID) + "/" + serverID
	}
}
