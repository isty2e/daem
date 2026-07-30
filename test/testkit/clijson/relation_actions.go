package clijson

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

type RelationOrderMember struct {
	Subject *struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	HostLoadIdentity string `json:"host_load_identity"`
}

type RelationOrderRisk struct {
	Code           string `json:"code"`
	ManagedSubject *struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"managed_subject"`
	ForeignIdentity     string `json:"foreign_identity"`
	ManagedWasBefore    bool   `json:"managed_was_before"`
	ManagedWillBeBefore bool   `json:"managed_will_be_before"`
}

type RelationOrderAction struct {
	Target                string                `json:"target"`
	Scope                 string                `json:"scope"`
	ClassID               string                `json:"class_id"`
	SequenceID            string                `json:"sequence_id"`
	RuntimeMeaning        string                `json:"runtime_meaning"`
	ConstraintFingerprint string                `json:"constraint_fingerprint"`
	Authority             string                `json:"authority"`
	Revision              string                `json:"revision"`
	Kind                  string                `json:"kind"`
	Reason                string                `json:"reason"`
	Detail                string                `json:"detail"`
	DesiredMembers        []RelationOrderMember `json:"desired_members"`
	ObservedMembers       []RelationOrderMember `json:"observed_members"`
	MissingMembers        []RelationOrderMember `json:"missing_members"`
	ForeignRowCount       int                   `json:"foreign_row_count"`
	Risks                 []RelationOrderRisk   `json:"risks"`
	BlocksOrdinaryApply   bool                  `json:"blocks_ordinary_apply"`
	RequiresMutation      bool                  `json:"requires_mutation"`
}

type RelationOrderResult struct {
	Target     string `json:"target"`
	Scope      string `json:"scope"`
	ClassID    string `json:"class_id"`
	SequenceID string `json:"sequence_id"`
	Outcome    string `json:"outcome"`
	Changed    bool   `json:"changed"`
	Detail     string `json:"detail"`
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
	FinalClaimProvenance     string   `json:"final_claim_provenance"`
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
