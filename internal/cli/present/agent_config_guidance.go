package clipresent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type agentConfigKeyConflicts struct {
	identity nativeHumanGroupKey
	replace  []output.KeyPath
	retire   []output.KeyPath
}

// AgentConfigReconciliationGuidance projects advice from the plan and its
// unfiltered declaration membership. Nil membership is unknown, not absence.
func AgentConfigReconciliationGuidance(result reconcile.Result, manifestPath string, declarationIDs []entity.ID) []Guidance {
	if declarationIDs == nil {
		return nil
	}
	declared := make(map[entity.ID]struct{}, len(declarationIDs))
	for _, id := range declarationIDs {
		declared[id] = struct{}{}
	}

	var guidance []Guidance
	for _, group := range agentConfigKeyConflictGroups(result) {
		_, present := declared[group.identity.declaration]
		if present && len(group.replace) != 0 {
			guidance = append(guidance, agentConfigKeyReplacementGuidance(group, manifestPath)...)
		}
		if len(group.retire) != 0 {
			guidance = append(guidance, agentConfigKeyRetirementGuidance(group, manifestPath, present)...)
		}
	}
	for _, decision := range result.ManagedPaths() {
		id, retained := blockedRetainedAgentConfigDocument(decision)
		if !retained {
			continue
		}
		if _, present := declared[id]; !present {
			guidance = append(guidance, agentConfigDocumentRetirementGuidance(decision, id, manifestPath)...)
		}
	}
	return guidance
}

func agentConfigKeyConflictGroups(result reconcile.Result) []agentConfigKeyConflicts {
	var groups []agentConfigKeyConflicts
	indices := make(map[nativeHumanGroupKey]int)
	for _, document := range result.Aggregates() {
		if !document.IsBlocked() {
			continue
		}
		for _, projection := range document.Projections() {
			if _, native := aggregate.AgentConfigSyntaxForCodec(projection.Contract().CodecContractID()); !native {
				continue
			}
			desired, hasDesired := projection.Desired()
			contributions := desired.Contributions()
			for _, decision := range projection.SubjectDecisions() {
				id, key, native := topologyprojection.AgentConfigKeyIdentity(decision.Subject())
				if !native || !decision.IsBlocked() {
					continue
				}
				switch decision.Reason() {
				case reconcile.ReasonDriftedOutput, reconcile.ReasonUnmanagedOutputExists, reconcile.ReasonMissingOutput:
				default:
					continue
				}
				replace := slices.ContainsFunc(contributions, func(value aggregate.SubjectContribution) bool {
					return value.SubjectID() == decision.Subject()
				})
				_, retained := decision.PreviousContribution()
				retire := !hasDesired && retained && decision.Reason() == reconcile.ReasonDriftedOutput
				if !replace && !retire {
					continue
				}
				identity := nativeHumanGroupKey{id, decision.Contract().Address().Document()}
				index, found := indices[identity]
				if !found {
					index = len(groups)
					indices[identity] = index
					groups = append(groups, agentConfigKeyConflicts{identity: identity})
				}
				if replace {
					groups[index].replace = append(groups[index].replace, key)
				} else {
					groups[index].retire = append(groups[index].retire, key)
				}
			}
		}
	}
	return groups
}

func agentConfigKeyReplacementGuidance(group agentConfigKeyConflicts, manifestPath string) []Guidance {
	var options []string
	detail := fmt.Sprintf("%s remain unchanged", CountLabel(len(group.replace), "blocked key", "blocked keys"))
	if len(group.replace) == 1 {
		key := group.replace[0]
		options = append(options, "--key", key.String())
		detail = fmt.Sprintf("key=%s remains unchanged", nativeKeyDisplay(key))
	}
	return []Guidance{
		agentConfigFileNote(group.identity.declaration, group.identity.document.AggregateRoot(), detail),
		agentConfigPreviewGuidance("replace", group.identity.declaration, manifestPath, "preview restoring declared values or taking over existing keys", options...),
		InstructionGuidance("to keep the current host value instead, align its source, run lock, and preview apply again"),
	}
}

func agentConfigKeyRetirementGuidance(group agentConfigKeyConflicts, manifestPath string, stillDeclared bool) []Guidance {
	identity := group.identity
	const maxKeyLabels = 5
	labels := make([]string, 0, min(len(group.retire), maxKeyLabels)+1)
	for _, key := range group.retire[:min(len(group.retire), maxKeyLabels)] {
		labels = append(labels, nativeKeyDisplay(key))
	}
	if omitted := len(group.retire) - len(labels); omitted > 0 {
		labels = append(labels, fmt.Sprintf("and %d more", omitted))
	}
	guidance := []Guidance{agentConfigFileNote(identity.declaration, identity.document.AggregateRoot(),
		"keys blocked from removal because their values differ from the recorded baseline: "+strings.Join(labels, ", "))}
	if !stillDeclared {
		guidance = append(guidance, agentConfigReleaseGuidance(identity.declaration, identity.document.Target(), identity.document.Scope(), manifestPath,
			"to keep the current values and release management, preview unmanage")...)
	}
	return append(guidance, InstructionGuidance("to delete keys that are no longer wanted, first save any changes you need, remove only those keys from the host file, then preview apply again"))
}

func blockedRetainedAgentConfigDocument(decision reconcile.ManagedPathDecision) (entity.ID, bool) {
	if !decision.IsBlocked() {
		return entity.ID{}, false
	}
	if decision.Reason() != reconcile.ReasonDriftedOutput && decision.Reason() != reconcile.ReasonMissingOutput {
		return entity.ID{}, false
	}
	if _, retained := decision.PreviousState(); !retained {
		return entity.ID{}, false
	}
	id, found := topologyprojection.EntityID(decision.Subject())
	return id, found && id.Kind() == entity.KindAgentConfig
}

func agentConfigDocumentRetirementGuidance(decision reconcile.ManagedPathDecision, id entity.ID, manifestPath string) []Guidance {
	missing := decision.Reason() == reconcile.ReasonMissingOutput
	detail := "removal is blocked because the file differs from its recorded contents or permissions"
	if missing {
		detail = "the file is already missing, but apply cannot retire its management"
	}
	guidance := []Guidance{agentConfigFileNote(id, decision.Destination(), detail)}
	previous, _ := decision.PreviousState()
	targets := previous.ConsumerTargets()
	if len(targets) == 1 {
		instruction := "to keep the file and release management, preview unmanage"
		if missing {
			instruction = "to release the missing file's management, preview unmanage"
		}
		guidance = append(guidance, agentConfigReleaseGuidance(id, targets[0], decision.Scope(), manifestPath, instruction)...)
	}
	if !missing {
		guidance = append(guidance, InstructionGuidance("to delete the file yourself, save any needed changes and release its management before deleting it"))
	}
	return guidance
}

func agentConfigFileNote(id entity.ID, destination output.Destination, message string) Guidance {
	return NoteGuidance(fmt.Sprintf("agent configuration %s: file=%s %s", Quote(id.Name()), Quote(destination.String()), message))
}

func agentConfigReleaseGuidance(id entity.ID, selected target.Target, scope target.Scope, manifestPath, instruction string) []Guidance {
	return []Guidance{
		InstructionGuidance(instruction),
		agentConfigPreviewGuidance("unmanage", id, manifestPath, "", "--target", string(selected), "--scope", string(scope)),
	}
}

func agentConfigPreviewGuidance(operation string, id entity.ID, manifestPath, purpose string, options ...string) Guidance {
	command := []string{"daem", operation, "agent-config"}
	literal := strings.HasPrefix(id.Name(), "-")
	if !literal {
		command = append(command, id.Name())
	}
	command = append(command, options...)
	command = append(command, "--dry-run")
	if manifestPath != "" {
		command = append(command, "--manifest", manifestPath)
	}
	if literal {
		command = append(command, "--", id.Name())
	}
	guidance, err := CommandGuidance(command, purpose)
	if err != nil {
		return InstructionGuidance(fmt.Sprintf("preview %s agent-config for the selected declaration in the same manifest", operation))
	}
	return guidance
}
