package clijson

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

type MCPStatusDimension struct {
	Dimension string `json:"dimension"`
	State     string `json:"state"`
	Reason    string `json:"reason"`
}

type MCPStatus struct {
	Subject struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	Target                 string               `json:"target"`
	Scope                  string               `json:"scope"`
	ConfigPath             string               `json:"config_path"`
	ContentPath            string               `json:"content_path"`
	AdapterContractVersion string               `json:"adapter_contract_version"`
	Projection             []MCPStatusDimension `json:"projection_dimensions"`
	Host                   []MCPStatusDimension `json:"host_dimensions"`
	Delegate               []MCPStatusDimension `json:"delegate_dimensions"`
	Runtime                []MCPStatusDimension `json:"runtime_dimensions"`
	Residue                []MCPStatusDimension `json:"residue_dimensions"`
	Other                  []MCPStatusDimension `json:"other_dimensions"`
}

func (status MCPStatus) Dimensions() []MCPStatusDimension {
	dimensions := make([]MCPStatusDimension, 0)
	dimensions = append(dimensions, status.Projection...)
	dimensions = append(dimensions, status.Host...)
	dimensions = append(dimensions, status.Delegate...)
	dimensions = append(dimensions, status.Runtime...)
	dimensions = append(dimensions, status.Residue...)
	dimensions = append(dimensions, status.Other...)
	return dimensions
}

type Plan struct {
	SchemaVersion int `json:"schema_version"`
	Command       string
	Mode          string
	Lockfile      struct {
		Path    string
		Missing bool
	}
	LockOnly struct {
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
	ActionCount int  `json:"action_count"`
	HasErrors   bool `json:"has_errors"`
	Actions     []struct {
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
	DelegateActions   []DelegateAction        `json:"delegate_actions"`
	RelationActions   []RelationAction        `json:"relation_actions"`
	CarrierAdoptions  []CarrierAdoptionAction `json:"carrier_adoption_actions"`
	CarrierAbsences   []CarrierAbsenceAction  `json:"carrier_absence_actions"`
	HostRouteAttempts []HostRouteAttempt      `json:"host_route_attempts"`
	MCPStatuses       []MCPStatus             `json:"mcp_statuses"`
	Diagnostics       []struct {
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
}
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
	DelegateActions   []DelegateAction        `json:"delegate_actions"`
	RelationActions   []RelationAction        `json:"relation_actions"`
	CarrierAdoptions  []CarrierAdoptionAction `json:"carrier_adoption_actions"`
	CarrierAbsences   []CarrierAbsenceAction  `json:"carrier_absence_actions"`
	DelegateAttempts  []DelegateAttempt       `json:"delegate_attempts"`
	HostRouteAttempts []HostRouteAttempt      `json:"host_route_attempts"`
	MCPStatuses       []MCPStatus             `json:"mcp_statuses"`
	Diagnostics       []struct {
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

type RelationAction struct {
	Kind    string `json:"kind"`
	Subject *struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	Target                    string   `json:"target"`
	Scope                     string   `json:"scope"`
	SourceNamespace           string   `json:"source_namespace"`
	SourceKind                string   `json:"source_kind"`
	SourceRef                 string   `json:"source_ref"`
	RelationSubjectKey        string   `json:"relation_subject_key"`
	EvidenceSource            string   `json:"evidence_source"`
	EvidenceAvailability      string   `json:"evidence_availability"`
	EvidenceFreshness         string   `json:"evidence_freshness"`
	RouteID                   string   `json:"route_id"`
	RouteRequestHash          string   `json:"route_request_hash"`
	RouteAdmissionRow         string   `json:"route_admission_row"`
	RequestedOutcome          string   `json:"requested_outcome"`
	SelectedOutcome           string   `json:"selected_outcome"`
	CorrelationState          string   `json:"correlation_state"`
	CorrelationReason         string   `json:"correlation_reason"`
	Reason                    string   `json:"reason"`
	Execution                 string   `json:"execution"`
	Watchpoints               []string `json:"watchpoints"`
	ReplayBoundary            string   `json:"replay_boundary"`
	RetainedEffects           []string `json:"retained_effects"`
	NonClaims                 []string `json:"non_claims"`
	InvokesHostRoute          bool     `json:"invokes_host_route"`
	AllowsHostRouteInvocation bool     `json:"allows_host_route_invocation"`
	BlocksOrdinaryApply       bool     `json:"blocks_ordinary_apply"`
}

type CarrierAdoptionAction struct {
	Kind    string `json:"kind"`
	Subject *struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	CarrierSubject *struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"carrier_subject"`
	Target                   string   `json:"target"`
	Scope                    string   `json:"scope"`
	SourceNamespace          string   `json:"source_namespace"`
	RelationSubjectKey       string   `json:"relation_subject_key"`
	Result                   string   `json:"result"`
	CorrelationState         string   `json:"correlation_state"`
	CorrelationReason        string   `json:"correlation_reason"`
	EvidenceAvailability     string   `json:"evidence_availability"`
	EvidenceFreshness        string   `json:"evidence_freshness"`
	ClaimOwner               string   `json:"claim_owner"`
	ClaimStore               string   `json:"claim_store"`
	CurrentClaimProvenance   string   `json:"current_claim_provenance"`
	ProposedClaimProvenance  string   `json:"proposed_claim_provenance"`
	ClaimTransition          string   `json:"claim_transition"`
	LifecycleEligible        bool     `json:"lifecycle_eligible"`
	LifecycleBlocker         string   `json:"lifecycle_blocker"`
	DaemKnownConsumerCount   int      `json:"daem_known_consumer_count"`
	ConflictingClaimCount    int      `json:"conflicting_claim_count"`
	InstallRouteStatus       string   `json:"install_route_status"`
	InstallRouteID           string   `json:"install_route_id"`
	InstallRouteRequestHash  string   `json:"install_route_request_hash"`
	RemovalRouteStatus       string   `json:"removal_route_status"`
	RemovalRouteID           string   `json:"removal_route_id"`
	RemovalRouteRequestHash  string   `json:"removal_route_request_hash"`
	RemovalActuation         string   `json:"removal_actuation"`
	LaterOmission            string   `json:"later_omission"`
	PreservesSharedCarrier   bool     `json:"preserves_shared_carrier"`
	RemovedEffects           []string `json:"removed_effects"`
	RetainedEffects          []string `json:"retained_effects"`
	NonClaims                []string `json:"non_claims"`
	AmbientConsumerAssurance string   `json:"ambient_consumer_assurance"`
	ManageExisting           bool     `json:"manage_existing"`
	InvokesHostRoute         bool     `json:"invokes_host_route"`
	StateOnly                bool     `json:"state_only"`
	BlocksOrdinaryApply      bool     `json:"blocks_ordinary_apply"`
}

type CarrierAbsenceAction struct {
	Kind    string `json:"kind"`
	Subject *struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	CarrierSubject *struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"carrier_subject"`
	Target                      string   `json:"target"`
	Scope                       string   `json:"scope"`
	SourceNamespace             string   `json:"source_namespace"`
	RequestedOutcome            string   `json:"requested_outcome"`
	SelectedAction              string   `json:"selected_action"`
	Execution                   string   `json:"execution"`
	CorrelationState            string   `json:"correlation_state"`
	CorrelationReason           string   `json:"correlation_reason"`
	EvidenceAvailability        string   `json:"evidence_availability"`
	EvidenceFreshness           string   `json:"evidence_freshness"`
	DaemKnownConsumerCount      int      `json:"daem_known_consumer_count"`
	RemainingDaemKnownConsumers int      `json:"remaining_daem_known_consumers"`
	RouteID                     string   `json:"route_id"`
	RouteRequestHash            string   `json:"route_request_hash"`
	PostconditionVerification   string   `json:"postcondition_verification"`
	RecoveryContract            string   `json:"recovery_contract"`
	RemovedEffects              []string `json:"removed_effects"`
	RetainedEffects             []string `json:"retained_effects"`
	NonClaims                   []string `json:"non_claims"`
	InvokesHostRoute            bool     `json:"invokes_host_route"`
	RetiresClaim                bool     `json:"retires_claim"`
	StateOnly                   bool     `json:"state_only"`
	BlocksOrdinaryApply         bool     `json:"blocks_ordinary_apply"`
}

type DelegateEnvBinding struct {
	Name       string `json:"name"`
	SourceName string `json:"source_name"`
}

type DelegateAction struct {
	Subject struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	Target           string               `json:"target"`
	Scope            string               `json:"scope"`
	Status           string               `json:"status"`
	PolicyOutcome    string               `json:"policy_outcome"`
	SchedulesAttempt bool                 `json:"schedules_attempt"`
	PlanIdentityKey  string               `json:"plan_identity_key"`
	RunnerKind       string               `json:"runner_kind"`
	Command          string               `json:"command"`
	Args             []string             `json:"args"`
	EnvBindings      []DelegateEnvBinding `json:"env_bindings"`
	Environment      string               `json:"environment"`
	Package          *struct {
		Ecosystem string `json:"ecosystem"`
		Name      string `json:"name"`
		Selector  string `json:"selector"`
	} `json:"package"`
	PinPolicy      string `json:"pin_policy"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Risks          []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		Subject  string `json:"subject"`
	} `json:"risks"`
	Dependencies []struct {
		Kind    string `json:"kind"`
		Subject struct {
			Kind      string `json:"kind"`
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"subject"`
	} `json:"dependencies"`
}

type DelegateAttempt struct {
	EvidenceKind string `json:"evidence_kind"`
	Authority    string `json:"authority"`
	Subject      struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	Target              string `json:"target"`
	Scope               string `json:"scope"`
	PlanIdentityKey     string `json:"plan_identity_key"`
	Status              string `json:"status"`
	Reason              string `json:"reason"`
	Observation         string `json:"observation"`
	Postcondition       string `json:"postcondition"`
	ExitCode            *int   `json:"exit_code"`
	TimedOut            bool   `json:"timed_out"`
	OutputObserved      bool   `json:"output_observed"`
	OutputTruncated     bool   `json:"output_truncated"`
	RunnerErrorObserved bool   `json:"runner_error_observed"`
	Redacted            bool   `json:"redacted"`
}

type HostRouteAttempt struct {
	EvidenceKind string `json:"evidence_kind"`
	Authority    string `json:"authority"`
	Subject      struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	Target                   string                       `json:"target"`
	Scope                    string                       `json:"scope"`
	Operation                string                       `json:"operation"`
	RouteID                  string                       `json:"route_id"`
	RouteRequestHash         string                       `json:"route_request_hash"`
	ObservedAt               string                       `json:"observed_at"`
	ResultClass              string                       `json:"result_class"`
	Reason                   string                       `json:"reason"`
	AttemptObserved          bool                         `json:"attempt_observed"`
	AttemptReason            string                       `json:"attempt_reason"`
	Observation              string                       `json:"observation"`
	Postcondition            string                       `json:"postcondition"`
	EffectPostconditions     []EffectPostconditionSummary `json:"effect_postconditions"`
	ExitCode                 *int                         `json:"exit_code"`
	TimedOut                 bool                         `json:"timed_out"`
	Redacted                 bool                         `json:"redacted"`
	GrantsApplySkipAuthority bool                         `json:"grants_apply_skip_authority"`
	NonClaims                []string                     `json:"non_claims"`
}

type EffectPostconditionSummary struct {
	Requirement string `json:"requirement"`
	State       string `json:"state"`
}

func DecodePlan(t *testing.T, content []byte) Plan {
	t.Helper()

	var payload Plan
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("Decode returned error: %v\ncontent=%s", err, content)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("unexpected trailing JSON content: %s", content)
	}

	return payload
}

func FindPlanDiagnostic(t *testing.T, payload Plan, code string, severity string, resourceID string, target string) struct {
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
} {
	t.Helper()

	for _, diagnostic := range payload.Diagnostics {
		if diagnostic.Code == code &&
			diagnostic.Severity == severity &&
			diagnostic.ResourceID == resourceID &&
			diagnostic.Target == target {
			return diagnostic
		}
	}
	t.Fatalf("diagnostics = %#v, want %s %s %s %s", payload.Diagnostics, severity, code, resourceID, target)
	return struct {
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
	}{}
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

	return payload
}
