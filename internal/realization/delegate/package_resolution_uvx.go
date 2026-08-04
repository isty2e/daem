package delegate

import "strings"

// uvx accepts one command-providing package plus repeated additive package
// inputs before the command boundary.
// https://docs.astral.sh/uv/reference/cli/#uv-tool-run
func pythonPackageSpecs(args []string) ([]string, bool, error) {
	packages := make([]string, 0)
	var primary string
	commandFound := false
	complete := true
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--from":
			value, err := requiredSeparateOptionValue(args, index, RunnerUVX)
			if err != nil {
				return nil, false, err
			}
			primary = value
			index++
		case strings.HasPrefix(arg, "--from="):
			primary = strings.TrimPrefix(arg, "--from=")
			if primary == "" {
				return nil, false, missingOptionValue(RunnerUVX, "--from")
			}
		case arg == "--with" || arg == "-w" || arg == "--with-executables-from":
			value, err := requiredSeparateOptionValue(args, index, RunnerUVX)
			if err != nil {
				return nil, false, err
			}
			packages = append(packages, value)
			index++
		case strings.HasPrefix(arg, "--with="):
			value := strings.TrimPrefix(arg, "--with=")
			if value == "" {
				return nil, false, missingOptionValue(RunnerUVX, "--with")
			}
			packages = append(packages, value)
		case strings.HasPrefix(arg, "--with-executables-from="):
			value := strings.TrimPrefix(arg, "--with-executables-from=")
			if value == "" {
				return nil, false, missingOptionValue(RunnerUVX, "--with-executables-from")
			}
			packages = append(packages, value)
		case pythonOpaquePackageOption(arg):
			complete = false
			if !strings.Contains(arg, "=") {
				if _, err := requiredSeparateOptionValue(args, index, RunnerUVX); err != nil {
					return nil, false, err
				}
				index++
			}
		case arg == "--":
			if index+1 >= len(args) {
				return nil, false, missingUVXCommand()
			}
			if primary == "" {
				primary = uvxToolPackageSpec(args[index+1])
			}
			commandFound = true
			return resolvedPythonSpecs(primary, packages), complete, nil
		case pythonOptionTakesValue(arg):
			if !strings.Contains(arg, "=") {
				if _, err := requiredSeparateOptionValue(args, index, RunnerUVX); err != nil {
					return nil, false, err
				}
				index++
			}
		case pythonFlagOption(arg):
		case strings.HasPrefix(arg, "-"):
			// New uvx options are not guessed. Their exact argv remains in the
			// plan, while package assurance degrades until their arity is known.
			return resolvedPythonSpecs(primary, packages), false, nil
		case primary == "":
			primary = uvxToolPackageSpec(arg)
			commandFound = true
			return resolvedPythonSpecs(primary, packages), complete, nil
		default:
			commandFound = true
			return resolvedPythonSpecs(primary, packages), complete, nil
		}
	}
	if !commandFound {
		return nil, false, missingUVXCommand()
	}
	return resolvedPythonSpecs(primary, packages), complete, nil
}

func missingUVXCommand() error {
	return validationError(
		ReasonMissingPackage,
		string(RunnerUVX),
		"uvx delegate requires a command-providing package",
	)
}

func resolvedPythonSpecs(primary string, packages []string) []string {
	if primary == "" {
		return packages
	}
	return append([]string{primary}, packages...)
}

func pythonOpaquePackageOption(option string) bool {
	name := strings.SplitN(option, "=", 2)[0]
	switch name {
	case "--with-requirements", "--with-editable", "--constraints", "-c",
		"--build-constraints", "-b", "--overrides":
		return true
	default:
		return false
	}
}

func pythonOptionTakesValue(option string) bool {
	name := strings.SplitN(option, "=", 2)[0]
	switch name {
	case "--env-file", "--python-platform", "--torch-backend", "--index", "--default-index",
		"--index-url", "-i", "--extra-index-url", "--find-links", "-f", "--index-strategy",
		"--keyring-provider", "--upgrade-package", "-P", "--upgrade-group", "--resolution",
		"--prerelease", "--fork-strategy", "--exclude-newer", "--exclude-newer-package",
		"--no-sources-package", "--reinstall-package", "--link-mode", "--config-setting", "-C",
		"--config-settings-package", "--no-build-isolation-package", "--no-build-package",
		"--no-binary-package", "--cache-dir", "--refresh-package", "--python", "-p", "--color",
		"--allow-insecure-host", "--directory", "--project", "--config-file":
		return true
	default:
		return false
	}
}

func pythonFlagOption(option string) bool {
	switch option {
	case "--isolated", "--no-env-file", "--lfs", "--version", "-V", "--no-index", "--upgrade",
		"-U", "--no-sources", "--reinstall", "--compile-bytecode", "--no-build-isolation",
		"--no-build", "--no-binary", "--no-cache", "-n", "--refresh", "--managed-python",
		"--no-managed-python", "--no-python-downloads", "--quiet", "-q", "--verbose", "-v",
		"--system-certs", "--offline", "--no-progress", "--no-config", "--help", "-h":
		return true
	default:
		return strings.HasPrefix(option, "-q") && strings.Trim(option[1:], "q") == "" ||
			strings.HasPrefix(option, "-v") && strings.Trim(option[1:], "v") == ""
	}
}

func uvxToolPackageSpec(spec string) string {
	if index := strings.LastIndex(spec, "@"); index > 0 && index+1 < len(spec) {
		return spec[:index] + "==" + spec[index+1:]
	}
	return spec
}

func splitPythonSpec(spec string) (string, string, error) {
	selectorIndex, operator := pythonSpecifierBoundary(spec)
	if selectorIndex < 0 {
		return spec, "", nil
	}
	if selectorIndex == 0 || len(spec) == selectorIndex+len(operator) {
		return "", "", validationError(ReasonInvalidPackageRef, spec, "python package selector is empty")
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
