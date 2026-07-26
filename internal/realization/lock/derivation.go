package lock

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func cloneExactArtifactIdentity(identity artifact.ExactIdentity) *artifact.ExactIdentity {
	cloned := identity
	return &cloned
}

func validateExactArtifactDerivationMatchesSupply(
	derivation DerivationContract,
	supply artifact.ExactIdentity,
	isExactFileUse bool,
) error {
	switch derivation.kind {
	case DerivationDirectResolution:
		if derivation.directResolution == nil || !derivation.directResolution.Equal(supply) {
			return fmt.Errorf("direct resolution derivation must match exact Supply identity")
		}
	case DerivationDeterministicTransform:
		if derivation.deterministicTransform == nil {
			return fmt.Errorf("deterministic transform body is required")
		}
		if isExactFileUse {
			if !derivation.deterministicTransform.InputIdentity.Equal(supply) {
				return fmt.Errorf("file materialization input identity must match exact Supply identity")
			}
			if derivation.deterministicTransform.AlgorithmID != artifact.FileMaterializationAlgorithmID ||
				derivation.deterministicTransform.AlgorithmVersion != artifact.FileMaterializationAlgorithmVersion ||
				derivation.deterministicTransform.ExecutionDomain != artifact.FileMaterializationExecutionDomain {
				return fmt.Errorf("exact file use deterministic transform must use the file materialization contract")
			}
			return nil
		}
		if !derivation.deterministicTransform.ExpectedOutputIdentity.Equal(supply) {
			return fmt.Errorf("deterministic transform expected output identity must match exact Supply identity")
		}
	}
	return nil
}

// DerivationKind identifies how an exact subject is derived.
type DerivationKind string

const (
	DerivationDirectResolution       DerivationKind = "direct_resolution"
	DerivationDeterministicTransform DerivationKind = "deterministic_transform"
)

// DeterministicTransform describes a replayable deterministic transformation into an exact output.
type DeterministicTransform struct {
	InputIdentity          artifact.ExactIdentity
	RecipeHash             string
	AlgorithmID            string
	AlgorithmVersion       string
	ExecutionDomain        string
	ExpectedOutputIdentity artifact.ExactIdentity
}

// DerivationContract is orthogonal to exact Supply and structural Realization.
type DerivationContract struct {
	kind                   DerivationKind
	directResolution       *artifact.ExactIdentity
	deterministicTransform *DeterministicTransform
}

// NewDirectResolutionDerivation records direct source resolution for an exact identity.
func NewDirectResolutionDerivation(identity artifact.ExactIdentity) (DerivationContract, error) {
	contract := DerivationContract{
		kind:             DerivationDirectResolution,
		directResolution: cloneExactArtifactIdentity(identity),
	}
	if err := contract.validate(); err != nil {
		return DerivationContract{}, err
	}
	return contract, nil
}

// NewFileMaterializationDerivation maps one canonical file materialization to
// the corresponding direct or deterministic locked derivation reference.
func NewFileMaterializationDerivation(
	materialization artifact.FileMaterialization,
) (DerivationContract, error) {
	if !materialization.ChangesIdentity() {
		return NewDirectResolutionDerivation(materialization.InputIdentity())
	}
	return NewDeterministicTransformDerivation(DeterministicTransform{
		InputIdentity:          materialization.InputIdentity(),
		RecipeHash:             materialization.RecipeHash(),
		AlgorithmID:            artifact.FileMaterializationAlgorithmID,
		AlgorithmVersion:       artifact.FileMaterializationAlgorithmVersion,
		ExecutionDomain:        artifact.FileMaterializationExecutionDomain,
		ExpectedOutputIdentity: materialization.OutputIdentity(),
	})
}

// NewDeterministicTransformDerivation records a deterministic transformation into an exact output identity.
func NewDeterministicTransformDerivation(transform DeterministicTransform) (DerivationContract, error) {
	contract := DerivationContract{
		kind:                   DerivationDeterministicTransform,
		deterministicTransform: cloneDeterministicTransform(transform),
	}
	if err := contract.validate(); err != nil {
		return DerivationContract{}, err
	}
	return contract, nil
}

// Kind returns the derivation contract kind.
func (contract DerivationContract) Kind() DerivationKind {
	return contract.kind
}

// DirectResolution returns the exact identity for direct-resolution derivations.
func (contract DerivationContract) DirectResolution() (artifact.ExactIdentity, bool) {
	if contract.directResolution == nil {
		return artifact.ExactIdentity{}, false
	}
	return *cloneExactArtifactIdentity(*contract.directResolution), contract.kind == DerivationDirectResolution
}

// DeterministicTransform returns the deterministic transform contract.
func (contract DerivationContract) DeterministicTransform() (DeterministicTransform, bool) {
	if contract.deterministicTransform == nil {
		return DeterministicTransform{}, false
	}
	return *cloneDeterministicTransform(*contract.deterministicTransform), contract.kind == DerivationDeterministicTransform
}

func (contract DerivationContract) validate() error {
	if err := validateDerivationKind(contract.kind); err != nil {
		return err
	}
	bodyCount := 0
	if contract.directResolution != nil {
		bodyCount++
	}
	if contract.deterministicTransform != nil {
		bodyCount++
	}
	if bodyCount != 1 {
		return fmt.Errorf("derivation contract %q must contain exactly one derivation body", contract.kind)
	}
	switch contract.kind {
	case DerivationDirectResolution:
		if contract.directResolution == nil {
			return fmt.Errorf("direct resolution derivation requires exact identity")
		}
		if err := contract.directResolution.Validate(); err != nil {
			return fmt.Errorf("direct resolution derivation: %w", err)
		}
		return nil
	case DerivationDeterministicTransform:
		if contract.deterministicTransform == nil {
			return fmt.Errorf("deterministic transform derivation requires transform contract")
		}
		return validateDeterministicTransform(*contract.deterministicTransform)
	default:
		return fmt.Errorf("unsupported derivation kind %q", contract.kind)
	}
}

func validateDerivationKind(kind DerivationKind) error {
	switch kind {
	case DerivationDirectResolution, DerivationDeterministicTransform:
		return nil
	default:
		return fmt.Errorf("unsupported derivation kind %q", kind)
	}
}

func validateDeterministicTransform(transform DeterministicTransform) error {
	if err := transform.InputIdentity.Validate(); err != nil {
		return fmt.Errorf("deterministic transform input identity: %w", err)
	}
	if strings.TrimSpace(transform.RecipeHash) == "" {
		return fmt.Errorf("deterministic transform recipe hash is required")
	}
	if strings.TrimSpace(transform.AlgorithmID) == "" {
		return fmt.Errorf("deterministic transform algorithm id is required")
	}
	if strings.TrimSpace(transform.AlgorithmVersion) == "" {
		return fmt.Errorf("deterministic transform algorithm version is required")
	}
	if strings.TrimSpace(transform.ExecutionDomain) == "" {
		return fmt.Errorf("deterministic transform execution domain is required")
	}
	if err := transform.ExpectedOutputIdentity.Validate(); err != nil {
		return fmt.Errorf("deterministic transform expected output identity: %w", err)
	}
	return nil
}

func cloneDeterministicTransform(transform DeterministicTransform) *DeterministicTransform {
	cloned := DeterministicTransform{
		InputIdentity:          *cloneExactArtifactIdentity(transform.InputIdentity),
		RecipeHash:             strings.TrimSpace(transform.RecipeHash),
		AlgorithmID:            strings.TrimSpace(transform.AlgorithmID),
		AlgorithmVersion:       strings.TrimSpace(transform.AlgorithmVersion),
		ExecutionDomain:        strings.TrimSpace(transform.ExecutionDomain),
		ExpectedOutputIdentity: *cloneExactArtifactIdentity(transform.ExpectedOutputIdentity),
	}
	return &cloned
}

func cloneDerivationContract(contract DerivationContract) DerivationContract {
	cloned := DerivationContract{kind: contract.kind}
	if contract.directResolution != nil {
		cloned.directResolution = cloneExactArtifactIdentity(*contract.directResolution)
	}
	if contract.deterministicTransform != nil {
		cloned.deterministicTransform = cloneDeterministicTransform(*contract.deterministicTransform)
	}
	return cloned
}
