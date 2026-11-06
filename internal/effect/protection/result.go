// Package protection owns physical permission containment and record settlement,
// shared by invocation and recovery workflows. It never restores document content.
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

func Unobserved(row ownership.PendingProtection) Record {
	record := Record{invocation: row.Invocation(), phase: row.Phase(), settlement: SettlementUnknown}
	for _, document := range row.Documents() {
		record.targets = append(record.targets, Target{destination: document.Bindings()[0].Destination(), state: TargetBlocked})
	}
	return record
}

func (record Record) Unobserved() Record {
	record.settlement = SettlementUnknown
	return record
}

func (record Record) WithFailure(cause error) Record {
	record.cause = cause
	return record
}

func (record Record) Refused(cause error) Record {
	record.cause = cause
	record.targets = slices.Clone(record.targets)
	for index := range record.targets {
		record.targets[index].state = TargetBlocked
		record.targets[index].cause = cause
	}
	return record
}

func (record Record) Invocation() executionidentity.Invocation { return record.invocation }
func (record Record) Phase() ownership.ProtectionPhase         { return record.phase }
func (record Record) Settlement() Settlement                   { return record.settlement }
func (record Record) Targets() []Target                        { return slices.Clone(record.targets) }
func (record Record) Cause() error                             { return record.cause }

func cloneRecords(records []Record) []Record {
	result := slices.Clone(records)
	for index := range result {
		result[index].targets = slices.Clone(result[index].targets)
	}
	return result
}

type Result struct{ records []Record }

func NewResult(records []Record) Result { return Result{records: cloneRecords(records)} }

func (result Result) Records() []Record { return cloneRecords(result.records) }

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
