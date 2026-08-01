package platformsupport

import (
	"fmt"
	"strconv"
	"strings"
)

const minimumMacOSProductVersion = "26.0"

// MacOSProductVersion is one canonical sw_vers product version.
type MacOSProductVersion struct {
	major      uint32
	minor      uint32
	patch      uint32
	components uint8
}

// ParseMacOSProductVersion validates one canonical macOS product version.
func ParseMacOSProductVersion(value string) (MacOSProductVersion, error) {
	parts := strings.Split(value, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return MacOSProductVersion{}, fmt.Errorf("macOS product version %q must contain two or three decimal components", value)
	}

	components := make([]uint32, len(parts))
	for index, part := range parts {
		component, err := parseProductVersionComponent(part)
		if err != nil {
			return MacOSProductVersion{}, fmt.Errorf("macOS product version %q component %d: %w", value, index+1, err)
		}
		components[index] = component
	}
	if components[0] == 0 {
		return MacOSProductVersion{}, fmt.Errorf("macOS product version %q requires a positive major component", value)
	}

	version := MacOSProductVersion{
		major:      components[0],
		minor:      components[1],
		components: uint8(len(components)),
	}
	if len(components) == 3 {
		version.patch = components[2]
	}
	return version, nil
}

// String returns the canonical product-version spelling.
func (version MacOSProductVersion) String() string {
	if version.components == 0 {
		return "unknown"
	}
	value := fmt.Sprintf("%d.%d", version.major, version.minor)
	if version.components == 3 {
		value += fmt.Sprintf(".%d", version.patch)
	}
	return value
}

func (version MacOSProductVersion) atLeast(minimum MacOSProductVersion) bool {
	if version.major != minimum.major {
		return version.major > minimum.major
	}
	if version.minor != minimum.minor {
		return version.minor > minimum.minor
	}
	return version.patch >= minimum.patch
}

func parseProductVersionComponent(value string) (uint32, error) {
	if value == "" {
		return 0, fmt.Errorf("component is empty")
	}
	if len(value) > 1 && value[0] == '0' {
		return 0, fmt.Errorf("component is not canonical")
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("component must contain decimal digits only")
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("component is out of range")
	}
	return uint32(parsed), nil
}

// RuntimeObservationReason classifies why a required runtime version is not available.
type RuntimeObservationReason uint8

const (
	RuntimeObservationNotObserved RuntimeObservationReason = iota
	RuntimeObservationCommandFailed
	RuntimeObservationInvalidOutput
	RuntimeObservationTimedOut
)

// String returns the stable runtime-observation reason.
func (reason RuntimeObservationReason) String() string {
	switch reason {
	case RuntimeObservationCommandFailed:
		return "command-failed"
	case RuntimeObservationInvalidOutput:
		return "invalid-output"
	case RuntimeObservationTimedOut:
		return "timed-out"
	default:
		return "not-observed"
	}
}

// RuntimeObservation is either one valid product version or one typed failure.
type RuntimeObservation struct {
	version  MacOSProductVersion
	reason   RuntimeObservationReason
	observed bool
}

// NewRuntimeObservation records one valid observed product version.
func NewRuntimeObservation(version MacOSProductVersion) (RuntimeObservation, error) {
	if version.components == 0 {
		return RuntimeObservation{}, fmt.Errorf("observed macOS product version is required")
	}
	return RuntimeObservation{version: version, observed: true}, nil
}

// NewRuntimeObservationFailure records one classified observation failure.
func NewRuntimeObservationFailure(reason RuntimeObservationReason) (RuntimeObservation, error) {
	if reason != RuntimeObservationCommandFailed &&
		reason != RuntimeObservationInvalidOutput &&
		reason != RuntimeObservationTimedOut {
		return RuntimeObservation{}, fmt.Errorf("runtime observation failure reason %d is invalid", reason)
	}
	return RuntimeObservation{reason: reason}, nil
}

// Version returns the observed version when observation succeeded.
func (observation RuntimeObservation) Version() (MacOSProductVersion, bool) {
	return observation.version, observation.observed
}

// Reason returns why no version was observed.
func (observation RuntimeObservation) Reason() RuntimeObservationReason {
	if observation.observed {
		return RuntimeObservationNotObserved
	}
	return observation.reason
}

// PlatformAssessment combines static target admission with runtime evidence.
type PlatformAssessment struct {
	admission   Admission
	observation RuntimeObservation
}

// AssessRuntime constructs one complete platform assessment.
func AssessRuntime(admission Admission, observation RuntimeObservation) PlatformAssessment {
	return PlatformAssessment{admission: admission, observation: observation}
}

// TargetAdmission returns the static target admission fact.
func (assessment PlatformAssessment) TargetAdmission() Admission {
	return assessment.admission
}

// RuntimeObservation returns the runtime evidence used by this assessment.
func (assessment PlatformAssessment) RuntimeObservation() RuntimeObservation {
	return assessment.observation
}

// RuntimeRequirement returns the macOS floor when this target requires one.
func (assessment PlatformAssessment) RuntimeRequirement() (MacOSProductVersion, bool) {
	return assessment.admission.RuntimeRequirement()
}

// IsAdmitted reports whether both static target policy and runtime evidence pass.
func (assessment PlatformAssessment) IsAdmitted() bool {
	return assessment.RequireSupported() == nil
}

// RequireSupported rejects unsupported targets and unsatisfied runtime floors.
func (assessment PlatformAssessment) RequireSupported() error {
	if err := assessment.admission.RequireSupported(); err != nil {
		return err
	}
	minimum, required := assessment.RuntimeRequirement()
	if !required {
		return nil
	}
	observed, ok := assessment.observation.Version()
	if ok && observed.atLeast(minimum) {
		return nil
	}
	return &UnsupportedRuntimeError{assessment: assessment}
}

// UnsupportedRuntimeError reports one fail-closed runtime admission decision.
type UnsupportedRuntimeError struct {
	assessment PlatformAssessment
}

// Target returns the target whose runtime failed admission.
func (err *UnsupportedRuntimeError) Target() Target {
	if err == nil {
		return Target{}
	}
	return err.assessment.TargetAdmission().Target()
}

// MinimumVersion returns the required macOS product-version floor.
func (err *UnsupportedRuntimeError) MinimumVersion() MacOSProductVersion {
	if err == nil {
		return MacOSProductVersion{}
	}
	minimum, _ := err.assessment.RuntimeRequirement()
	return minimum
}

// ObservedVersion returns the product version when one was observed.
func (err *UnsupportedRuntimeError) ObservedVersion() (MacOSProductVersion, bool) {
	if err == nil {
		return MacOSProductVersion{}, false
	}
	return err.assessment.RuntimeObservation().Version()
}

// ObservationReason returns why the runtime version was unavailable.
func (err *UnsupportedRuntimeError) ObservationReason() RuntimeObservationReason {
	if err == nil {
		return RuntimeObservationNotObserved
	}
	return err.assessment.RuntimeObservation().Reason()
}

func (err *UnsupportedRuntimeError) Error() string {
	if err == nil {
		return "platform runtime admission failed"
	}
	verification := err.assessment.TargetAdmission().Verification()
	if observed, ok := err.ObservedVersion(); ok {
		return fmt.Sprintf(
			"platform %s requires macOS %s or newer (observed=%s; verification=%s)",
			err.Target(),
			err.MinimumVersion(),
			observed,
			verification,
		)
	}
	return fmt.Sprintf(
		"platform %s requires macOS %s or newer but its product version was not available (reason=%s; verification=%s)",
		err.Target(),
		err.MinimumVersion(),
		err.ObservationReason(),
		verification,
	)
}

// RuntimeRequirement returns the runtime floor separate from target support.
func (admission Admission) RuntimeRequirement() (MacOSProductVersion, bool) {
	if admission.Target() != (Target{goos: "darwin", goarch: "arm64"}) {
		return MacOSProductVersion{}, false
	}
	minimum, err := ParseMacOSProductVersion(minimumMacOSProductVersion)
	if err != nil {
		panic(err)
	}
	return minimum, true
}
