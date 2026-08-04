package delegate

import "strings"

// npx options belong to npx only before the first positional command.
// https://docs.npmjs.com/cli/v11/commands/npx/
func npmPackageSpecs(args []string) ([]string, bool, error) {
	packages := make([]string, 0)
	complete := true
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--package" || arg == "-p":
			if index+1 >= len(args) {
				return nil, false, missingOptionValue(RunnerNPX, arg)
			}
			packages = append(packages, args[index+1])
			index++
		case strings.HasPrefix(arg, "--package="):
			value := strings.TrimPrefix(arg, "--package=")
			if value == "" {
				return nil, false, missingOptionValue(RunnerNPX, "--package")
			}
			packages = append(packages, value)
		case arg == "--":
			if len(packages) > 0 {
				return packages, complete, nil
			}
			if index+1 >= len(args) {
				return packages, complete, nil
			}
			return []string{args[index+1]}, complete, nil
		case arg == "--call" || arg == "-c":
			if index+1 >= len(args) {
				return nil, false, missingOptionValue(RunnerNPX, arg)
			}
			// The call string can execute ambient project binaries that are not
			// represented by --package selectors.
			complete = false
			index++
		case strings.HasPrefix(arg, "--call="):
			if strings.TrimPrefix(arg, "--call=") == "" {
				return nil, false, missingOptionValue(RunnerNPX, "--call")
			}
			complete = false
		case arg == "--script-shell" || arg == "--shell" || arg == "--workspace" || arg == "-w":
			if index+1 >= len(args) {
				return nil, false, missingOptionValue(RunnerNPX, arg)
			}
			index++
		case strings.HasPrefix(arg, "--script-shell=") || strings.HasPrefix(arg, "--shell=") ||
			strings.HasPrefix(arg, "--workspace="):
		case arg == "--yes" || arg == "-y" || arg == "--no" || arg == "--no-install" ||
			arg == "--workspaces" || arg == "-ws" || arg == "--include-workspace-root":
		case strings.HasPrefix(arg, "-"):
			// Unknown npm config options can change how later argv is parsed.
			// Preserve the exact command, but never claim complete package facts.
			complete = false
			if !strings.Contains(arg, "=") && index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
				return packages, false, nil
			}
		default:
			if len(packages) > 0 {
				return packages, complete, nil
			}
			if !complete {
				return nil, false, nil
			}
			return []string{arg}, true, nil
		}
	}
	if len(packages) > 0 {
		return packages, complete, nil
	}
	return packages, complete, nil
}

func splitNPMSpec(spec string) (string, string, error) {
	if strings.HasSuffix(spec, "@") {
		return "", "", validationError(ReasonInvalidPackageRef, spec, "npm package selector is empty")
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
