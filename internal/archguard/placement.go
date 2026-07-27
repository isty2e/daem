package archguard

import (
	"fmt"
	"path"
	"strings"
)

const (
	rulePackagePlacementMetadata  = "package-placement-metadata"
	rulePackagePlacementOwnership = "package-placement-ownership"
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

func validPlacementPackagePath(packagePath string) bool {
	if strings.TrimSpace(packagePath) != packagePath ||
		!strings.HasPrefix(packagePath, "internal/") ||
		strings.Contains(packagePath, "\\") ||
		strings.ContainsAny(packagePath, "*?[]") ||
		path.Clean(packagePath) != packagePath {
		return false
	}
	for _, component := range strings.Split(packagePath, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func validatePackagePlacementRows(rows []packagePlacementRow) []GuardrailFinding {
	var findings []GuardrailFinding
	seenIDs := make(map[string]struct{}, len(rows))
	seenPackages := make(map[string]string)
	for index, row := range rows {
		rowPath := fmt.Sprintf("placement[%d]", index)
		if strings.TrimSpace(row.id) == "" || strings.TrimSpace(row.id) != row.id {
			findings = append(findings, GuardrailFinding{
				Rule:   rulePackagePlacementMetadata,
				Path:   rowPath,
				Detail: "placement row ID must be non-empty and canonical",
			})
		} else if _, duplicate := seenIDs[row.id]; duplicate {
			findings = append(findings, GuardrailFinding{
				Rule:   rulePackagePlacementMetadata,
				Path:   row.id,
				Detail: "placement row ID is duplicated",
			})
		} else {
			seenIDs[row.id] = struct{}{}
			rowPath = row.id
		}
		if err := row.placement.validate(); err != nil {
			findings = append(findings, GuardrailFinding{
				Rule:   rulePackagePlacementMetadata,
				Path:   rowPath,
				Detail: err.Error(),
			})
		}
		if len(row.packages) == 0 {
			findings = append(findings, GuardrailFinding{
				Rule:   rulePackagePlacementMetadata,
				Path:   rowPath,
				Detail: "placement row must classify at least one exact package",
			})
		}
		for _, packagePath := range row.packages {
			switch {
			case !validPlacementPackagePath(packagePath):
				findings = append(findings, GuardrailFinding{
					Rule:   rulePackagePlacementMetadata,
					Path:   packagePath,
					Detail: "placement package path must be an exact canonical internal package",
				})
			case seenPackages[packagePath] != "":
				findings = append(findings, GuardrailFinding{
					Rule: rulePackagePlacementMetadata,
					Path: packagePath,
					Detail: fmt.Sprintf(
						"package is classified by both %q and %q",
						seenPackages[packagePath],
						rowPath,
					),
				})
			default:
				seenPackages[packagePath] = rowPath
			}
		}
	}
	return sortedViolations(dedupViolations(findings))
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

func analyzePackagePlacements(records []PackageRecord) []GuardrailFinding {
	findings := validatePackagePlacementRows(packagePlacementRows)
	for _, record := range sortedRecords(records) {
		packagePath, internal := internalPath(record.ImportPath)
		if !internal {
			continue
		}
		candidates := packagePlacementCandidates(packagePlacementRows, packagePath)
		detail := ""
		switch {
		case len(candidates) == 0:
			detail = "package has no Pi placement"
		case len(candidates) > 1:
			detail = "package has more than one Pi placement"
		case candidates[0].validate() != nil:
			detail = "package has an invalid Pi placement"
		}
		if detail == "" {
			continue
		}
		findings = append(findings, GuardrailFinding{
			Rule:        rulePackagePlacementOwnership,
			PackagePath: packagePath,
			Path:        packagePath,
			Detail:      detail,
		})
	}
	return sortedViolations(dedupViolations(findings))
}
