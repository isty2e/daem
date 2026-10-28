package clipresent

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/reconcile"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// NativeReplacementGuidance names the selected conflicts and their explicit
// repair operation without granting replacement authority to ordinary apply.
func NativeReplacementGuidance(result reconcile.Result, manifestPath string) []Guidance {
	type conflicts struct {
		name, destination string
		keys              []output.KeyPath
	}
	var groups []conflicts
	byDeclaration := make(map[string]int)
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
		index, found := byDeclaration[id.String()]
		if !found {
			index = len(groups)
			byDeclaration[id.String()] = index
			groups = append(groups, conflicts{name: id.Name(), destination: aggregate.Destination().String()})
		}
		groups[index].keys = append(groups[index].keys, key)
	}

	var resultGuidance []Guidance
	for _, group := range groups {
		argv := []string{"daem", "replace", "host-config", group.name}
		detail := fmt.Sprintf("%s remain unchanged", CountLabel(len(group.keys), "blocked key", "blocked keys"))
		if len(group.keys) == 1 {
			argv = append(argv, "--key", group.keys[0].String())
			detail = fmt.Sprintf("key=%s remains unchanged", nativeKeyDisplay(group.keys[0]))
		}
		argv = append(argv, "--dry-run")
		if manifestPath != "" {
			argv = append(argv, "--manifest", manifestPath)
		}
		command, err := CommandGuidance(argv, "preview restoring declared values or taking over existing keys")
		if err != nil {
			continue
		}
		resultGuidance = append(
			resultGuidance,
			NoteGuidance(fmt.Sprintf("host-config/%s: file=%s %s", humanErrorDetail(group.name), Quote(group.destination), detail)),
			command,
			InstructionGuidance("to keep the current host value instead, align its source, run lock, and preview apply again"),
		)
	}
	return resultGuidance
}
