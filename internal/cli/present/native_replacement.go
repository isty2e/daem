package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

type nativeReplacementKeyJSON struct {
	KeyPath        []string `json:"key_path"`
	File           string   `json:"file"`
	Action         string   `json:"action"`
	CurrentPresent bool     `json:"current_present"`
	Current        string   `json:"current,omitempty"`
	Desired        string   `json:"desired"`
}

// NativeReplacementJSON prepares the complete response before any mutation.
// Display masking is deliberately separate from the full-fidelity approval hash.
func NativeReplacementJSON(disclosure applyworkflow.NativeReplacementDisclosure, mode string, actionCount int) ([]byte, error) {
	name := disclosure.Declaration.Name()
	if err := checkAgentConfigIdentifier("declaration", name); err != nil {
		return nil, err
	}
	keys := make([]nativeReplacementKeyJSON, 0, len(disclosure.Keys))
	for i, row := range disclosure.Keys {
		if err := checkAgentConfigIdentifier(fmt.Sprintf("keys[%d].file", i), row.File); err != nil {
			return nil, err
		}
		segments, err := agentConfigKeySegments(fmt.Sprintf("keys[%d].key_path", i), row.Path)
		if err != nil {
			return nil, err
		}
		keys = append(keys, nativeReplacementKeyJSON{
			KeyPath: segments, File: row.File, Action: row.Action, CurrentPresent: row.CurrentPresent,
			Current: agentConfigValueSnippet(row.Path, row.Current), Desired: agentConfigValueSnippet(row.Path, row.Desired),
		})
	}
	return json.MarshalIndent(struct {
		SchemaVersion int                        `json:"schema_version"`
		Command       string                     `json:"command"`
		Mode          string                     `json:"mode"`
		Fingerprint   string                     `json:"fingerprint"`
		Declaration   string                     `json:"declaration"`
		ActionCount   int                        `json:"action_count"`
		Keys          []nativeReplacementKeyJSON `json:"keys"`
	}{
		SchemaVersion: contractversion.AgentConfigReplacementJSON, Command: "replace " + AgentConfigResourceName, Mode: mode,
		Fingerprint: disclosure.Fingerprint, Declaration: name, ActionCount: actionCount, Keys: keys,
	}, "", "  ")
}

func PrintNativeReplacement(output io.Writer, disclosure applyworkflow.NativeReplacementDisclosure) error {
	if _, err := fmt.Fprintf(output, "Replace %s\n", humanErrorDetail(disclosure.Declaration.String())); err != nil {
		return err
	}
	for _, row := range disclosure.Keys {
		current := "<absent>"
		if row.CurrentPresent {
			current = humanAgentConfigValue(row.Path, row.Current)
		}
		if _, err := fmt.Fprintf(output, "  %s: %s\n    file: %s\n    current: %s\n    declared: %s\n", row.Action, nativeKeyDisplay(row.Path), humanErrorDetail(row.File), current, humanAgentConfigValue(row.Path, row.Desired)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output, "fingerprint: %s\n", disclosure.Fingerprint)
	return err
}

func PrintNativeReplacementDiff(output io.Writer, disclosure applyworkflow.NativeReplacementDisclosure) error {
	for _, row := range disclosure.Keys {
		if row.CurrentPresent && row.Current == row.Desired {
			continue
		}
		current := "<absent>"
		if row.CurrentPresent {
			current = humanAgentConfigValue(row.Path, row.Current)
		}
		if _, err := fmt.Fprintf(output, "@@ %s @@\n- %s\n+ %s\n", nativeKeyDisplay(row.Path), current, humanAgentConfigValue(row.Path, row.Desired)); err != nil {
			return err
		}
	}
	return nil
}
