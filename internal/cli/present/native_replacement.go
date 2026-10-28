package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/subprocess"

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
	keys := make([]nativeReplacementKeyJSON, 0, len(disclosure.Keys))
	for _, row := range disclosure.Keys {
		keys = append(keys, nativeReplacementKeyJSON{row.Path.Segments(), humanErrorDetail(row.File), row.Action, row.CurrentPresent, nativeReplacementValueDisplay(row.Path, row.Current), nativeReplacementValueDisplay(row.Path, row.Desired)})
	}
	return json.MarshalIndent(struct {
		SchemaVersion int                        `json:"schema_version"`
		Command       string                     `json:"command"`
		Mode          string                     `json:"mode"`
		Fingerprint   string                     `json:"fingerprint"`
		Declaration   string                     `json:"declaration"`
		ActionCount   int                        `json:"action_count"`
		Keys          []nativeReplacementKeyJSON `json:"keys"`
	}{1, "replace host-config", mode, disclosure.Fingerprint, humanErrorDetail(disclosure.Declaration), actionCount, keys}, "", "  ")
}

func PrintNativeReplacement(output io.Writer, disclosure applyworkflow.NativeReplacementDisclosure) error {
	if _, err := fmt.Fprintf(output, "Replace %s\n", humanErrorDetail(disclosure.Declaration)); err != nil {
		return err
	}
	for _, row := range disclosure.Keys {
		current := "<absent>"
		if row.CurrentPresent {
			current = nativeReplacementValueDisplay(row.Path, row.Current)
		}
		if _, err := fmt.Fprintf(output, "  %s: %s\n    file: %s\n    current: %s\n    declared: %s\n", row.Action, nativeKeyDisplay(row.Path), humanErrorDetail(row.File), current, nativeReplacementValueDisplay(row.Path, row.Desired)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output, "fingerprint: %s\n", disclosure.Fingerprint)
	return err
}

func nativeReplacementValueDisplay(key output.KeyPath, value string) string {
	if len(value) > maximumHumanErrorDetailRunes {
		return "[REDACTED: display limit]"
	}
	for _, segment := range key.Segments() {
		encoded, err := json.Marshal(map[string]string{segment: value})
		if err != nil {
			return "[REDACTED]"
		}
		result := subprocess.NewCapturePolicy(nil, maximumHumanErrorDetailRunes).Sanitize(string(encoded), false)
		if result.Redacted() || result.Truncated() {
			return "[REDACTED]"
		}
	}
	return humanErrorDetail(value)
}

func PrintNativeReplacementDiff(output io.Writer, disclosure applyworkflow.NativeReplacementDisclosure) error {
	for _, row := range disclosure.Keys {
		if row.CurrentPresent && row.Current == row.Desired {
			continue
		}
		current := "<absent>"
		if row.CurrentPresent {
			current = nativeReplacementValueDisplay(row.Path, row.Current)
		}
		if _, err := fmt.Fprintf(output, "@@ %s @@\n- %s\n+ %s\n", nativeKeyDisplay(row.Path), current, nativeReplacementValueDisplay(row.Path, row.Desired)); err != nil {
			return err
		}
	}
	return nil
}
