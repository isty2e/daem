package diagnose

import (
	"context"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	skillresource "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

const (
	skillDiscoveryDuplicateRetainedCode = "skill_discovery_duplicate_retained"
	skillDiscoveryObservationFailedCode = "skill_discovery_observation_failed"
)

type skillDiscoveryFinding struct {
	code         string
	entityID     entity.ID
	target       target.Target
	scope        target.Scope
	selectedPath string
	observedPath string
	cause        error
}

// RetainedSkillDiscoveryChecks reports same-name skill paths that doctor can
// prove are visible through another modeled discovery root and are not already
// represented by the current manifest state.
func RetainedSkillDiscoveryChecks(
	ctx context.Context,
	paths daempaths.Paths,
	skills []skillresource.Skill,
	selection targetselection.Selection,
) []findings.Check {
	if len(skills) == 0 {
		return nil
	}
	currentState, err := statefile.LoadOptional(ctx, paths.StatefilePath)
	if err != nil {
		return []findings.Check{findings.ErrorCheck(
			"skill_discovery_state",
			fmt.Sprintf("read managed state for skill discovery diagnostics: %v", err),
		)}
	}
	found := inspectRetainedSkillDiscoveries(
		ctx,
		paths,
		skills,
		selection,
		skillDiscoveryCoverageFromState(currentState),
		skillDiscoveryObserver{stat: os.Stat},
	)
	checks := make([]findings.Check, 0, len(found))
	for _, finding := range found {
		checks = append(checks, finding.check())
	}
	return checks
}

// RetainedSkillDiscoveryDiagnostics reports same-name skill paths that the
// current reconciliation plan will retain.
func RetainedSkillDiscoveryDiagnostics(
	ctx context.Context,
	paths daempaths.Paths,
	skills []skillresource.Skill,
	selection targetselection.Selection,
	planned reconcile.Result,
) []findings.Diagnostic {
	found := inspectRetainedSkillDiscoveries(
		ctx,
		paths,
		skills,
		selection,
		skillDiscoveryCoverageFromPlan(planned),
		skillDiscoveryObserver{stat: os.Stat},
	)
	diagnostics := make([]findings.Diagnostic, 0, len(found))
	for _, finding := range found {
		diagnostics = append(diagnostics, finding.diagnostic())
	}
	return diagnostics
}

func newRetainedSkillDiscoveryFinding(
	skill skillresource.Skill,
	selectedTarget target.Target,
	selectedPath string,
	observedPath string,
) skillDiscoveryFinding {
	return skillDiscoveryFinding{
		code:         skillDiscoveryDuplicateRetainedCode,
		entityID:     skill.ID(),
		target:       selectedTarget,
		scope:        skill.Scope(),
		selectedPath: selectedPath,
		observedPath: observedPath,
	}
}

func newSkillDiscoveryObservationFailure(
	skill skillresource.Skill,
	selectedTarget target.Target,
	selectedPath string,
	observedPath string,
	cause error,
) skillDiscoveryFinding {
	return skillDiscoveryFinding{
		code:         skillDiscoveryObservationFailedCode,
		entityID:     skill.ID(),
		target:       selectedTarget,
		scope:        skill.Scope(),
		selectedPath: selectedPath,
		observedPath: observedPath,
		cause:        cause,
	}
}

func (finding skillDiscoveryFinding) check() findings.Check {
	return findings.Check{
		Status: findings.CheckWarn,
		Name: fmt.Sprintf(
			"%s target=%s scope=%s skill=%s",
			finding.code,
			finding.target,
			finding.scope,
			finding.entityID.Name(),
		),
		Detail:   finding.detail(),
		NextStep: finding.nextStep(),
	}
}

func (finding skillDiscoveryFinding) diagnostic() findings.Diagnostic {
	return findings.Diagnostic{
		Severity: findings.SeverityWarn,
		Code:     finding.code,
		EntityID: finding.entityID,
		Target:   finding.target,
		Scope:    finding.scope,
		Event:    finding.event(),
		Detail:   finding.detail(),
		NextStep: finding.nextStep(),
	}
}

func (finding skillDiscoveryFinding) detail() string {
	if finding.code == skillDiscoveryObservationFailedCode {
		return fmt.Sprintf(
			"inspect skill discovery path %q for selected destination %q: %v",
			finding.observedPath,
			finding.selectedPath,
			finding.cause,
		)
	}
	return fmt.Sprintf(
		"selected skill destination is %q; same-name discovery entry %q is not scheduled for migration or removal by this command and will be retained",
		finding.selectedPath,
		finding.observedPath,
	)
}

func (finding skillDiscoveryFinding) nextStep() string {
	if finding.code == skillDiscoveryObservationFailedCode {
		return "restore access to the discovery path and rerun the command"
	}
	return "confirm which copy the agent loads, then remove the retained entry manually or migrate it into the selected destination"
}

func (finding skillDiscoveryFinding) event() string {
	if finding.code == skillDiscoveryObservationFailedCode {
		return "discovery-observation-failed"
	}
	return "retained-discovery-entry"
}

func (finding skillDiscoveryFinding) sortKey() string {
	return fmt.Sprintf(
		"%s\x00%s\x00%s\x00%s\x00%s",
		finding.entityID.String(),
		finding.target,
		finding.scope,
		finding.code,
		finding.observedPath,
	)
}
