package clipresent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestPrintDoctorChecksCountsSkippedAndUnsupported(t *testing.T) {
	var output bytes.Buffer
	PrintDoctorChecksWithOptions(&output, []findings.Check{
		findings.OKCheck("platform", "admitted"),
		findings.WarnCheck("manifest", "missing"),
		findings.ErrorCheck("git", "missing"),
		findings.SkippedCheck("skill_observation", "not attempted"),
		findings.UnsupportedCheck("cache", "capability"),
	}, HumanOptions{})
	text := output.String()
	if !strings.Contains(text, "doctor: 5 checks (ok=1 warn=1 error=1 skipped=1 unsupported=1)") {
		t.Fatalf("totals = %q", text)
	}
	if strings.Contains(text, "\nok platform") {
		t.Fatalf("default output leaked ok rows: %q", text)
	}
	for _, want := range []string{
		"warn manifest",
		"error git",
		"skipped skill_observation",
		"unsupported cache",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output = %q, want %q", text, want)
		}
	}
}

func TestPrintDoctorJSONUsesStatusAndSchemaTwo(t *testing.T) {
	selection, err := targetselection.ForDiagnostics([]string{string(target.TargetCodex)})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := PrintDoctorJSON(&output, DoctorJSONInput{
		ManifestPath:     "/tmp/daem.toml",
		ManifestExplicit: true,
		Selection:        selection,
		Checks: []findings.Check{
			findings.ErrorCheck("platform", "windows/amd64"),
			findings.UnsupportedCheck("file_set", "durable file-set inventory cannot be honored on this platform"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["schema_version"] != float64(contractversion.DoctorJSON) {
		t.Fatalf("schema_version = %#v, want %d", payload["schema_version"], contractversion.DoctorJSON)
	}
	checks, ok := payload["checks"].([]any)
	if !ok || len(checks) != 2 {
		t.Fatalf("checks = %#v", payload["checks"])
	}
	first, ok := checks[0].(map[string]any)
	if !ok {
		t.Fatalf("first check = %#v", checks[0])
	}
	if _, found := first["severity"]; found {
		t.Fatalf("doctor check retained severity: %#v", first)
	}
	if first["status"] != "error" || first["name"] != "platform" {
		t.Fatalf("first check = %#v", first)
	}
	second, ok := checks[1].(map[string]any)
	if !ok || second["status"] != "unsupported" || second["name"] != "file_set" {
		t.Fatalf("second check = %#v", checks[1])
	}
}
