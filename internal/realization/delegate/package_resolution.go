package delegate

import "sort"

type packageResolution struct {
	refs     []PackageRef
	complete bool
}

func derivePackageResolution(runner Runner, command CommandSpec) (packageResolution, error) {
	var (
		specs       []string
		ecosystem   PackageEcosystem
		complete    = true
		requiresRef bool
	)

	switch runner.Kind() {
	case RunnerPlain:
		return packageResolution{complete: true}, nil
	case RunnerHostNative:
		return packageResolution{complete: true}, nil
	case RunnerNPX:
		ecosystem = EcosystemNPM
		requiresRef = true
		var err error
		specs, complete, err = npmPackageSpecs(command.Args())
		if err != nil {
			return packageResolution{}, err
		}
	case RunnerUVX:
		ecosystem = EcosystemPython
		requiresRef = true
		var err error
		specs, complete, err = pythonPackageSpecs(command.Args())
		if err != nil {
			return packageResolution{}, err
		}
	case RunnerDocker:
		ecosystem = EcosystemContainer
		requiresRef = true
		spec, found, resolved, err := dockerImageSpec(command.Args())
		if err != nil {
			return packageResolution{}, err
		}
		complete = resolved
		if found {
			specs = []string{spec}
		}
	default:
		return packageResolution{}, validationError(ReasonInvalidRunnerKind, string(runner.Kind()), "unsupported runner kind")
	}

	refs, represented := canonicalPackageRefsFromSpecs(ecosystem, specs)
	complete = complete && represented
	if requiresRef && len(refs) == 0 && complete {
		return packageResolution{}, validationError(
			ReasonMissingPackage,
			string(runner.Kind()),
			"package-backed runner requires a package argument",
		)
	}
	return packageResolution{refs: refs, complete: complete}, nil
}

// canonicalPackageRefsFromSpecs projects only package syntax that daem can
// represent without changing the delegated argv. Unrepresentable syntax stays
// opaque and lowers completeness; command validation remains a separate gate.
func canonicalPackageRefsFromSpecs(ecosystem PackageEcosystem, specs []string) ([]PackageRef, bool) {
	refs := make([]PackageRef, 0, len(specs))
	complete := true
	for _, spec := range specs {
		name, selector, err := splitPackageSpec(ecosystem, spec)
		if err != nil {
			complete = false
			continue
		}
		ref, err := NewPackageRef(ecosystem, name, selector)
		if err != nil {
			complete = false
			continue
		}
		refs = append(refs, ref)
	}
	return canonicalPackageRefs(refs), complete
}

func packageResolutionPinPolicy(runner Runner, resolution packageResolution) PinPolicy {
	switch runner.Kind() {
	case RunnerPlain:
		return PinNotApplicable
	case RunnerHostNative:
		return PinHostSelected
	}
	if !resolution.complete || len(resolution.refs) == 0 {
		return PinFloating
	}
	for _, ref := range resolution.refs {
		if ref.PinPolicy() != PinPinned {
			return PinFloating
		}
	}
	return PinPinned
}

func canonicalPackageRefs(refs []PackageRef) []PackageRef {
	canonical := append([]PackageRef(nil), refs...)
	sort.Slice(canonical, func(left int, right int) bool {
		if canonical[left].Ecosystem() != canonical[right].Ecosystem() {
			return canonical[left].Ecosystem() < canonical[right].Ecosystem()
		}
		if canonical[left].Name() != canonical[right].Name() {
			return canonical[left].Name() < canonical[right].Name()
		}
		return canonical[left].Selector() < canonical[right].Selector()
	})
	result := canonical[:0]
	for _, ref := range canonical {
		if len(result) == 0 || result[len(result)-1] != ref {
			result = append(result, ref)
		}
	}
	return result
}

func splitPackageSpec(ecosystem PackageEcosystem, spec string) (string, string, error) {
	switch ecosystem {
	case EcosystemNPM:
		return splitNPMSpec(spec)
	case EcosystemPython:
		return splitPythonSpec(spec)
	case EcosystemContainer:
		return splitDockerSpec(spec)
	default:
		return "", "", validationError(ReasonInvalidPackageRef, string(ecosystem), "unsupported package ecosystem")
	}
}

func missingOptionValue(runner RunnerKind, option string) error {
	return validationError(
		ReasonInvalidDelegatePlan,
		string(runner),
		"delegated runner option "+option+" requires a value",
	)
}

func requiredSeparateOptionValue(args []string, optionIndex int, runner RunnerKind) (string, error) {
	option := args[optionIndex]
	if optionIndex+1 >= len(args) {
		return "", missingOptionValue(runner, option)
	}

	value := args[optionIndex+1]
	switch value {
	case "":
		return "", validationError(
			ReasonInvalidDelegatePlan,
			string(runner),
			"delegated runner option "+option+" requires a non-empty value",
		)
	case "--":
		return "", validationError(
			ReasonInvalidDelegatePlan,
			string(runner),
			"delegated runner option "+option+" cannot consume the command delimiter as its value",
		)
	default:
		return value, nil
	}
}
