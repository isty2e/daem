package archguard

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	densityProductionFileWarning = 18
	densityProductionFileReview  = 25
	densityProductionLineWarning = 350
	densityProductionLineReview  = 500
	densityTestLineWarning       = 350
)

type densityReviewAdmission struct {
	reviewedValue       int
	owner               string
	reason              string
	naturalSplit        string
	alternativeRejected string
}

func (admission densityReviewAdmission) validate(metric string) error {
	if admission.reviewedValue <= 0 {
		return fmt.Errorf("reviewed value must be positive")
	}
	if strings.TrimSpace(admission.owner) == "" {
		return fmt.Errorf("owner is required")
	}
	if strings.TrimSpace(admission.reason) == "" {
		return fmt.Errorf("reason is required")
	}
	expectedReasonPrefix := fmt.Sprintf("at %d %s,", admission.reviewedValue, metric)
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(admission.reason)), expectedReasonPrefix) {
		return fmt.Errorf("reason must start with %q to bind rationale to the reviewed value", expectedReasonPrefix)
	}
	if strings.TrimSpace(admission.naturalSplit) == "" {
		return fmt.Errorf("natural split is required")
	}
	if strings.TrimSpace(admission.alternativeRejected) == "" {
		return fmt.Errorf("alternative rejection is required")
	}
	return nil
}

var packageDensityAdmissions = map[string]densityReviewAdmission{
	"internal/archguard": {
		reviewedValue:       19,
		owner:               "repository topology analysis and report projection",
		reason:              "at 19 production files, Pi validation, the finite placement catalog, and import policy have distinct change reasons while all guard families share one package-record model, finding identity, and deterministic report contract",
		naturalSplit:        "move each guard family into a child analyzer package",
		alternativeRejected: "the children would export test-only machinery and duplicate the shared finding and report boundary",
	},
	"internal/cli": {
		reviewedValue:       25,
		owner:               "process command boundary",
		reason:              "at 25 production files, parsing, dispatch, help, streams, cancellation, and exit selection form one executable boundary",
		naturalSplit:        "create one child package per command",
		alternativeRejected: "semantic use cases already live outside CLI, so command packages would add import paths without isolating new invariants",
	},
	"internal/effect/execute": {
		reviewedValue:       20,
		owner:               "authorized Effect sequence",
		reason:              "at 20 production files, mutation, verification, rollback, recovery, and state commit share private execution state and ordering",
		naturalSplit:        "separate effect phases into child packages",
		alternativeRejected: "phase packages would expose private transaction state and weaken the single ordered effect boundary",
	},
	"internal/effect/journal": {
		reviewedValue:       18,
		owner:               "journal-v7 persistence transaction",
		reason:              "at 18 production files, capture, backup identity, and active lifecycle evolve atomically under one private wire schema",
		naturalSplit:        "separate capture and lifecycle persistence packages",
		alternativeRejected: "the split would duplicate schema and transaction authority; wire-neutral recovery algebra is already isolated",
	},
	"internal/realization/lock": {
		reviewedValue:       21,
		owner:               "canonical Realization aggregate",
		reason:              "at 21 production files, identity, admission, ordering, snapshot comparison, operation, and replay invariants form one closed aggregate after delegate-plan identity moved to its canonical owner",
		naturalSplit:        "move realization variants into child packages",
		alternativeRejected: "variant packages would require reverse imports or export aggregate-private construction rules",
	},
	"internal/cli/present": {
		reviewedValue:       32,
		owner:               "CLI human, JSON, diff, and exit projection",
		reason:              "at 32 production files, command, inventory, and carrier-lifecycle projections share stable cross-format output and error-shaping contracts",
		naturalSplit:        "create one presentation child package per command",
		alternativeRejected: "command packages would duplicate transport policy and make cross-command output consistency implicit",
	},
	"internal/effect/storage/commit": {
		reviewedValue:       21,
		owner:               "guarded filesystem commit adapter",
		reason:              "at 21 production files, commit phases, entry identity, visibility, residue, and rollback share one platform-selected transaction",
		naturalSplit:        "separate commit phases into child packages",
		alternativeRejected: "phase packages would export transaction internals and make rollback ordering cross-package state",
	},
	"internal/realization/aggregate/codec/mcp": {
		reviewedValue:       18,
		owner:               "MCP aggregate external protocol",
		reason:              "at 18 production files, canonical entry, mutation fold, restore, compare, preservation, launch-vector decoding, Codex global environment-reference normalization, and the shared Pi adapter document codec form one syntax boundary while capability admission remains profile-owned",
		naturalSplit:        "create one aggregate codec package per host",
		alternativeRejected: "host packages would duplicate protocol rules and require a facade over the same canonical aggregate",
	},
	"internal/workflow/apply": {
		reviewedValue:       24,
		owner:               "application use case",
		reason:              "at 24 production files, one PreparedWrite lifecycle binds authority evidence, operation fingerprinting, retained-root capability, apply-time MCP environment and provider preflight, single-use execution, and project/global commit sequencing",
		naturalSplit:        "separate apply phases into child workflows",
		alternativeRejected: "child workflows would export or duplicate the private PreparedWrite authority transfer and obscure its single-use commit boundary",
	},
	"internal/workflow/authoring": {
		reviewedValue:       17,
		owner:               "manifest, lock, and exact management-metadata authoring transaction",
		reason:              "at 17 production files, request admission, explicit multi-resource provider authoring, exact management selection, optimistic revalidation, and recoverable metadata commit sequencing form one host-free authoring boundary",
		naturalSplit:        "create a separate unmanage workflow or merge selection, plan, execution, and result files",
		alternativeRejected: "a child workflow would reuse authoring in reverse dependency order, while file merging would couple independent identity, planning, publication, and output change triggers",
	},
}

var productionFileDensityAdmissions = map[string]densityReviewAdmission{}

func analyzeDensity(records []PackageRecord) ([]rawFinding, []PackageDensity) {
	var findings []rawFinding
	var densities []PackageDensity
	for _, record := range sortedRecords(records) {
		packagePath, ok := internalPath(record.ImportPath)
		if !ok {
			continue
		}

		density := packageDensity(packagePath, record)
		densities = append(densities, density)
		findings = append(findings, packageDensityFindings(density, packageDensityAdmissions)...)
		findings = append(findings, fileDensityFindings(packagePath, record, productionFileDensityAdmissions)...)
	}

	return findings, sortedPackageDensities(densities)
}

func packageDensity(packagePath string, record PackageRecord) PackageDensity {
	density := PackageDensity{
		PackagePath:     packagePath,
		ProductionFiles: len(densityProductionFiles(record)),
		TestFiles:       len(record.TestGoFiles) + len(record.XTestGoFiles),
	}

	for _, fileName := range densityProductionFiles(record) {
		filePath := filepath.ToSlash(filepath.Join(packagePath, fileName))
		lines := lineCount(record, fileName)
		if lines > density.MaxProductionLines {
			density.MaxProductionLines = lines
			density.MaxProductionPath = filePath
		}
	}

	for _, fileName := range sortedStrings(append(append([]string(nil), record.TestGoFiles...), record.XTestGoFiles...)) {
		filePath := filepath.ToSlash(filepath.Join(packagePath, fileName))
		lines := lineCount(record, fileName)
		if lines > density.MaxTestLines {
			density.MaxTestLines = lines
			density.MaxTestPath = filePath
		}
	}

	return density
}

func packageDensityFindings(
	density PackageDensity,
	admissions map[string]densityReviewAdmission,
) []rawFinding {
	reviewed := false
	if admission, admitted := admissions[density.PackagePath]; admitted {
		if err := admission.validate("production files"); err != nil {
			return []rawFinding{densityAdmissionInvalidFinding(density.PackagePath, density.PackagePath, err.Error())}
		}
		switch {
		case density.ProductionFiles > admission.reviewedValue:
			return []rawFinding{densityReviewRequiredFinding(
				density.PackagePath,
				density.PackagePath,
				fmt.Sprintf("production file count increased from reviewed %d to %d", admission.reviewedValue, density.ProductionFiles),
			)}
		case density.ProductionFiles == admission.reviewedValue:
			reviewed = true
		}
	}
	if density.ProductionFiles <= densityProductionFileWarning {
		return nil
	}

	detail := fmt.Sprintf(
		"production file count %d exceeds warning threshold %d",
		density.ProductionFiles,
		densityProductionFileWarning,
	)
	if density.ProductionFiles > densityProductionFileReview && !reviewed {
		return []rawFinding{densityReviewRequiredFinding(
			density.PackagePath,
			density.PackagePath,
			fmt.Sprintf("production file count %d exceeds unreviewed threshold %d", density.ProductionFiles, densityProductionFileReview),
		)}
	}
	return []rawFinding{densityWarningFinding(
		density.PackagePath,
		density.PackagePath,
		detail,
	)}
}

func fileDensityFindings(
	packagePath string,
	record PackageRecord,
	admissions map[string]densityReviewAdmission,
) []rawFinding {
	var findings []rawFinding
	for _, fileName := range densityProductionFiles(record) {
		filePath := filepath.ToSlash(filepath.Join(packagePath, fileName))
		lines := lineCount(record, fileName)
		reviewed := false
		if admission, admitted := admissions[filePath]; admitted {
			if err := admission.validate("production lines"); err != nil {
				findings = append(findings, densityAdmissionInvalidFinding(packagePath, filePath, err.Error()))
				continue
			}
			switch {
			case lines > admission.reviewedValue:
				findings = append(findings, densityReviewRequiredFinding(
					packagePath,
					filePath,
					fmt.Sprintf("production line count increased from reviewed %d to %d", admission.reviewedValue, lines),
				))
				continue
			case lines == admission.reviewedValue:
				reviewed = true
			}
		}
		if lines <= densityProductionLineWarning {
			continue
		}
		if lines > densityProductionLineReview && !reviewed {
			findings = append(findings, densityReviewRequiredFinding(
				packagePath,
				filePath,
				fmt.Sprintf("production line count %d exceeds unreviewed threshold %d", lines, densityProductionLineReview),
			))
			continue
		}
		findings = append(findings, densityWarningFinding(
			packagePath,
			filePath,
			fmt.Sprintf("production line count %d exceeds warning threshold %d", lines, densityProductionLineWarning),
		))
	}

	testFiles := append(append([]string(nil), record.TestGoFiles...), record.XTestGoFiles...)
	for _, fileName := range sortedStrings(testFiles) {
		filePath := filepath.ToSlash(filepath.Join(packagePath, fileName))
		lines := lineCount(record, fileName)
		if lines <= densityTestLineWarning {
			continue
		}
		findings = append(findings, densityWarningFinding(
			packagePath,
			filePath,
			fmt.Sprintf("test line count %d exceeds warning threshold %d", lines, densityTestLineWarning),
		))
	}

	return findings
}

func densityAdmissionInvalidFinding(packagePath string, path string, detail string) rawFinding {
	return rawFinding{
		finding: GuardrailFinding{
			Rule:        ruleDensityAdmissionInvalid,
			PackagePath: packagePath,
			Path:        path,
			Reason:      "density admission must identify a current target and preserve its counterfactual review",
			Detail:      detail,
		},
		disposition: findingDispositionViolation,
	}
}

func densityReviewRequiredFinding(packagePath string, path string, detail string) rawFinding {
	return rawFinding{
		finding: GuardrailFinding{
			Rule:        ruleDensityReviewRequired,
			PackagePath: packagePath,
			Path:        path,
			Reason:      "density requires explicit ownership review; size alone does not prove invalidity",
			Detail:      detail,
		},
		disposition: findingDispositionReviewRequired,
	}
}

func densityWarningFinding(packagePath string, path string, detail string) rawFinding {
	return rawFinding{
		finding: GuardrailFinding{
			Rule:        ruleDensityThreshold,
			PackagePath: packagePath,
			Path:        path,
			Reason:      "density is a review pressure signal; inspect semantic ownership before splitting",
			Detail:      detail,
		},
		disposition: findingDispositionWarning,
	}
}

func densityProductionFiles(record PackageRecord) []string {
	files := append([]string(nil), record.GoFiles...)
	files = append(files, record.CgoFiles...)
	return sortedStrings(files)
}

func validateDensityAdmissionInventory(
	records []PackageRecord,
	packageAdmissions map[string]densityReviewAdmission,
	fileAdmissions map[string]densityReviewAdmission,
) []GuardrailFinding {
	recordsByPath := make(map[string]PackageRecord, len(records))
	productionFilesByPath := make(map[string]PackageRecord)
	for _, record := range records {
		packagePath, ok := internalPath(record.ImportPath)
		if !ok {
			continue
		}
		recordsByPath[packagePath] = record
		for _, fileName := range densityProductionFiles(record) {
			filePath := filepath.ToSlash(filepath.Join(packagePath, fileName))
			productionFilesByPath[filePath] = record
		}
	}

	var findings []GuardrailFinding
	for _, packagePath := range sortedAdmissionPaths(packageAdmissions) {
		admission := packageAdmissions[packagePath]
		if err := admission.validate("production files"); err != nil {
			findings = append(findings, densityAdmissionInvalidFinding(packagePath, packagePath, err.Error()).finding)
			continue
		}
		record, present := recordsByPath[packagePath]
		if !present {
			findings = append(findings, densityAdmissionInvalidFinding(
				packagePath,
				packagePath,
				"reviewed package is missing or build-excluded",
			).finding)
			continue
		}
		current := len(densityProductionFiles(record))
		if current < admission.reviewedValue {
			findings = append(findings, densityAdmissionInvalidFinding(
				packagePath,
				packagePath,
				fmt.Sprintf("reviewed production file count %d is stale; current count is %d", admission.reviewedValue, current),
			).finding)
		}
	}

	for _, filePath := range sortedAdmissionPaths(fileAdmissions) {
		admission := fileAdmissions[filePath]
		packagePath := filepath.ToSlash(filepath.Dir(filePath))
		if err := admission.validate("production lines"); err != nil {
			findings = append(findings, densityAdmissionInvalidFinding(packagePath, filePath, err.Error()).finding)
			continue
		}
		record, present := productionFilesByPath[filePath]
		if !present {
			findings = append(findings, densityAdmissionInvalidFinding(
				packagePath,
				filePath,
				"reviewed production file is missing or build-excluded",
			).finding)
			continue
		}
		current := lineCount(record, filepath.Base(filePath))
		if current < admission.reviewedValue {
			findings = append(findings, densityAdmissionInvalidFinding(
				packagePath,
				filePath,
				fmt.Sprintf("reviewed production line count %d is stale; current count is %d", admission.reviewedValue, current),
			).finding)
		}
	}

	return sortedFindings(dedupFindings(findings))
}

func sortedAdmissionPaths(admissions map[string]densityReviewAdmission) []string {
	paths := make([]string, 0, len(admissions))
	for path := range admissions {
		paths = append(paths, path)
	}
	return sortedStrings(paths)
}

func lineCount(record PackageRecord, fileName string) int {
	if record.FileLineCounts != nil {
		return record.FileLineCounts[fileName]
	}
	if record.Dir == "" {
		return 0
	}
	content, err := os.ReadFile(filepath.Join(record.Dir, fileName))
	if err != nil {
		return 0
	}
	lines := bytes.Count(content, []byte{'\n'})
	if len(content) != 0 && content[len(content)-1] != '\n' {
		lines++
	}
	return lines
}
