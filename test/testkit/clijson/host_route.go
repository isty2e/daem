package clijson

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
