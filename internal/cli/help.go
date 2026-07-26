package cli

import (
	"fmt"
	"io"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/target"
	workflowhelp "github.com/isty2e/daem/internal/workflow/help"
)

func printUsage(output io.Writer, width int) {
	clipresent.PrintUsage(output, usageContext(width))
}

func printCommandUsage(path []string, output io.Writer, width int) bool {
	return clipresent.PrintCommandUsage(output, path, usageContext(width))
}

func printCommandHelpHint(output io.Writer, path []string) {
	fmt.Fprintf(output, "next: run daem help %s\n", strings.Join(path, " "))
}

func usageContext(width int) clipresent.UsageContext {
	facts := workflowhelp.BuildUsageFacts()
	return clipresent.UsageContext{
		SupportedTargets:          targetValuesForHelp(facts.SupportedTargets),
		ImportTargets:             targetValuesForHelp(facts.ImportTargets),
		MCPAuthoringTargets:       mcpPlacementTargetsForHelp(facts.MCPAuthoringPlacements),
		MCPAuthoringScopes:        mcpPlacementScopesForHelp(facts.MCPAuthoringPlacements),
		MCPAuthoringPlacements:    mcpPlacementValuesForHelp(facts.MCPAuthoringPlacements),
		MCPRuntimeProbeTargets:    mcpPlacementTargetsForHelp(facts.MCPRuntimeProbePlacements),
		MCPRuntimeProbeScopes:     mcpPlacementScopesForHelp(facts.MCPRuntimeProbePlacements),
		MCPRuntimeProbePlacements: mcpPlacementValuesForHelp(facts.MCPRuntimeProbePlacements),
		Width:                     width,
	}
}

func supportedTargetValuesForHelp() string {
	return targetValuesForHelp(target.SupportedTargets())
}

func targetValuesForHelp(targets []target.Target) string {
	values := make([]string, 0, len(targets))
	for _, target := range targets {
		values = append(values, string(target))
	}

	return strings.Join(values, ", ")
}

func mcpPlacementTargetsForHelp(placements []workflowhelp.MCPPlacementFact) string {
	values := make([]string, 0, len(placements))
	seen := make(map[target.Target]struct{})
	for _, placement := range placements {
		if _, duplicate := seen[placement.Target]; duplicate {
			continue
		}
		seen[placement.Target] = struct{}{}
		values = append(values, string(placement.Target))
	}
	return strings.Join(values, ", ")
}

func mcpPlacementScopesForHelp(placements []workflowhelp.MCPPlacementFact) string {
	values := make([]string, 0, len(placements))
	seen := make(map[target.Scope]struct{})
	for _, placement := range placements {
		if _, duplicate := seen[placement.Scope]; duplicate {
			continue
		}
		seen[placement.Scope] = struct{}{}
		values = append(values, string(placement.Scope))
	}
	return strings.Join(values, ", ")
}

func mcpPlacementValuesForHelp(placements []workflowhelp.MCPPlacementFact) string {
	values := make([]string, 0, len(placements))
	for _, placement := range placements {
		values = append(values, string(placement.Target)+"/"+string(placement.Scope))
	}
	return strings.Join(values, ", ")
}

func commandHelpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
	}

	return len(args) == 1 && args[0] == "help"
}

func handleGroupHelp(group string, args []string, stdout io.Writer, stderr io.Writer) (bool, int) {
	if len(args) == 0 {
		printCommandUsage([]string{group}, stdout, 0)
		return true, 2
	}
	if len(args) == 1 && commandHelpRequested(args) {
		printCommandUsage([]string{group}, stdout, 0)
		return true, 0
	}
	if args[0] != "help" {
		return false, 0
	}
	if len(args) != 2 {
		if len(args) < 2 {
			printCommandUsage([]string{group}, stdout, 0)
			return true, 0
		}
		fmt.Fprintf(stderr, "unexpected argument %q\n", args[2])
		return true, 2
	}
	path := []string{group, args[1]}
	if printCommandUsage(path, stdout, 0) {
		return true, 0
	}
	fmt.Fprintf(stderr, "unknown help topic %q\n", strings.Join(path, " "))
	fmt.Fprintf(stderr, "next: run daem help %s\n", group)
	return true, 2
}
