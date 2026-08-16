package statefile

const (
	snapshotVersion       = 9
	legacySnapshotVersion = 8
)

type snapshotDTO struct {
	Version                   int                        `json:"version"`
	ManagedPaths              []managedPathDTO           `json:"managed_paths"`
	ManagedAggregateBaselines []managedAggregateDTO      `json:"managed_aggregate_contributions"`
	PendingCarrierInstalls    []pendingCarrierInstallDTO `json:"pending_carrier_installs"`
	PendingCarrierRemovals    []pendingCarrierRemovalDTO `json:"pending_carrier_removals"`
	ManagedCarrierClaims      []managedCarrierClaimDTO   `json:"managed_carrier_claims"`
	DelegateAttempts          []delegateAttemptDTO       `json:"delegate_attempts"`
	HostRouteAttempts         []hostRouteAttemptDTO      `json:"host_route_attempts"`
}

type subjectDTO struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type managedPathDTO struct {
	Subject          subjectDTO `json:"subject"`
	ConsumerTargets  []string   `json:"consumer_targets"`
	Scope            string     `json:"scope"`
	Destination      string     `json:"destination"`
	ContentHash      string     `json:"content_hash"`
	ContentKind      string     `json:"content_kind"`
	PermissionPolicy string     `json:"permission_policy"`
	FileMode         *uint32    `json:"file_mode,omitempty"`
}

type managedAggregateDTO struct {
	Subject               subjectDTO `json:"subject"`
	PlacementID           string     `json:"placement_id"`
	Target                string     `json:"target"`
	Scope                 string     `json:"scope"`
	AggregateRoot         string     `json:"aggregate_root"`
	ContentPath           string     `json:"content_path"`
	MergeUnit             string     `json:"merge_unit"`
	Cardinality           string     `json:"cardinality"`
	SiblingRetention      string     `json:"sibling_retention"`
	SiblingPreservation   string     `json:"sibling_preservation"`
	Equivalence           string     `json:"equivalence"`
	CanonicalContribution string     `json:"canonical_contribution"`
	CodecContractID       string     `json:"codec_contract_id"`
	ComparedFields        []string   `json:"compared_fields"`
}

type stateAuthorityDTO struct {
	StatefileAuthority pathAuthorityDTO `json:"statefile_authority"`
	ManifestPath       string           `json:"manifest_path"`
}

type pathAuthorityDTO struct {
	Key     string `json:"key"`
	Witness string `json:"semantics_witness"`
}

type managedCarrierIdentityDTO struct {
	CarrierSubject     subjectDTO `json:"carrier_subject"`
	CarrierFamily      string     `json:"carrier_family"`
	Target             string     `json:"target"`
	Scope              string     `json:"scope"`
	SourceKind         string     `json:"source_kind"`
	SourceRef          string     `json:"source_ref"`
	RelationSubject    subjectDTO `json:"relation_subject"`
	RelationSubjectKey string     `json:"relation_subject_key"`
	ManagedInstanceKey string     `json:"managed_instance_key"`
}

type delegatedRequestDTO struct {
	RouteID                string `json:"route_id"`
	AdapterContractVersion string `json:"adapter_contract_version"`
	CanonicalRequestHash   string `json:"canonical_request_hash"`
}

type pendingCarrierInstallDTO struct {
	Owner          stateAuthorityDTO         `json:"owner"`
	Identity       managedCarrierIdentityDTO `json:"identity"`
	InstallRequest delegatedRequestDTO       `json:"install_request"`
}

type pendingCarrierRemovalDTO struct {
	Claim                managedCarrierClaimDTO `json:"claim"`
	RemoveRequest        delegatedRequestDTO    `json:"remove_request"`
	EffectPostconditions []string               `json:"effect_postconditions"`
	EffectBaselines      []effectBaselineDTO    `json:"effect_baselines"`
}

type effectBaselineDTO struct {
	Requirement string `json:"requirement"`
	State       string `json:"state"`
	ContentHash string `json:"content_hash,omitempty"`
}

type managedCarrierClaimDTO struct {
	Owner          stateAuthorityDTO         `json:"owner"`
	Identity       managedCarrierIdentityDTO `json:"identity"`
	InstallRequest delegatedRequestDTO       `json:"install_request"`
	Provenance     string                    `json:"provenance"`
}

type delegateAttemptDTO struct {
	Subject         subjectDTO `json:"subject"`
	Target          string     `json:"target"`
	Scope           string     `json:"scope"`
	PlanIdentityKey string     `json:"plan_identity_key"`
	ObservedAt      string     `json:"observed_at"`
	Status          string     `json:"status"`
	Reason          string     `json:"reason"`
	AttemptObserved *bool      `json:"attempt_observed"`
	ProcessReason   string     `json:"process_reason,omitempty"`
	Observation     string     `json:"observation"`
	Postcondition   string     `json:"postcondition"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	TimedOut        bool       `json:"timed_out,omitempty"`
	StdoutTruncated bool       `json:"stdout_truncated,omitempty"`
	StderrTruncated bool       `json:"stderr_truncated,omitempty"`
	Redacted        bool       `json:"redacted,omitempty"`
}

type hostRouteAttemptDTO struct {
	Subject              subjectDTO                      `json:"subject"`
	Target               string                          `json:"target"`
	Scope                string                          `json:"scope"`
	Operation            string                          `json:"operation"`
	RouteID              string                          `json:"route_id"`
	RouteRequestHash     string                          `json:"route_request_hash"`
	ObservedAt           string                          `json:"observed_at"`
	ResultClass          string                          `json:"result_class"`
	Reason               string                          `json:"reason"`
	AttemptObserved      bool                            `json:"attempt_observed"`
	AttemptReason        string                          `json:"attempt_reason,omitempty"`
	Observation          string                          `json:"observation"`
	Postcondition        string                          `json:"postcondition"`
	EffectPostconditions []effectPostconditionSummaryDTO `json:"effect_postconditions"`
	ExitCode             *int                            `json:"exit_code,omitempty"`
	TimedOut             bool                            `json:"timed_out,omitempty"`
	Redacted             bool                            `json:"redacted,omitempty"`
}

type effectPostconditionSummaryDTO struct {
	Requirement string `json:"requirement"`
	State       string `json:"state"`
}
