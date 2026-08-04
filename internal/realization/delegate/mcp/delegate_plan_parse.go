package mcp

import (
	"strings"

	"github.com/isty2e/daem/internal/realization/delegate"
)

func packageBackedRunner(
	kind delegate.RunnerKind,
	command delegate.CommandSpec,
	parse packageParser,
) (delegate.Runner, *delegate.PackageRef, delegate.PinPolicy, error) {
	runner, err := delegate.NewRunner(kind)
	if err != nil {
		return delegate.Runner{}, nil, "", err
	}
	packageRef, err := parse(command.Args())
	if err != nil {
		return delegate.Runner{}, nil, "", err
	}
	return runner, &packageRef, packageRef.PinPolicy(), nil
}

func parseNPMDelegatePackage(args []string) (delegate.PackageRef, error) {
	spec, ok := npmPackageSpec(args)
	if !ok {
		return delegate.PackageRef{}, missingPackageError("npx")
	}
	name, selector, err := splitNPMSpec(spec)
	if err != nil {
		return delegate.PackageRef{}, err
	}
	return newDelegatePackageRef(delegate.EcosystemNPM, name, selector)
}

func parsePythonDelegatePackage(args []string) (delegate.PackageRef, error) {
	spec, ok := uvxPackageSpec(args)
	if !ok {
		return delegate.PackageRef{}, missingPackageError("uvx")
	}
	name, selector, err := splitPythonSpec(spec)
	if err != nil {
		return delegate.PackageRef{}, err
	}
	return newDelegatePackageRef(delegate.EcosystemPython, name, selector)
}

func parseDockerDelegatePackage(args []string) (delegate.PackageRef, error) {
	spec, ok := dockerImageSpec(args)
	if !ok {
		return delegate.PackageRef{}, missingPackageError("docker")
	}
	name, selector, err := splitDockerSpec(spec)
	if err != nil {
		return delegate.PackageRef{}, err
	}
	return newDelegatePackageRef(delegate.EcosystemContainer, name, selector)
}

func newDelegatePackageRef(ecosystem delegate.PackageEcosystem, name string, selector string) (delegate.PackageRef, error) {
	packageRef, err := delegate.NewPackageRef(ecosystem, name, selector)
	if err != nil {
		return delegate.PackageRef{}, newMCPDelegatePlanError(
			MCPDelegatePlanReasonInvalidPackage,
			name,
			"invalid delegated package reference",
			err,
		)
	}
	return packageRef, nil
}

func missingPackageError(runner string) error {
	return newMCPDelegatePlanError(
		MCPDelegatePlanReasonMissingPackage,
		runner,
		"package-backed MCP delegate command requires a package argument",
		nil,
	)
}

func npmPackageSpec(args []string) (string, bool) {
	for index, arg := range args {
		if arg == "--package" || arg == "-p" {
			if index+1 >= len(args) {
				return "", false
			}
			return args[index+1], true
		}
		if after, ok := strings.CutPrefix(arg, "--package="); ok {
			return after, true
		}
	}
	return firstNonOptionArg(args)
}

func uvxPackageSpec(args []string) (string, bool) {
	for index, arg := range args {
		if arg == "--from" {
			if index+1 >= len(args) {
				return "", false
			}
			return args[index+1], true
		}
		if after, ok := strings.CutPrefix(arg, "--from="); ok {
			return after, true
		}
	}
	return firstNonOptionArg(args)
}

func dockerImageSpec(args []string) (string, bool) {
	start := 0
	if len(args) > 0 && args[0] == "run" {
		start = 1
	}
	for index := start; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			if !strings.Contains(arg, "=") && dockerLongOptionTakesValue(arg) {
				index++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if dockerShortOptionTakesValue(arg) {
				index++
			}
			continue
		}
		return arg, true
	}
	return "", false
}

func firstNonOptionArg(args []string) (string, bool) {
	for _, arg := range args {
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg, true
	}
	return "", false
}

func splitNPMSpec(spec string) (string, string, error) {
	if strings.HasSuffix(spec, "@") {
		return "", "", newMCPDelegatePlanError(MCPDelegatePlanReasonInvalidPackage, spec, "npm package selector is empty", nil)
	}
	if strings.HasPrefix(spec, "@") {
		slash := strings.Index(spec, "/")
		if slash == -1 {
			return spec, "", nil
		}
		tail := spec[slash+1:]
		selectorIndex := strings.LastIndex(tail, "@")
		if selectorIndex <= 0 {
			return spec, "", nil
		}
		nameEnd := slash + 1 + selectorIndex
		return spec[:nameEnd], spec[nameEnd+1:], nil
	}
	selectorIndex := strings.LastIndex(spec, "@")
	if selectorIndex <= 0 {
		return spec, "", nil
	}
	return spec[:selectorIndex], spec[selectorIndex+1:], nil
}

func splitPythonSpec(spec string) (string, string, error) {
	selectorIndex, operator := pythonSpecifierBoundary(spec)
	if selectorIndex < 0 {
		return spec, "", nil
	}
	if selectorIndex == 0 || len(spec) == selectorIndex+len(operator) {
		return "", "", newMCPDelegatePlanError(MCPDelegatePlanReasonInvalidPackage, spec, "python package selector is empty", nil)
	}
	selector := spec[selectorIndex:]
	if operator == "==" && !strings.ContainsAny(selector[len(operator):], "*,") {
		selector = selector[len(operator):]
	}
	return spec[:selectorIndex], selector, nil
}

func pythonSpecifierBoundary(spec string) (int, string) {
	operators := [...]string{"===", "~=", "==", "!=", "<=", ">=", "<", ">"}
	bestIndex := -1
	bestOperator := ""
	for _, operator := range operators {
		index := strings.Index(spec, operator)
		if index >= 0 && (bestIndex < 0 || index < bestIndex) {
			bestIndex = index
			bestOperator = operator
		}
	}
	return bestIndex, bestOperator
}

func splitDockerSpec(spec string) (string, string, error) {
	if strings.HasSuffix(spec, "@") || strings.HasSuffix(spec, ":") {
		return "", "", newMCPDelegatePlanError(MCPDelegatePlanReasonInvalidPackage, spec, "container image selector is empty", nil)
	}
	if selectorIndex := strings.LastIndex(spec, "@"); selectorIndex > 0 {
		return spec[:selectorIndex], spec[selectorIndex+1:], nil
	}
	lastSlash := strings.LastIndex(spec, "/")
	lastColon := strings.LastIndex(spec, ":")
	if lastColon > lastSlash {
		return spec[:lastColon], spec[lastColon+1:], nil
	}
	return spec, "", nil
}

func dockerLongOptionTakesValue(option string) bool {
	switch option {
	case "--add-host", "--cpus", "--entrypoint", "--env", "--env-file", "--label", "--mount",
		"--name", "--network", "--platform", "--publish", "--user", "--volume", "--workdir":
		return true
	default:
		return false
	}
}

func dockerShortOptionTakesValue(option string) bool {
	switch option {
	case "-e", "-h", "-l", "-m", "-p", "-u", "-v", "-w":
		return true
	default:
		return false
	}
}
