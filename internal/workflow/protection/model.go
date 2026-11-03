// Package protection owns permission containment and settlement for invocations
// and source-independent recovery. It never restores document content.
package protection

import (
	"errors"
	"slices"

	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

type TargetState string

const (
	TargetCheck   TargetState = "check_and_restrict"
	TargetBlocked TargetState = "blocked"
	TargetSecure  TargetState = "secure"
	TargetAbsent  TargetState = "absent"
)

type Settlement string

const (
	SettlementPending Settlement = "pending"
	SettlementCleared Settlement = "cleared"
	SettlementUnknown Settlement = "unknown"
)

// Target preserves the result of one document independently of its peers.
type Target struct {
	destination output.Destination
	state       TargetState
	expanded    bool
	cause       error
}

func (target Target) Destination() output.Destination { return target.destination }
func (target Target) State() TargetState              { return target.state }

// Expanded reports expansion observed during this operation, not earlier history.
func (target Target) Expanded() bool { return target.expanded }
func (target Target) Cause() error   { return target.cause }

type Record struct {
	invocation executionidentity.Invocation
	phase      ownership.ProtectionPhase
	settlement Settlement
	targets    []Target
	cause      error
}

func (record Record) Invocation() executionidentity.Invocation { return record.invocation }
func (record Record) Phase() ownership.ProtectionPhase         { return record.phase }
func (record Record) Settlement() Settlement                   { return record.settlement }
func (record Record) Targets() []Target                        { return slices.Clone(record.targets) }
func (record Record) Cause() error                             { return record.cause }

type Plan struct {
	records []Record
	blocker error
}

func (plan Plan) Records() []Record {
	result := slices.Clone(plan.records)
	for index := range result {
		result[index].targets = slices.Clone(result[index].targets)
	}
	return result
}

func (plan Plan) Clone() Plan    { return Plan{records: plan.Records(), blocker: plan.blocker} }
func (plan Plan) Blocker() error { return plan.blocker }
func (plan Plan) HasErrors() bool {
	if plan.blocker != nil {
		return true
	}
	for _, record := range plan.records {
		if record.cause != nil {
			return true
		}
		for _, target := range record.targets {
			if target.cause != nil {
				return true
			}
		}
	}
	return false
}

type Result struct{ records []Record }

func (result Result) Records() []Record { return (Plan{records: result.records}).Records() }

type ResultPhase string

const (
	ResultPending   ResultPhase = "pending"
	ResultCompleted ResultPhase = "completed"
	ResultUnknown   ResultPhase = "unknown"
)

func (result Result) Phase() ResultPhase {
	phase := ResultCompleted
	if len(result.records) == 0 {
		return ResultPending
	}
	for _, record := range result.records {
		if record.settlement == SettlementUnknown {
			return ResultUnknown
		}
		if record.settlement != SettlementCleared {
			phase = ResultPending
		}
	}
	return phase
}

func (result Result) Complete() bool { return result.Phase() == ResultCompleted }

func (result Result) Failure() error {
	var failures []error
	for _, record := range result.records {
		failures = append(failures, record.cause)
		for _, target := range record.targets {
			failures = append(failures, target.cause)
		}
	}
	return errors.Join(failures...)
}
