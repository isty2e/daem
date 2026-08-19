package diagnose

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/platformsupport"
)

func TestPlatformCheckReportsAdmittedTarget(t *testing.T) {
	admission, err := platformsupport.Lookup("darwin", "arm64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	version, err := platformsupport.ParseMacOSProductVersion("26.5.1")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := platformsupport.NewRuntimeObservation(version)
	if err != nil {
		t.Fatal(err)
	}
	check := PlatformCheck(platformsupport.AssessRuntime(admission, observation))
	if check.Status != findings.CheckOK || check.Name != "platform" {
		t.Fatalf("check = %#v", check)
	}
	if check.Detail != "darwin/arm64 is an admitted product target (runtime=macOS 26.5.1; required>=26.0; verification=native-required)" {
		t.Fatalf("detail = %q", check.Detail)
	}
}

func TestPlatformCheckReportsCompileOnlyTargetAsError(t *testing.T) {
	admission, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	check := PlatformCheck(platformsupport.AssessRuntime(admission, platformsupport.RuntimeObservation{}))
	if check.Status != findings.CheckError || check.Name != "platform" {
		t.Fatalf("check = %#v", check)
	}
	for _, want := range []string{
		"platform windows/amd64",
		"verification=compile-only",
		"admitted=darwin/arm64,linux/amd64",
	} {
		if !strings.Contains(check.Detail, want) {
			t.Fatalf("detail = %q, want %q", check.Detail, want)
		}
	}
	if check.NextStep != "run daem on an admitted platform: darwin/arm64, linux/amd64" {
		t.Fatalf("next step = %q", check.NextStep)
	}
}

func TestPlatformCheckReportsMacOSRuntimeRemediation(t *testing.T) {
	admission, err := platformsupport.Lookup("darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}

	version, err := platformsupport.ParseMacOSProductVersion("25.9")
	if err != nil {
		t.Fatal(err)
	}
	observation, err := platformsupport.NewRuntimeObservation(version)
	if err != nil {
		t.Fatal(err)
	}
	belowFloor := PlatformCheck(platformsupport.AssessRuntime(admission, observation))
	if belowFloor.Status != findings.CheckError || belowFloor.NextStep != "upgrade macOS to 26.0 or newer" {
		t.Fatalf("below-floor check = %#v", belowFloor)
	}

	observation, err = platformsupport.NewRuntimeObservationFailure(platformsupport.RuntimeObservationInvalidOutput)
	if err != nil {
		t.Fatal(err)
	}
	unknown := PlatformCheck(platformsupport.AssessRuntime(admission, observation))
	if unknown.Status != findings.CheckError || unknown.NextStep != "verify /usr/bin/sw_vers --productVersion, then rerun daem doctor" {
		t.Fatalf("unknown-runtime check = %#v", unknown)
	}
}
