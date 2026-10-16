package cli

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/workflow/authoring"
)

func requiredAuthoringOperand(plan authoringArgumentPlan, missing string) (string, error) {
	positionals, err := plan.operands()
	if err != nil {
		return "", err
	}
	if len(positionals) == 0 || strings.TrimSpace(positionals[0]) == "" {
		return "", fmt.Errorf("%s", missing)
	}
	return positionals[0], nil
}

func extensionIDOperand(plan authoringArgumentPlan) (string, error) {
	positionals, err := plan.operands()
	if err != nil {
		return "", err
	}
	id := ""
	if len(positionals) != 0 {
		id = positionals[0]
	}
	return authoring.CleanExtensionID(id)
}

func validateAuthoringPresentation(command string, output requestedOutput, verbose, diff, dryRun bool) error {
	if diff && !dryRun {
		return fmt.Errorf("%s failed: --diff requires --dry-run", command)
	}
	return validatePresentationFlags(command, output == requestedJSON, verbose, diff)
}
