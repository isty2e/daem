package mcpcodec

import (
	"encoding/json"
	"fmt"
	"reflect"
)

func canonicalMCPJSONTypedEntryHostEncodedSize(
	entry any,
	serverID string,
	serversKey string,
) (int64, error) {
	return canonicalMCPJSONHostEncodedSize(nil, serversKey, nil, serverID, entry)
}

func canonicalMCPJSONConfigEncodedSize(
	top map[string]json.RawMessage,
	serversKey string,
	servers map[string]json.RawMessage,
) (int64, error) {
	return canonicalMCPJSONHostEncodedSize(top, serversKey, servers, "", nil)
}

func canonicalMCPJSONHostEncodedSize(
	top map[string]json.RawMessage,
	serversKey string,
	servers map[string]json.RawMessage,
	replacementServerID string,
	replacement any,
) (int64, error) {
	if (replacementServerID == "") != (replacement == nil) {
		return 0, fmt.Errorf("canonical MCP JSON replacement requires both server ID and entry")
	}
	if replacement != nil && !isCanonicalJSONServerEntry(replacement) {
		return 0, fmt.Errorf("unsupported canonical MCP JSON server entry type %T", replacement)
	}

	counter := boundedCanonicalJSONSize{}
	if err := counter.addMCPJSONHost(
		top,
		serversKey,
		servers,
		replacementServerID,
		replacement,
	); err != nil {
		return 0, err
	}
	if err := counter.addBytes(1); err != nil {
		return 0, err
	}
	return counter.bytes, nil
}

func (counter *boundedCanonicalJSONSize) addMCPJSONHost(
	top map[string]json.RawMessage,
	serversKey string,
	servers map[string]json.RawMessage,
	replacementServerID string,
	replacement any,
) error {
	if err := counter.addBytes(1); err != nil {
		return err
	}
	included := 0
	for key, raw := range top {
		if key == serversKey {
			continue
		}
		if err := counter.addObjectField(key, 0, included); err != nil {
			return err
		}
		if err := counter.addRawJSONValue(raw, 1); err != nil {
			return err
		}
		included++
	}
	if err := counter.addObjectField(serversKey, 0, included); err != nil {
		return err
	}
	if err := counter.addMCPJSONServers(
		servers,
		replacementServerID,
		replacement,
		1,
	); err != nil {
		return err
	}
	return counter.finishObject(0, included+1)
}

func (counter *boundedCanonicalJSONSize) addMCPJSONServers(
	servers map[string]json.RawMessage,
	replacementServerID string,
	replacement any,
	depth int,
) error {
	if err := counter.addBytes(1); err != nil {
		return err
	}
	included := 0
	replaced := false
	for serverID, raw := range servers {
		if err := counter.addObjectField(serverID, depth, included); err != nil {
			return err
		}
		if replacement != nil && serverID == replacementServerID {
			if err := counter.addValue(reflect.ValueOf(replacement), depth+1); err != nil {
				return err
			}
			replaced = true
		} else if err := counter.addRawJSONValue(raw, depth+1); err != nil {
			return err
		}
		included++
	}
	if replacement != nil && !replaced {
		if err := counter.addObjectField(replacementServerID, depth, included); err != nil {
			return err
		}
		if err := counter.addValue(reflect.ValueOf(replacement), depth+1); err != nil {
			return err
		}
		included++
	}
	return counter.finishObject(depth, included)
}
