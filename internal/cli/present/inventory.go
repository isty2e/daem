package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	statusworkflow "github.com/isty2e/daem/internal/workflow/status"
)

const listOutputsJSONSchemaVersion = 3

type inventoryJSONEntry struct {
	ResourceID string   `json:"resource_id,omitempty"`
	Subject    string   `json:"subject,omitempty"`
	Target     string   `json:"target,omitempty"`
	Targets    []string `json:"targets,omitempty"`
	Scope      string   `json:"scope"`
	Path       string   `json:"path"`
	Hash       string   `json:"hash"`
	Reason     string   `json:"reason,omitempty"`
	Detail     string   `json:"detail,omitempty"`
}

func PrintInventoryReportWithOptions(output io.Writer, inventory statusworkflow.Inventory, options HumanOptions) {
	fmt.Fprintln(output, "inventory:")
	fmt.Fprintf(output, "managed: %d\n", len(inventory.Managed))
	for _, entry := range inventory.Managed {
		printInventoryEntry(output, "managed", entry, options)
	}
	fmt.Fprintf(output, "unmanaged: %d\n", len(inventory.Unmanaged))
	for _, entry := range inventory.Unmanaged {
		printInventoryEntry(output, "unmanaged", entry, options)
	}
	fmt.Fprintf(output, "blocked: %d\n", len(inventory.Blocked))
	for _, entry := range inventory.Blocked {
		printInventoryEntry(output, "blocked", entry, options)
	}
}

func printInventoryEntry(output io.Writer, label string, entry statusworkflow.InventoryEntry, options HumanOptions) {
	resourceID := inventoryResourceID(entry)
	identityLabel, identity := "resource", resourceID
	targetValues := []string(nil)
	if entry.Target != "" {
		targetValues = []string{string(entry.Target)}
	}
	if !entry.Subject.IsZero() {
		targetValues = inventoryTargetStrings(entry)
		if identity == "" {
			identityLabel, identity = "subject", entry.Subject.String()
		}
	}
	targetKey := "targets"
	if len(targetValues) == 1 {
		targetKey = "target"
	}
	fmt.Fprintf(output, "%s %s=%q %s=%s scope=%s path=%q", label, identityLabel, identity, targetKey, strings.Join(targetValues, ","), entry.Scope, entry.Path)
	if options.Verbose && entry.Hash != "" {
		fmt.Fprintf(output, " hash=%q", entry.Hash)
	}
	if entry.Reason != "" {
		fmt.Fprintf(output, " reason=%q", entry.Reason)
	}
	if entry.Detail != "" {
		fmt.Fprintf(output, " detail=%q", entry.Detail)
	}
	fmt.Fprintln(output)
}

type listOutputsJSONOutput struct {
	SchemaVersion  int                  `json:"schema_version"`
	Command        string               `json:"command"`
	ManagedCount   int                  `json:"managed_count"`
	UnmanagedCount int                  `json:"unmanaged_count"`
	BlockedCount   int                  `json:"blocked_count"`
	Managed        []inventoryJSONEntry `json:"managed"`
	Unmanaged      []inventoryJSONEntry `json:"unmanaged"`
	Blocked        []inventoryJSONEntry `json:"blocked"`
}

func PrintListOutputsJSON(output io.Writer, inventory statusworkflow.Inventory) error {
	payload := listOutputsJSONOutput{
		SchemaVersion:  listOutputsJSONSchemaVersion,
		Command:        "list outputs",
		ManagedCount:   len(inventory.Managed),
		UnmanagedCount: len(inventory.Unmanaged),
		BlockedCount:   len(inventory.Blocked),
		Managed:        inventoryJSONEntries(inventory.Managed),
		Unmanaged:      inventoryJSONEntries(inventory.Unmanaged),
		Blocked:        inventoryJSONEntries(inventory.Blocked),
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func inventoryJSONEntries(entries []statusworkflow.InventoryEntry) []inventoryJSONEntry {
	result := make([]inventoryJSONEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, inventoryJSONEntry{
			ResourceID: inventoryResourceID(entry),
			Subject:    entry.Subject.String(),
			Target:     string(entry.Target),
			Targets:    inventoryTargetStrings(entry),
			Scope:      string(entry.Scope),
			Path:       entry.Path,
			Hash:       entry.Hash,
			Reason:     string(entry.Reason),
			Detail:     entry.Detail,
		})
	}
	return result
}

func inventoryResourceID(entry statusworkflow.InventoryEntry) string {
	if entry.EntityID == (entity.ID{}) {
		return ""
	}
	return string(entry.EntityID.Kind()) + "/" + entry.EntityID.Name()
}

func inventoryTargetStrings(entry statusworkflow.InventoryEntry) []string {
	result := make([]string, 0, len(entry.Targets))
	for _, selectedTarget := range entry.Targets {
		result = append(result, string(selectedTarget))
	}
	return result
}
