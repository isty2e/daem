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
	check := PlatformCheck(admission)
	if check.Severity != findings.SeverityOK || check.Name != "platform" {
		t.Fatalf("check = %#v", check)
	}
	if check.Detail != "darwin/arm64 is an admitted product target (verification=native-required)" {
		t.Fatalf("detail = %q", check.Detail)
	}
}

func TestPlatformCheckReportsCompileOnlyTargetAsError(t *testing.T) {
	admission, err := platformsupport.Lookup("windows", "amd64")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	check := PlatformCheck(admission)
	if check.Severity != findings.SeverityError || check.Name != "platform" {
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
