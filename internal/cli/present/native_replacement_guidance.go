package clipresent

import (
	"fmt"

	"github.com/isty2e/daem/internal/reconcile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// NativeReplacementGuidance names the selected conflict and its explicit repair
// operation without granting replacement authority to ordinary apply.
func NativeReplacementGuidance(result reconcile.Result, manifestPath string) []Guidance {
	var resultGuidance []Guidance
	for _, decision := range result.Decisions() {
		aggregate, present := decision.Aggregate()
		if !present || !aggregate.IsBlocked() {
			continue
		}
		id, key, native := topologyprojection.HostConfigKeyIdentity(aggregate.Subject())
		if !native {
			continue
		}
		switch aggregate.Reason() {
		case reconcile.ReasonDriftedOutput, reconcile.ReasonUnmanagedOutputExists, reconcile.ReasonMissingOutput:
		default:
			continue
		}
		argv := []string{"daem", "replace", "host-config", id.Name(), "--key", key.String(), "--dry-run"}
		if manifestPath != "" {
			argv = append(argv, "--manifest", manifestPath)
		}
		command, err := CommandGuidance(argv, "preview setting this key to its declared value")
		if err != nil {
			continue
		}
		resultGuidance = append(
			resultGuidance,
			NoteGuidance(fmt.Sprintf("host-config/%s: file=%s key=%s remains unchanged", humanErrorDetail(id.Name()), Quote(aggregate.Destination().String()), nativeKeyDisplay(key))),
			command,
			InstructionGuidance("to keep the current host value instead, align its source, run lock, and preview apply again"),
		)
	}
	return resultGuidance
}
