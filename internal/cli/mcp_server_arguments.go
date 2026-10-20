package cli

import (
	"errors"
	"fmt"
	"strings"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

type mcpInputFlagValues struct {
	values []string
}

func (values mcpInputFlagValues) String() string { return "" }

func (values *mcpInputFlagValues) Set(value string) error {
	values.values = append(values.values, value)
	return nil
}

func mcpInputTargets(raw mcpInputFlagValues) ([]string, error) {
	var targets targetFlagValues
	for _, value := range raw.values {
		if err := targets.Set(value); err != nil {
			accepted := targetFlagValues(target.SupportedTargets()).strings()
			return nil, fmt.Errorf("--target accepts: %s", strings.Join(accepted, ", "))
		}
	}

	return targets.strings(), nil
}

func mcpInputScope(raw mcpInputFlagValues) (string, error) {
	var scopes scopeFlagValues
	for _, value := range raw.values {
		if err := scopes.Set(value); err != nil {
			accepted := scopeFlagValues(target.SupportedScopes()).strings()
			return "", fmt.Errorf("--scope accepts: %s", strings.Join(accepted, ", "))
		}
	}

	return singleScopeValue(scopes)
}

func mcpInputEnvironment(raw mcpInputFlagValues) ([]authoring.MCPServerEnvAssignment, error) {
	assignments := make([]authoring.MCPServerEnvAssignment, 0, len(raw.values))
	for _, value := range raw.values {
		child, source, present := strings.Cut(value, "=")
		if !present || child == "" || !mcpInputSourceName(source) {
			return nil, fmt.Errorf("--env-from requires CHILD=SOURCE names; SOURCE must be an uppercase host environment variable name, not a value")
		}
		assignments = append(assignments, authoring.MCPServerEnvAssignment{Name: child, FromEnv: source})
	}

	return assignments, nil
}

func mcpInputSourceName(value string) bool {
	if value == "" {
		return false
	}

	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') || character == '_' || (index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}

	return true
}

func mcpInputBindingDiagnostic(path []string, plan authoringArgumentPlan, err error, args []string) clipresent.FailureDiagnostic {
	var unexpected *unexpectedArgumentError
	if errors.As(err, &unexpected) {
		diagnostic := clipresent.NewFailureDiagnostic(fmt.Sprintf("unexpected operand %d", len(plan.positionals)+1)).
			WithGuidance(clipresent.InstructionGuidance("pass MCP command arguments with repeated --arg <value>"))
		if guidance, present := unexpectedArgumentTargetGuidance(err, args); present {
			diagnostic = diagnostic.WithGuidance(guidance)
		}
		return diagnostic.WithGuidance(commandHelpGuidance(path))
	}

	var option *optionArgumentError
	if errors.As(err, &option) {
		return authoringArgumentDiagnostic(path, err, args)
	}

	return clipresent.NewFailureDiagnostic("invalid MCP server option value; use true or false for boolean options").
		WithGuidance(commandHelpGuidance(path))
}
