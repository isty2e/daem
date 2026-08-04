package cli

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func newCommandFlagSet(path []string, stderr io.Writer) *flag.FlagSet {
	commandPath := append([]string(nil), path...)
	flags := flag.NewFlagSet(strings.Join(commandPath, " "), flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		printCommandHelpHint(stderr, commandPath)
	}
	return flags
}

func splitAuthoringArgs(
	args []string,
	positionalCount int,
	flagTakesValue func(string) bool,
) ([]string, []string, error) {
	positionals := make([]string, 0, positionalCount)
	flagArgs := make([]string, 0, len(args))
	positionalOnly := false

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !positionalOnly && arg == "--" {
			positionalOnly = true
			continue
		}
		if !positionalOnly && (arg == "-h" || arg == "--help") {
			flagArgs = append(flagArgs, arg)
			continue
		}
		if !positionalOnly && strings.HasPrefix(arg, "--") {
			name, _, hasInlineValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			if !flagTakesValue(name) && !authoringBooleanFlag(name) {
				return nil, nil, fmt.Errorf("flag provided but not defined: -%s", name)
			}
			flagArgs = append(flagArgs, arg)
			if !hasInlineValue && flagTakesValue(name) {
				index++
				if index >= len(args) {
					return nil, nil, fmt.Errorf("flag needs an argument: --%s", name)
				}
				flagArgs = append(flagArgs, args[index])
				continue
			}
			continue
		}
		if !positionalOnly && strings.HasPrefix(arg, "-") && arg != "-" {
			return nil, nil, fmt.Errorf("flag provided but not defined: %s", arg)
		}
		if len(positionals) == positionalCount {
			return nil, nil, fmt.Errorf("unexpected argument %q", arg)
		}
		positionals = append(positionals, arg)
	}

	return positionals, flagArgs, nil
}

func authoringBooleanFlag(name string) bool {
	switch name {
	case "dry-run", "diff", "json", "verbose":
		return true
	default:
		return false
	}
}

type targetFlagValues []target.Target

func (values *targetFlagValues) String() string {
	return fmt.Sprint(values.strings())
}

func (values *targetFlagValues) Set(value string) error {
	parsed, err := target.ParseTarget(value)
	if err != nil {
		return err
	}
	if slices.Contains(*values, parsed) {
		return nil
	}
	*values = append(*values, parsed)
	return nil
}

func (values targetFlagValues) strings() []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

type scopeFlagValues []target.Scope

func (values *scopeFlagValues) String() string {
	return fmt.Sprint(values.strings())
}

func (values *scopeFlagValues) Set(value string) error {
	parsed, err := target.ParseScope(value)
	if err != nil {
		return err
	}
	if slices.Contains(*values, parsed) {
		return nil
	}
	*values = append(*values, parsed)
	return nil
}

func (values scopeFlagValues) strings() []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

type skillGroupMemberFlagValues []string

func (values *skillGroupMemberFlagValues) String() string {
	return fmt.Sprint([]string(*values))
}

func (values *skillGroupMemberFlagValues) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func singleScopeValue(values scopeFlagValues) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > 1 {
		return "", fmt.Errorf("--scope accepts at most one distinct scope for this command")
	}
	return string(values[0]), nil
}

func skillGroupMembers(values skillGroupMemberFlagValues) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one --member is required")
	}

	names := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		name, err := authoring.CleanSkillGroupName(value)
		if err != nil {
			return nil, fmt.Errorf("--member: %w", err)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

func validatePresentationFlags(command string, jsonOutput bool, verbose bool, showDiff bool) error {
	if jsonOutput && verbose {
		return fmt.Errorf("%s failed: --json and --verbose are mutually exclusive", command)
	}
	if jsonOutput && showDiff {
		return fmt.Errorf("%s failed: --json and --diff are mutually exclusive", command)
	}
	return nil
}

func parsePositiveDuration(name string, raw string) (time.Duration, error) {
	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 30s: %w", name, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return duration, nil
}

func tomlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
