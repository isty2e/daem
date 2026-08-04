package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/desired/entity"
	listworkflow "github.com/isty2e/daem/internal/workflow/list"
)

type inventoryJSONEntry struct {
	ResourceID  string   `json:"resource_id,omitempty"`
	Subject     string   `json:"subject,omitempty"`
	Targets     []string `json:"targets,omitempty"`
	Scope       string   `json:"scope"`
	Path        string   `json:"path"`
	ContentPath string   `json:"content_path,omitempty"`
	Hash        string   `json:"hash"`
	Reason      string   `json:"reason,omitempty"`
	Detail      string   `json:"detail,omitempty"`
}

func PrintInventoryReportWithOptions(output io.Writer, inventory listworkflow.OutputInventory, options HumanOptions) {
	fmt.Fprintln(output, "inventory:")
	managed := inventory.Managed()
	fmt.Fprintf(output, "managed: %d\n", len(managed))
	for _, entry := range managed {
		printInventoryEntry(output, "managed", entry, options)
	}
	unmanaged := inventory.Unmanaged()
	fmt.Fprintf(output, "unmanaged: %d\n", len(unmanaged))
	for _, entry := range unmanaged {
		printInventoryEntry(output, "unmanaged", entry, options)
	}
	blocked := inventory.Blocked()
	fmt.Fprintf(output, "blocked: %d\n", len(blocked))
	for _, entry := range blocked {
		printInventoryEntry(output, "blocked", entry, options)
	}
}

func printInventoryEntry(output io.Writer, label string, entry listworkflow.OutputInventoryEntry, options HumanOptions) {
	resourceID := inventoryResourceID(entry)
	identityLabel, identity := "resource", resourceID
	targetValues := inventoryTargetStrings(entry)
	if !entry.Subject().IsZero() {
		if identity == "" {
			identityLabel, identity = "subject", entry.Subject().String()
		}
	}
	targetKey := "targets"
	if len(targetValues) == 1 {
		targetKey = "target"
	}
	fmt.Fprintf(output, "%s %s=%q %s=%s scope=%s path=%q", label, identityLabel, identity, targetKey, strings.Join(targetValues, ","), entry.Scope(), entry.Path())
	if entry.ContentPath() != "" {
		fmt.Fprintf(output, " content_path=%q", entry.ContentPath())
	}
	if options.Verbose && entry.Hash() != "" {
		fmt.Fprintf(output, " hash=%q", entry.Hash())
	}
	if entry.Reason() != "" {
		fmt.Fprintf(output, " reason=%q", entry.Reason())
	}
	if entry.Detail() != "" {
		fmt.Fprintf(output, " detail=%q", entry.Detail())
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

func PrintListOutputsJSON(output io.Writer, inventory listworkflow.OutputInventory) error {
	managed := inventory.Managed()
	unmanaged := inventory.Unmanaged()
	blocked := inventory.Blocked()
	payload := listOutputsJSONOutput{
		SchemaVersion:  contractversion.OutputInventoryJSON,
		Command:        "list outputs",
		ManagedCount:   len(managed),
		UnmanagedCount: len(unmanaged),
		BlockedCount:   len(blocked),
		Managed:        inventoryJSONEntries(managed),
		Unmanaged:      inventoryJSONEntries(unmanaged),
		Blocked:        inventoryJSONEntries(blocked),
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func inventoryJSONEntries(entries []listworkflow.OutputInventoryEntry) []inventoryJSONEntry {
	result := make([]inventoryJSONEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, inventoryJSONEntry{
			ResourceID:  inventoryResourceID(entry),
			Subject:     entry.Subject().String(),
			Targets:     inventoryTargetStrings(entry),
			Scope:       string(entry.Scope()),
			Path:        entry.Path(),
			ContentPath: string(entry.ContentPath()),
			Hash:        entry.Hash(),
			Reason:      string(entry.Reason()),
			Detail:      entry.Detail(),
		})
	}
	return result
}

func inventoryResourceID(entry listworkflow.OutputInventoryEntry) string {
	if entry.EntityID() == (entity.ID{}) {
		return ""
	}
	return string(entry.EntityID().Kind()) + "/" + entry.EntityID().Name()
}

func inventoryTargetStrings(entry listworkflow.OutputInventoryEntry) []string {
	targets := entry.Targets()
	result := make([]string, 0, len(targets))
	for _, selectedTarget := range targets {
		result = append(result, string(selectedTarget))
	}
	return result
}
