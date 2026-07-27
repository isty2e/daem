package recovery

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// Entry is one canonical journal-described path authority.
type Entry struct {
	subject           topology.SubjectID
	target            target.Target
	consumerTargets   []target.Target
	scope             target.Scope
	destination       output.Destination
	contentPath       string
	contentKind       realization.PathProjectionContentKind
	before            BeforePathState
	expectedAfter     ExpectedPathState
	aggregateContract *aggregate.ProjectionContract
}

// NewEntry constructs one immutable canonical recovery entry.
func NewEntry(
	subject topology.SubjectID,
	agentTarget target.Target,
	consumerTargets []target.Target,
	scope target.Scope,
	destination string,
	contentPath string,
	contentKind realization.PathProjectionContentKind,
	before BeforePathState,
	expectedAfter ExpectedPathState,
	aggregateContract *aggregate.ProjectionContract,
) (Entry, error) {
	parsedDestination, err := output.Parse(destination)
	if err != nil {
		return Entry{}, fmt.Errorf("recovery entry destination: %w", err)
	}
	entry := Entry{
		subject:         subject,
		target:          agentTarget,
		consumerTargets: append([]target.Target(nil), consumerTargets...),
		scope:           scope,
		destination:     parsedDestination,
		contentPath:     contentPath,
		contentKind:     contentKind,
		before:          cloneBeforePathState(before),
		expectedAfter:   expectedAfter.Clone(),
	}
	if aggregateContract != nil {
		contract := aggregateContract.Clone()
		entry.aggregateContract = &contract
	}
	if err := entry.validate(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (entry Entry) validate() error {
	if err := entry.subject.Validate(); err != nil {
		return fmt.Errorf("recovery entry subject: %w", err)
	}
	if _, err := target.ParseScope(string(entry.scope)); err != nil {
		return fmt.Errorf("recovery entry scope: %w", err)
	}
	if err := entry.destination.ValidateScope(entry.scope); err != nil {
		return fmt.Errorf("recovery entry destination: %w", err)
	}

	switch entry.contentKind {
	case realization.PathProjectionFile, realization.PathProjectionDirectory:
		if entry.target != "" || len(entry.consumerTargets) == 0 {
			return fmt.Errorf("recovery path entry requires consumer targets and no primary target")
		}
		if entry.contentPath != "" || entry.aggregateContract != nil {
			return fmt.Errorf("recovery path entry must not carry aggregate projection facts")
		}
		if _, err := target.NewSet(entry.consumerTargets); err != nil {
			return fmt.Errorf("recovery entry consumer targets: %w", err)
		}
	case "":
		if _, err := target.ParseTarget(string(entry.target)); err != nil {
			return fmt.Errorf("recovery entry target: %w", err)
		}
		if len(entry.consumerTargets) != 0 {
			return fmt.Errorf("recovery non-path entry requires one primary target and no consumer targets")
		}
		if err := validateEntryAggregateCorrelation(entry); err != nil {
			return err
		}
	default:
		return fmt.Errorf("recovery entry content kind %q is unsupported", entry.contentKind)
	}

	if err := ValidateBefore(entry.before, entry.contentPath); err != nil {
		return err
	}
	if err := ValidateExpected(entry.expectedAfter, entry.contentPath); err != nil {
		return err
	}
	return nil
}

func validateEntryAggregateCorrelation(entry Entry) error {
	if entry.contentPath == "" {
		if entry.aggregateContract != nil {
			return fmt.Errorf("recovery aggregate contract has no content path")
		}
		return nil
	}
	if entry.aggregateContract == nil {
		return fmt.Errorf("recovery content path %q has no aggregate contract", entry.contentPath)
	}
	contract := entry.aggregateContract.Clone()
	if err := contract.Validate(); err != nil {
		return fmt.Errorf("recovery aggregate contract: %w", err)
	}
	if err := aggregate.ValidateSubjectContract(entry.subject, contract); err != nil {
		return fmt.Errorf("recovery aggregate subject: %w", err)
	}
	address := contract.Address()
	document := address.Document()
	if entry.target != document.Target() || entry.scope != document.Scope() ||
		entry.destination != document.AggregateRoot() ||
		entry.contentPath != string(address.ContentPath()) {
		return fmt.Errorf("recovery aggregate entry does not match its projection contract")
	}
	return nil
}

func cloneBeforePathState(state BeforePathState) BeforePathState {
	clone := state
	if state.PathMode != nil {
		mode := *state.PathMode
		clone.PathMode = &mode
	}
	return clone
}
