package archguard

import (
	"fmt"
	"strings"
)

type semanticAffinity uint8

const (
	affinityUnknown semanticAffinity = iota
	affinityNone
	affinityDesired
	affinityTopology
	affinitySupply
	affinityRealization
	affinityAssurance
	affinityReconciliation
	affinityEffect
)

func (affinity semanticAffinity) valid() bool {
	return affinity >= affinityNone && affinity <= affinityEffect
}

func (affinity semanticAffinity) String() string {
	switch affinity {
	case affinityNone:
		return "none"
	case affinityDesired:
		return "Desired"
	case affinityTopology:
		return "Topology"
	case affinitySupply:
		return "Supply"
	case affinityRealization:
		return "Realization"
	case affinityAssurance:
		return "Assurance"
	case affinityReconciliation:
		return "Reconciliation"
	case affinityEffect:
		return "Effect"
	default:
		return "unknown"
	}
}

type mechanismRole uint8

const (
	roleUnknown mechanismRole = iota
	roleSemanticKernel
	roleStableValue
	roleLoweringRefinement
	roleCodec
	roleObservationAdapter
	roleActiveAdapter
	roleTransactionRecovery
	roleWorkflowComposition
	rolePresentation
	roleRepositoryTool
	roleTestSupport
)

func (role mechanismRole) valid() bool {
	return role >= roleSemanticKernel && role <= roleTestSupport
}

func (role mechanismRole) String() string {
	switch role {
	case roleSemanticKernel:
		return "semantic kernel"
	case roleStableValue:
		return "stable value"
	case roleLoweringRefinement:
		return "lowering/refinement"
	case roleCodec:
		return "codec"
	case roleObservationAdapter:
		return "observation adapter"
	case roleActiveAdapter:
		return "active adapter"
	case roleTransactionRecovery:
		return "transaction/recovery"
	case roleWorkflowComposition:
		return "workflow/composition"
	case rolePresentation:
		return "presentation"
	case roleRepositoryTool:
		return "repository tool"
	case roleTestSupport:
		return "test support"
	default:
		return "unknown"
	}
}

type specializationKind uint8

const (
	specializationNone specializationKind = iota
	specializationFamily
	specializationHost
	specializationFormat
	specializationProtocol
	specializationBackend
	specializationPlatform
)

func (kind specializationKind) valid() bool {
	return kind >= specializationNone && kind <= specializationPlatform
}

type packageSpecialization struct {
	kind  specializationKind
	value string
}

func (specialization packageSpecialization) validate() error {
	if !specialization.kind.valid() {
		return fmt.Errorf("specialization kind is invalid")
	}
	value := strings.TrimSpace(specialization.value)
	if specialization.kind == specializationNone {
		if value != "" {
			return fmt.Errorf("specialization value requires a specialization kind")
		}
		return nil
	}
	if value == "" {
		return fmt.Errorf("specialization kind requires a non-empty value")
	}
	if value != specialization.value {
		return fmt.Errorf("specialization value must be canonical")
	}
	return nil
}

type packagePlacement struct {
	affinity       semanticAffinity
	role           mechanismRole
	specialization packageSpecialization
}

func (placement packagePlacement) validate() error {
	if !placement.affinity.valid() {
		return fmt.Errorf("semantic affinity is invalid")
	}
	if !placement.role.valid() {
		return fmt.Errorf("mechanism role is invalid")
	}
	if err := placement.specialization.validate(); err != nil {
		return err
	}
	return nil
}

type packagePlacementRow struct {
	id        string
	placement packagePlacement
	packages  []string
}

func plainPlacement(affinity semanticAffinity, role mechanismRole) packagePlacement {
	return packagePlacement{
		affinity: affinity,
		role:     role,
		specialization: packageSpecialization{
			kind: specializationNone,
		},
	}
}

func specializedPlacement(
	affinity semanticAffinity,
	role mechanismRole,
	kind specializationKind,
	value string,
) packagePlacement {
	return packagePlacement{
		affinity: affinity,
		role:     role,
		specialization: packageSpecialization{
			kind:  kind,
			value: value,
		},
	}
}

func packagePlacementCandidates(
	rows []packagePlacementRow,
	packagePath string,
) []packagePlacement {
	var candidates []packagePlacement
	for _, row := range rows {
		for _, admittedPackage := range row.packages {
			if packagePath == admittedPackage {
				candidates = append(candidates, row.placement)
			}
		}
	}
	return candidates
}

func packagePlacementFor(packagePath string) (packagePlacement, bool) {
	candidates := packagePlacementCandidates(packagePlacementRows, packagePath)
	if len(candidates) != 1 || candidates[0].validate() != nil {
		return packagePlacement{}, false
	}
	return candidates[0], true
}
