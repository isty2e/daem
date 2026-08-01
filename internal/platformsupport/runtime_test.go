package platformsupport

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMacOSProductVersionAcceptsCanonicalProductVersions(t *testing.T) {
	for _, value := range []string{"10.15.7", "26.0", "26.5.1", "27.0", "4294967295.4294967295.4294967295"} {
		version, err := ParseMacOSProductVersion(value)
		if err != nil {
			t.Fatalf("ParseMacOSProductVersion(%q): %v", value, err)
		}
		if version.String() != value {
			t.Fatalf("ParseMacOSProductVersion(%q).String() = %q", value, version)
		}
	}
}

func TestParseMacOSProductVersionRejectsNonCanonicalInput(t *testing.T) {
	for _, value := range []string{
		"",
		"26",
		"26.",
		".0",
		"26.0.0.0",
		"026.0",
		"26.00",
		"26.a",
		"26.0\n",
		" 26.0",
		"4294967296.0",
		"26.4294967296",
	} {
		if _, err := ParseMacOSProductVersion(value); err == nil {
			t.Fatalf("ParseMacOSProductVersion(%q) succeeded", value)
		}
	}
}

func TestPlatformAssessmentRequiresExactMacOSRuntimeFloor(t *testing.T) {
	admission := mustLookupAdmission(t, "darwin", "arm64")
	tests := []struct {
		version  string
		admitted bool
	}{
		{version: "25.9.9"},
		{version: "26.0", admitted: true},
		{version: "26.0.1", admitted: true},
		{version: "27.0", admitted: true},
	}

	for _, test := range tests {
		version, err := ParseMacOSProductVersion(test.version)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := NewRuntimeObservation(version)
		if err != nil {
			t.Fatal(err)
		}
		assessment := AssessRuntime(admission, observation)
		if assessment.IsAdmitted() != test.admitted {
			t.Fatalf("macOS %s admitted = %t, want %t", test.version, assessment.IsAdmitted(), test.admitted)
		}
	}
}

func TestPlatformAssessmentReportsRuntimeEvidence(t *testing.T) {
	admission := mustLookupAdmission(t, "darwin", "arm64")
	version, err := ParseMacOSProductVersion("25.6")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := NewRuntimeObservation(version)
	if err != nil {
		t.Fatal(err)
	}
	err = AssessRuntime(admission, observation).RequireSupported()
	var unsupported *UnsupportedRuntimeError
	if !errors.As(err, &unsupported) {
		t.Fatalf("RequireSupported error = %T %v", err, err)
	}
	minimum := unsupported.MinimumVersion()
	observed, ok := unsupported.ObservedVersion()
	if minimum.String() != "26.0" || !ok || observed.String() != "25.6" {
		t.Fatalf("runtime error minimum=%s observed=%s,%t", minimum, observed, ok)
	}
	for _, want := range []string{"darwin/arm64", "macOS 26.0 or newer", "observed=25.6", "verification=native-required"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q", err, want)
		}
	}
}

func TestPlatformAssessmentFailsClosedForEveryObservationFailure(t *testing.T) {
	admission := mustLookupAdmission(t, "darwin", "arm64")
	for _, reason := range []RuntimeObservationReason{
		RuntimeObservationCommandFailed,
		RuntimeObservationInvalidOutput,
		RuntimeObservationTimedOut,
	} {
		observation, err := NewRuntimeObservationFailure(reason)
		if err != nil {
			t.Fatal(err)
		}
		err = AssessRuntime(admission, observation).RequireSupported()
		var unsupported *UnsupportedRuntimeError
		if !errors.As(err, &unsupported) || unsupported.ObservationReason() != reason {
			t.Fatalf("reason %s error = %T %v", reason, err, err)
		}
		if !strings.Contains(err.Error(), "reason="+reason.String()) {
			t.Fatalf("error = %q, want reason %s", err, reason)
		}
	}

	err := AssessRuntime(admission, RuntimeObservation{}).RequireSupported()
	var unsupported *UnsupportedRuntimeError
	if !errors.As(err, &unsupported) || unsupported.ObservationReason() != RuntimeObservationNotObserved {
		t.Fatalf("zero observation error = %T %v", err, err)
	}
}

func TestPlatformAssessmentKeepsRuntimeAndTargetAdmissionIndependent(t *testing.T) {
	linux := AssessRuntime(mustLookupAdmission(t, "linux", "amd64"), RuntimeObservation{})
	if !linux.IsAdmitted() {
		t.Fatalf("linux assessment error: %v", linux.RequireSupported())
	}
	if _, required := linux.RuntimeRequirement(); required {
		t.Fatal("linux unexpectedly requires macOS runtime evidence")
	}

	unsupportedTarget := AssessRuntime(mustLookupAdmission(t, "windows", "amd64"), RuntimeObservation{})
	err := unsupportedTarget.RequireSupported()
	var targetError *UnsupportedError
	var runtimeError *UnsupportedRuntimeError
	if !errors.As(err, &targetError) || errors.As(err, &runtimeError) {
		t.Fatalf("unsupported target error = %T %v", err, err)
	}

	var zero PlatformAssessment
	if zero.IsAdmitted() || zero.RequireSupported() == nil {
		t.Fatal("zero platform assessment passed admission")
	}
}

func TestRuntimeObservationConstructorsRejectInvalidStates(t *testing.T) {
	if _, err := NewRuntimeObservation(MacOSProductVersion{}); err == nil {
		t.Fatal("NewRuntimeObservation accepted zero version")
	}
	for _, reason := range []RuntimeObservationReason{RuntimeObservationNotObserved, RuntimeObservationReason(255)} {
		if _, err := NewRuntimeObservationFailure(reason); err == nil {
			t.Fatalf("NewRuntimeObservationFailure(%d) succeeded", reason)
		}
	}
}

func mustLookupAdmission(t *testing.T, goos string, goarch string) Admission {
	t.Helper()
	admission, err := Lookup(goos, goarch)
	if err != nil {
		t.Fatalf("Lookup(%q, %q): %v", goos, goarch, err)
	}
	return admission
}
