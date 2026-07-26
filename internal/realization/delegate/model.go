package delegate

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// RunnerKind identifies how a delegated executable request reaches code.
type RunnerKind string

const (
	RunnerPlain      RunnerKind = "plain"
	RunnerNPX        RunnerKind = "npx"
	RunnerUVX        RunnerKind = "uvx"
	RunnerDocker     RunnerKind = "docker"
	RunnerHostNative RunnerKind = "host_native"
)

// Runner identifies the delegated invocation family.
type Runner struct {
	kind RunnerKind
}

// NewRunner validates and constructs a delegated invocation runner.
func NewRunner(kind RunnerKind) (Runner, error) {
	if err := validateRunnerKind(kind); err != nil {
		return Runner{}, err
	}
	return Runner{kind: kind}, nil
}

// Kind returns the runner kind.
func (runner Runner) Kind() RunnerKind { return runner.kind }

func validateRunnerKind(kind RunnerKind) error {
	switch kind {
	case RunnerPlain, RunnerNPX, RunnerUVX, RunnerDocker, RunnerHostNative:
		return nil
	default:
		return validationError(ReasonInvalidRunnerKind, string(kind), "unsupported runner kind")
	}
}

func (runner Runner) fixedCommand() (string, bool) {
	switch runner.kind {
	case RunnerNPX:
		return "npx", true
	case RunnerUVX:
		return "uvx", true
	case RunnerDocker:
		return "docker", true
	default:
		return "", false
	}
}

func (runner Runner) packageEcosystem() (PackageEcosystem, bool) {
	switch runner.kind {
	case RunnerNPX:
		return EcosystemNPM, true
	case RunnerUVX:
		return EcosystemPython, true
	case RunnerDocker:
		return EcosystemContainer, true
	default:
		return "", false
	}
}

// CommandSpec is the argv command identity for a delegated executable plan.
type CommandSpec struct {
	name string
	args []string
}

// NewCommandSpec validates and constructs a command specification.
func NewCommandSpec(name string, args []string) (CommandSpec, error) {
	if err := validateCommandName(name); err != nil {
		return CommandSpec{}, err
	}
	clonedArgs := append([]string(nil), args...)
	for index, arg := range clonedArgs {
		if err := validateArgument(arg); err != nil {
			return CommandSpec{}, validationError(ReasonInvalidArgument, argumentSubject(index), err.Error())
		}
	}
	return CommandSpec{name: name, args: clonedArgs}, nil
}

// Name returns the portable executable token.
func (command CommandSpec) Name() string { return command.name }

// Args returns the delegated argv tail.
func (command CommandSpec) Args() []string { return append([]string(nil), command.args...) }

// EnvBinding maps one child-process variable to one host environment source.
type EnvBinding struct {
	name       string
	sourceName string
}

// NewEnvBinding validates and constructs one environment binding.
func NewEnvBinding(name string, sourceName string) (EnvBinding, error) {
	if err := validateEnvRef(name); err != nil {
		return EnvBinding{}, err
	}
	if err := validateEnvRef(sourceName); err != nil {
		return EnvBinding{}, err
	}
	return EnvBinding{name: name, sourceName: sourceName}, nil
}

// Name returns the child-process environment variable name.
func (binding EnvBinding) Name() string { return binding.name }

// SourceName returns the host environment variable read at execution time.
func (binding EnvBinding) SourceName() string { return binding.sourceName }

// EnvBindingSet is a deterministic child-name-keyed environment binding set.
type EnvBindingSet struct {
	bindings []EnvBinding
}

// NewEnvBindingSet validates, deduplicates, and sorts environment bindings.
// One source may feed multiple child names, but one child name cannot select
// multiple sources.
func NewEnvBindingSet(bindings []EnvBinding) (EnvBindingSet, error) {
	byName := make(map[string]EnvBinding, len(bindings))
	for _, binding := range bindings {
		canonical, err := NewEnvBinding(binding.Name(), binding.SourceName())
		if err != nil {
			return EnvBindingSet{}, err
		}
		if existing, ok := byName[canonical.Name()]; ok {
			if existing.SourceName() != canonical.SourceName() {
				return EnvBindingSet{}, validationError(
					ReasonInvalidEnvRef,
					canonical.Name(),
					"child environment name maps to multiple host sources",
				)
			}
			continue
		}
		byName[canonical.Name()] = canonical
	}
	normalized := make([]EnvBinding, 0, len(byName))
	for _, binding := range byName {
		normalized = append(normalized, binding)
	}
	sort.Slice(normalized, func(left int, right int) bool {
		if normalized[left].Name() != normalized[right].Name() {
			return normalized[left].Name() < normalized[right].Name()
		}
		return normalized[left].SourceName() < normalized[right].SourceName()
	})
	return EnvBindingSet{bindings: normalized}, nil
}

// Bindings returns deterministic environment bindings.
func (set EnvBindingSet) Bindings() []EnvBinding {
	return append([]EnvBinding(nil), set.bindings...)
}

// SourceNames returns deterministic unique host environment source names.
func (set EnvBindingSet) SourceNames() []string {
	seen := make(map[string]struct{}, len(set.bindings))
	names := make([]string, 0, len(set.bindings))
	for _, binding := range set.bindings {
		if _, ok := seen[binding.SourceName()]; ok {
			continue
		}
		seen[binding.SourceName()] = struct{}{}
		names = append(names, binding.SourceName())
	}
	sort.Strings(names)
	return names
}

// PackageEcosystem identifies the package namespace used by a delegated runner.
type PackageEcosystem string

const (
	EcosystemNPM       PackageEcosystem = "npm"
	EcosystemPython    PackageEcosystem = "python"
	EcosystemContainer PackageEcosystem = "container"
)

// PackageRef is package-like identity required by a delegated runner.
type PackageRef struct {
	ecosystem PackageEcosystem
	name      string
	selector  string
}

// NewPackageRef validates and constructs package-like delegated identity.
func NewPackageRef(ecosystem PackageEcosystem, name string, selector string) (PackageRef, error) {
	if err := validatePackageName(ecosystem, name); err != nil {
		return PackageRef{}, err
	}
	if err := validatePackageSelector(selector); err != nil {
		return PackageRef{}, err
	}
	return PackageRef{ecosystem: ecosystem, name: name, selector: selector}, nil
}

// Ecosystem returns the package namespace.
func (ref PackageRef) Ecosystem() PackageEcosystem { return ref.ecosystem }

// Name returns the package name without version, tag, or digest selector.
func (ref PackageRef) Name() string { return ref.name }

// Selector returns the optional version, tag, digest, or range selector.
func (ref PackageRef) Selector() string { return ref.selector }

func validateCommandName(name string) error {
	if name == "" {
		return validationError(ReasonInvalidCommand, "command", "command name is required")
	}
	if strings.TrimSpace(name) != name {
		return validationError(ReasonInvalidCommand, name, "command name must not contain leading or trailing whitespace")
	}
	if strings.ContainsAny(name, `/\`) {
		return validationError(ReasonInvalidCommand, name, "command name must be a portable token, not a path")
	}
	if strings.Contains(name, ":") {
		return validationError(ReasonInvalidCommand, name, "command name must not encode a drive, URL, or route")
	}
	if hasSpaceControlOrShell(name) {
		return validationError(ReasonInvalidCommand, name, "command name contains whitespace, control, or shell metacharacters")
	}
	if strings.HasPrefix(name, ".") {
		return validationError(ReasonInvalidCommand, name, "command name must not be relative-path-like")
	}
	return nil
}

func validateArgument(arg string) error {
	if strings.IndexFunc(arg, unicode.IsControl) >= 0 {
		return validationError(ReasonInvalidArgument, arg, "argument contains a control character")
	}
	return nil
}

func argumentSubject(index int) string {
	return "args[" + strconv.Itoa(index) + "]"
}

func validateEnvRef(name string) error {
	if name == "" {
		return validationError(ReasonInvalidEnvRef, "env", "environment reference name is required")
	}
	if strings.TrimSpace(name) != name {
		return validationError(ReasonInvalidEnvRef, name, "environment reference name must not contain leading or trailing whitespace")
	}
	for index, value := range name {
		switch {
		case value >= 'A' && value <= 'Z':
		case value >= 'a' && value <= 'z':
		case value == '_':
		case value >= '0' && value <= '9' && index > 0:
		default:
			return validationError(ReasonInvalidEnvRef, name, "environment reference name must be ASCII letters, digits, or underscore and must not start with a digit")
		}
	}
	return nil
}

func validatePackageName(ecosystem PackageEcosystem, name string) error {
	if name == "" {
		return validationError(ReasonInvalidPackageRef, "package", "package name is required")
	}
	if strings.TrimSpace(name) != name {
		return validationError(ReasonInvalidPackageRef, name, "package name must not contain leading or trailing whitespace")
	}
	if hasSpaceControlOrShell(name) {
		return validationError(ReasonInvalidPackageRef, name, "package name contains whitespace, control, or shell metacharacters")
	}
	switch ecosystem {
	case EcosystemNPM:
		return validateNPMPackageName(name)
	case EcosystemPython:
		return validateSegmentedPackageName(EcosystemPython, name, false)
	case EcosystemContainer:
		return validateContainerPackageName(name)
	default:
		return validationError(ReasonInvalidPackageRef, string(ecosystem), "unsupported package ecosystem")
	}
}

func validatePackageSelector(selector string) error {
	if strings.TrimSpace(selector) != selector {
		return validationError(ReasonInvalidPackageRef, selector, "package selector must not contain leading or trailing whitespace")
	}
	if selector == "" {
		return nil
	}
	if hasSpaceControlOrShell(selector) {
		return validationError(ReasonInvalidPackageRef, selector, "package selector contains whitespace, control, or shell metacharacters")
	}
	if strings.Contains(selector, "/") || strings.Contains(selector, `\`) {
		return validationError(ReasonInvalidPackageRef, selector, "package selector must not be path-like")
	}
	return nil
}

func validateNPMPackageName(name string) error {
	if strings.HasPrefix(name, "@") {
		parts := strings.Split(name, "/")
		if len(parts) != 2 || parts[0] == "@" || parts[1] == "" {
			return validationError(ReasonInvalidPackageRef, name, "scoped npm package must be @scope/name")
		}
		if err := validatePackageSegment(parts[0][1:], false); err != nil {
			return validationError(ReasonInvalidPackageRef, name, "invalid npm scope")
		}
		if err := validatePackageSegment(parts[1], false); err != nil {
			return validationError(ReasonInvalidPackageRef, name, "invalid npm package name")
		}
		return nil
	}
	return validateSegmentedPackageName(EcosystemNPM, name, false)
}

func validateSegmentedPackageName(ecosystem PackageEcosystem, name string, allowSlash bool) error {
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, `\`) {
		return validationError(ReasonInvalidPackageRef, name, "package name must not be a local path")
	}
	parts := strings.Split(name, "/")
	if !allowSlash && len(parts) != 1 {
		return validationError(ReasonInvalidPackageRef, name, string(ecosystem)+" package name must not contain slash")
	}
	for _, part := range parts {
		if err := validatePackageSegment(part, false); err != nil {
			return err
		}
	}
	return nil
}

func validateContainerPackageName(name string) error {
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, `\`) {
		return validationError(ReasonInvalidPackageRef, name, "container image name must not be a local path")
	}
	parts := strings.Split(name, "/")
	for index, part := range parts {
		if err := validatePackageSegment(part, index == 0 && len(parts) > 1); err != nil {
			return err
		}
	}
	return nil
}

func validatePackageSegment(segment string, allowColon bool) error {
	if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
		return validationError(ReasonInvalidPackageRef, segment, "package name contains an invalid path segment")
	}
	for _, value := range segment {
		switch {
		case value >= 'A' && value <= 'Z':
		case value >= 'a' && value <= 'z':
		case value >= '0' && value <= '9':
		case value == '.', value == '_', value == '-':
		case value == ':' && allowColon:
		default:
			return validationError(ReasonInvalidPackageRef, segment, "package name contains an unsupported character")
		}
	}
	return nil
}

func hasSpaceControlOrShell(value string) bool {
	for _, char := range value {
		if char <= ' ' || char == 0x7f {
			return true
		}
		switch char {
		case '|', '&', ';', '<', '>', '(', ')', '$', '`', '"', '\'', '*', '?':
			return true
		}
	}
	return false
}
