package inventory

import "fmt"

type ExecutionBasis struct {
	recordFingerprint string
	activeAuthority   ActiveJournalAuthority
}

func NewExecutionBasis(recordFingerprint string, activeAuthority ActiveJournalAuthority) (ExecutionBasis, error) {
	if recordFingerprint == "" {
		return ExecutionBasis{}, fmt.Errorf("recovery journal record fingerprint is required")
	}
	if err := activeAuthority.Validate(); err != nil {
		return ExecutionBasis{}, err
	}
	return ExecutionBasis{recordFingerprint: recordFingerprint, activeAuthority: activeAuthority}, nil
}

func (basis ExecutionBasis) Validate() error {
	_, err := NewExecutionBasis(basis.recordFingerprint, basis.activeAuthority)
	return err
}

func (basis ExecutionBasis) RecordFingerprint() string { return basis.recordFingerprint }

func (basis ExecutionBasis) ActiveAuthority() ActiveJournalAuthority { return basis.activeAuthority }
