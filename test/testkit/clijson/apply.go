package clijson

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
)

type ApplyResult struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Mode          string `json:"mode"`
	ActionCount   int    `json:"action_count"`
	StatefilePath string `json:"statefile_path"`
	LockOnly      struct {
		Skills []struct {
			Kind    string
			Name    string
			Targets []string
		}
		Hooks []struct {
			Kind    string
			Name    string
			Targets []string
		}
	} `json:"lock_only"`
	Actions []struct {
		Kind    string `json:"kind"`
		Reason  string `json:"reason"`
		Subject *struct {
			Kind      string `json:"kind"`
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"subject"`
		ResourceID string `json:"resource_id"`
		Resource   struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"resource"`
		Projection *struct {
			Target      string `json:"target"`
			Scope       string `json:"scope"`
			ConfigPath  string `json:"config_path"`
			ContentPath string `json:"content_path"`
		} `json:"projection"`
		Target           string   `json:"target"`
		Targets          []string `json:"targets"`
		Scope            string   `json:"scope"`
		Destination      string   `json:"destination"`
		ContentPath      string   `json:"content_path"`
		PlacementMode    string   `json:"placement_mode"`
		ContentKind      string   `json:"content_kind"`
		PermissionPolicy string   `json:"permission_policy"`
		DesiredFileMode  uint32   `json:"desired_file_mode"`
		LiveFileMode     uint32   `json:"live_file_mode"`
		DesiredHash      string   `json:"desired_hash"`
		LiveHash         string   `json:"live_hash"`
		StateHash        string   `json:"state_hash"`
		Detail           string   `json:"detail"`
		Safety           string   `json:"safety"`
		PreviousState    *struct {
			Subject *struct {
				Kind      string `json:"kind"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"subject"`
			ResourceID string `json:"resource_id"`
			Resource   struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"resource"`
			Target           string   `json:"target"`
			Targets          []string `json:"targets"`
			Scope            string   `json:"scope"`
			Destination      string   `json:"destination"`
			ContentPath      string   `json:"content_path"`
			ContentHash      string   `json:"content_hash"`
			ContentKind      string   `json:"content_kind"`
			PermissionPolicy string   `json:"permission_policy"`
			FileMode         uint32   `json:"file_mode"`
		} `json:"previous_state"`
	} `json:"actions"`
	DelegateActions      []DelegateAction        `json:"delegate_actions"`
	RelationActions      []RelationAction        `json:"relation_actions"`
	RelationOrders       []RelationOrderAction   `json:"relation_order_actions"`
	RelationOrderResults []RelationOrderResult   `json:"relation_order_results"`
	CarrierAdoptions     []CarrierAdoptionAction `json:"carrier_adoption_actions"`
	CarrierAbsences      []CarrierAbsenceAction  `json:"carrier_absence_actions"`
	DelegateAttempts     []DelegateAttempt       `json:"delegate_attempts"`
	HostRouteAttempts    []HostRouteAttempt      `json:"host_route_attempts"`
	MCPStatuses          []MCPStatus             `json:"mcp_statuses"`
	Diagnostics          []struct {
		Severity   string `json:"severity"`
		Code       string `json:"code"`
		ResourceID string `json:"resource_id"`
		Resource   struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"resource"`
		Target        string   `json:"target"`
		Scope         string   `json:"scope"`
		Event         string   `json:"event"`
		Command       string   `json:"command"`
		Detail        string   `json:"detail"`
		Repairability string   `json:"repairability"`
		RepairActions []string `json:"repair_actions"`
		ManualReasons []string `json:"manual_reasons"`
		NextStep      string   `json:"next_step"`
	} `json:"diagnostics"`
	HasErrors bool `json:"has_errors"`
	Errors    []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func DecodeApplyResult(t *testing.T, content []byte) ApplyResult {
	t.Helper()

	var payload ApplyResult
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("Decode returned error: %v\ncontent=%s", err, content)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("unexpected trailing JSON content: %s", content)
	}
	requireSchemaVersion(t, "apply result", payload.SchemaVersion, contractversion.ApplyResultJSON)

	return payload
}
