package clijson

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
