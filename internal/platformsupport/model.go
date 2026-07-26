// Package platformsupport owns daem product-platform admission policy.
package platformsupport

import (
	"fmt"
	"runtime"
	"strings"
)

// Support records whether daem admits a platform as a supported product target.
type Support uint8

const (
	SupportNotAdmitted Support = iota
	SupportAdmitted
)

// String returns the stable support label.
func (support Support) String() string {
	if support == SupportAdmitted {
		return "admitted"
	}
	return "not-admitted"
}

// Verification records the repository verification lane assigned to a platform.
// It does not claim that a particular CI run has passed.
type Verification uint8

const (
	VerificationUnverified Verification = iota
	VerificationCompileOnly
	VerificationNativeRequired
)

// String returns the stable verification label.
func (verification Verification) String() string {
	switch verification {
	case VerificationCompileOnly:
		return "compile-only"
	case VerificationNativeRequired:
		return "native-required"
	default:
		return "unverified"
	}
}

// Target is one canonical GOOS/GOARCH product-platform identity.
type Target struct {
	goos   string
	goarch string
}

// OS returns the target GOOS.
func (target Target) OS() string { return target.goos }

// Arch returns the target GOARCH.
func (target Target) Arch() string { return target.goarch }

// String returns the canonical GOOS/GOARCH identity.
func (target Target) String() string {
	goos := target.goos
	if goos == "" {
		goos = "unknown"
	}
	goarch := target.goarch
	if goarch == "" {
		goarch = "unknown"
	}
	return goos + "/" + goarch
}

// Admission combines independent product-support and verification-lane facts.
type Admission struct {
	target       Target
	support      Support
	verification Verification
}

// Target returns the admitted or rejected platform identity.
func (admission Admission) Target() Target { return admission.target }

// Support returns the product-support decision.
func (admission Admission) Support() Support { return admission.support }

// Verification returns the assigned repository verification lane.
func (admission Admission) Verification() Verification { return admission.verification }

// IsAdmitted reports whether the platform may run supported product workflows.
func (admission Admission) IsAdmitted() bool {
	return admission.support == SupportAdmitted
}

// RequireSupported rejects a platform that is not admitted by product policy.
func (admission Admission) RequireSupported() error {
	if admission.IsAdmitted() {
		return nil
	}
	return &UnsupportedError{admission: admission}
}

// UnsupportedError reports one fail-closed platform admission decision.
type UnsupportedError struct {
	admission Admission
}

// Target returns the rejected platform identity.
func (err *UnsupportedError) Target() Target {
	if err == nil {
		return Target{}
	}
	return err.admission.Target()
}

// Verification returns the rejected platform's verification lane.
func (err *UnsupportedError) Verification() Verification {
	if err == nil {
		return VerificationUnverified
	}
	return err.admission.Verification()
}

func (err *UnsupportedError) Error() string {
	if err == nil {
		return "platform support admission failed"
	}
	return fmt.Sprintf(
		"platform %s is not an admitted daem product target (verification=%s; admitted=%s)",
		err.Target(),
		err.Verification(),
		strings.Join(supportedTargetNames(), ","),
	)
}

var admissionRows = mustAdmissionCatalog(
	mustAdmission("darwin", "arm64", SupportAdmitted, VerificationNativeRequired),
	mustAdmission("linux", "amd64", SupportAdmitted, VerificationNativeRequired),
	mustAdmission("darwin", "amd64", SupportNotAdmitted, VerificationCompileOnly),
	mustAdmission("linux", "arm64", SupportNotAdmitted, VerificationCompileOnly),
	mustAdmission("linux", "386", SupportNotAdmitted, VerificationCompileOnly),
	mustAdmission("windows", "amd64", SupportNotAdmitted, VerificationCompileOnly),
)

// Current returns the admission decision for the running binary.
func Current() Admission {
	admission, err := Lookup(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		panic(fmt.Sprintf("invalid Go runtime platform: %v", err))
	}
	return admission
}

// Lookup returns the admission decision for one canonical GOOS/GOARCH identity.
func Lookup(goos string, goarch string) (Admission, error) {
	target, err := ParseTarget(goos, goarch)
	if err != nil {
		return Admission{}, err
	}
	for _, admission := range admissionRows {
		if admission.target == target {
			return admission, nil
		}
	}
	return Admission{
		target:       target,
		support:      SupportNotAdmitted,
		verification: VerificationUnverified,
	}, nil
}

// ParseTarget validates and returns one canonical GOOS/GOARCH identity without
// making a product-support decision.
func ParseTarget(goos string, goarch string) (Target, error) {
	return newTarget(goos, goarch)
}

// AdmittedTargets returns the deterministic set of supported product targets.
func AdmittedTargets() []Target {
	targets := make([]Target, 0, len(admissionRows))
	for _, admission := range admissionRows {
		if admission.IsAdmitted() {
			targets = append(targets, admission.Target())
		}
	}
	return targets
}

func mustAdmission(goos string, goarch string, support Support, verification Verification) Admission {
	admission := Admission{
		target:       Target{goos: goos, goarch: goarch},
		support:      support,
		verification: verification,
	}
	if err := admission.validate(); err != nil {
		panic(err)
	}
	return admission
}

func mustAdmissionCatalog(rows ...Admission) []Admission {
	seen := make(map[Target]struct{}, len(rows))
	hasAdmittedTarget := false
	for _, admission := range rows {
		if err := admission.validate(); err != nil {
			panic(err)
		}
		if _, exists := seen[admission.Target()]; exists {
			panic(fmt.Sprintf("duplicate platform admission row %s", admission.Target()))
		}
		seen[admission.Target()] = struct{}{}
		hasAdmittedTarget = hasAdmittedTarget || admission.IsAdmitted()
	}
	if !hasAdmittedTarget {
		panic("platform admission catalog requires at least one admitted target")
	}
	return append([]Admission(nil), rows...)
}

func (admission Admission) validate() error {
	if _, err := newTarget(admission.target.goos, admission.target.goarch); err != nil {
		return fmt.Errorf("invalid platform admission target: %w", err)
	}
	if admission.support != SupportNotAdmitted && admission.support != SupportAdmitted {
		return fmt.Errorf("platform %s has invalid product support %d", admission.Target(), admission.support)
	}
	if admission.verification != VerificationUnverified && admission.verification != VerificationCompileOnly && admission.verification != VerificationNativeRequired {
		return fmt.Errorf("platform %s has invalid verification lane %d", admission.Target(), admission.verification)
	}
	if admission.support == SupportAdmitted && admission.verification != VerificationNativeRequired {
		return fmt.Errorf("admitted platform %s requires a native verification lane", admission.Target())
	}
	return nil
}

func newTarget(goos string, goarch string) (Target, error) {
	if err := validateTargetComponent("GOOS", goos); err != nil {
		return Target{}, err
	}
	if err := validateTargetComponent("GOARCH", goarch); err != nil {
		return Target{}, err
	}
	return Target{goos: goos, goarch: goarch}, nil
}

func validateTargetComponent(name string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.TrimSpace(value) != value || strings.ToLower(value) != value {
		return fmt.Errorf("%s must be a canonical lowercase Go platform token: %q", name, value)
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return fmt.Errorf("%s must be a canonical lowercase Go platform token: %q", name, value)
		}
	}
	return nil
}

func supportedTargetNames() []string {
	targets := AdmittedTargets()
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.String())
	}
	return names
}
