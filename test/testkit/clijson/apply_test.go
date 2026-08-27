package clijson

import (
	"fmt"
	"testing"
)

func TestDecodeApplyResultAcceptsSchema19RecoveryBarriers(t *testing.T) {
	for _, test := range []struct {
		name    string
		barrier string
		journal string
		fileSet string
	}{
		{name: "active", barrier: `{"journal":"active_apply"}`, journal: "active_apply"},
		{name: "cleanup", barrier: `{"journal":"cleanup_only"}`, journal: "cleanup_only"},
		{name: "partial axis", barrier: `{"journal":"unknown","file_set":"abandoned_residue"}`, journal: "unknown", fileSet: "abandoned_residue"},
		{name: "file set only", barrier: `{"file_set":"access_unprovable"}`, fileSet: "access_unprovable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := DecodeApplyResult(t, []byte(fmt.Sprintf(`{
  "schema_version": 19,
  "command": "apply",
  "mode": "write",
  "action_count": 0,
  "statefile_path": "",
  "lock_only": {"skills": null, "hooks": null},
  "actions": [],
  "delegate_actions": [],
  "relation_actions": [],
  "relation_order_actions": [],
  "relation_order_results": [],
  "carrier_adoption_actions": [],
  "carrier_absence_actions": [],
  "delegate_attempts": [],
  "host_route_attempts": [],
  "mcp_statuses": [],
  "diagnostics": [],
  "has_errors": true,
  "errors": [{
    "code": "interrupted_apply_file_set_fence",
    "phase": "preflight",
    "outcome": "refused",
    "message": "recovery barrier",
    "recovery_barrier": %s
  }]
}`, test.barrier)))
			if len(payload.Errors) != 1 || payload.Errors[0].RecoveryBarrier == nil {
				t.Fatalf("errors = %#v", payload.Errors)
			}
			barrier := payload.Errors[0].RecoveryBarrier
			if barrier.Journal != test.journal || barrier.FileSet != test.fileSet {
				t.Fatalf("recovery_barrier = %#v", barrier)
			}
		})
	}
}
