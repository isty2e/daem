//go:build darwin || linux

package clipresent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/test/ownershiptest"
)

func TestProtectionProjectionPreservesFailureAnnotationsAndOmitsPrivateEvidence(t *testing.T) {
	fixture := ownershiptest.PermissionRecovery(t, ".codex/config.toml")
	observation := ownershipobserve.ObservePendingProtections(t.Context(), fixture.Paths)
	diagnostic := NewFailureDiagnostic("source invalid").WithPermissionProtection(observation).WithPrefix("status failed: ").WithGuidance(InstructionGuidance("inspect source"))
	var output bytes.Buffer
	if err := PrintCLIErrorJSON(&output, "status", CLIErrorOperation, diagnostic); err != nil {
		t.Fatal(err)
	}
	var value struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Protection *permissionProtectionJSON `json:"permission_protection"`
		Guidance   []cliErrorJSONGuidance    `json:"guidance"`
	}
	if err := json.Unmarshal(output.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Error.Message != "status failed: source invalid" || value.Protection == nil || len(value.Protection.Records) != 1 || len(value.Guidance) != 1 {
		t.Fatalf("lost orthogonal failure facts: %+v", value)
	}
	for _, private := range []string{fixture.Owner.Key().String(), "permission-recover-test", strings.Repeat("b", 64), strings.Repeat("c", 64), "malformed host payload"} {
		if strings.Contains(output.String(), private) {
			t.Fatal("projection disclosed private namespace, correlation or host evidence")
		}
	}
}

func TestKnownClearProtectionDoesNotChangeExistingJSONProducers(t *testing.T) {
	clear, err := ownershipobserve.KnownProtections(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, producer := range []string{"failure", "plan", "apply", "doctor"} {
		t.Run(producer, func(t *testing.T) {
			var before, after bytes.Buffer
			for index, observation := range []ownershipobserve.ProtectionObservation{{}, clear} {
				output := &before
				if index == 1 {
					output = &after
				}
				var err error
				switch producer {
				case "failure":
					err = PrintCLIErrorJSON(output, "apply", CLIErrorOperation, NewFailureDiagnostic("source invalid").WithPermissionProtection(observation))
				case "plan":
					err = PrintPlanJSON(output, PlanJSONInput{Command: "apply", Mode: "dry-run", PermissionProtection: observation})
				case "apply":
					err = PrintApplyResultJSON(output, ApplyResultJSONInput{PermissionProtection: observation})
				case "doctor":
					err = PrintDoctorJSON(output, DoctorJSONInput{PermissionProtection: observation})
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(before.Bytes(), after.Bytes()) {
				t.Fatalf("known-clear observation changed %s output", producer)
			}
		})
	}
}
