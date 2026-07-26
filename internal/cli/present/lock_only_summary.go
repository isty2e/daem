package clipresent

import (
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
	targetavailability "github.com/isty2e/daem/internal/target/availability"
)

type LockOnlyResources struct {
	Skills []LockOnlyResource `json:"skills"`
	Hooks  []LockOnlyResource `json:"hooks"`
}

type LockOnlyResource struct {
	Kind    string   `json:"kind"`
	Name    string   `json:"name"`
	Targets []string `json:"targets"`
}

// LockOnlyResourcesFrom projects canonical unsupported-target facts into the
// stable CLI lock_only shape.
func LockOnlyResourcesFrom(projections []targetavailability.UnsupportedProjection) LockOnlyResources {
	resources := LockOnlyResources{
		Skills: []LockOnlyResource{},
		Hooks:  []LockOnlyResource{},
	}
	for _, projection := range projections {
		entityID := projection.EntityID()
		entry := LockOnlyResource{
			Kind:    string(entityID.Kind()),
			Name:    entityID.Name(),
			Targets: lockOnlyTargetStrings(projection.Targets()),
		}
		switch entityID.Kind() {
		case entity.KindSkill:
			resources.Skills = append(resources.Skills, entry)
		case entity.KindHook:
			resources.Hooks = append(resources.Hooks, entry)
		}
	}
	return resources
}

func PrintLockOnlyResourceSummary(output io.Writer, resources LockOnlyResources) {
	printLockOnlyResourceCountSummary(output, resources)
}

func PrintStatusLockOnlyResourcesWithOptions(output io.Writer, resources LockOnlyResources, options HumanOptions) {
	printLockOnlyResourceCountSummary(output, resources)
	if options.Verbose {
		PrintLockOnlyResourceDetails(output, resources)
	}
}

func printLockOnlyResourceCountSummary(output io.Writer, resources LockOnlyResources) {
	if len(resources.Skills) == 0 && len(resources.Hooks) == 0 {
		return
	}

	fmt.Fprintf(output, "lock-only: skills=%d hooks=%d (unsupported or not reconciled by apply/status)\n", len(resources.Skills), len(resources.Hooks))
}

func PrintLockOnlyResourceDetails(output io.Writer, resources LockOnlyResources) {
	for _, resource := range resources.Skills {
		printLockOnlyResourceDetail(output, resource)
	}
	for _, resource := range resources.Hooks {
		printLockOnlyResourceDetail(output, resource)
	}
}

func printLockOnlyResourceDetail(output io.Writer, resource LockOnlyResource) {
	fmt.Fprintf(output, "  - %s/%s targets=%s\n", resource.Kind, resource.Name, strings.Join(resource.Targets, ","))
}

func lockOnlyTargetStrings(targets []target.Target) []string {
	values := make([]string, 0, len(targets))
	for _, selected := range targets {
		values = append(values, string(selected))
	}
	return values
}
