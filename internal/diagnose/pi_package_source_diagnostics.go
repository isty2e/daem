package diagnose

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/findings"
	targetpkg "github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func PiPackageSourceDiagnostics(extensions []desiredextension.Extension, selection targetselection.Selection) ([]findings.Diagnostic, error) {
	diagnostics := make([]findings.Diagnostic, 0)
	for _, value := range extensions {
		if value.Carrier() != desiredextension.CarrierPiPackage || !selection.Includes(value.Target()) {
			continue
		}
		difference, err := extensiontopology.PiPackageSourceInterpretationDifference(value.CarrierKey())
		if err != nil {
			return nil, fmt.Errorf("classify Pi package source: %w", err)
		}
		if difference == extensiontopology.PiSourceDifferenceNone {
			continue
		}

		diagnostics = append(diagnostics, newPiPackageSourceDiagnostic(difference, value.ID(), value.Target(), value.Scope()))
	}
	return diagnostics, nil
}

func newPiPackageSourceDiagnostic(difference extensiontopology.PiSourceInterpretationDifference, id entity.ID, target targetpkg.Target, scope targetpkg.Scope) findings.Diagnostic {
	var code, detail string
	switch difference {
	case extensiontopology.PiSourceDifferenceGitHubShorthand:
		code = "extension.pi_source.bare_github"
		detail = "bare github: shorthand has a documented local-path interpretation in Pi, unlike daem's generic Git classification; for Git intent use git:host/owner/repo or a lowercase https:// URL; daem does not convert the source"
	case extensiontopology.PiSourceDifferenceGitPlusURL:
		code = "extension.pi_source.git_plus"
		detail = "a bare git+ transport URL has a documented local-path interpretation in Pi, unlike daem's generic Git classification; for Git intent use git:host/owner/repo or a lowercase https:// URL; daem does not convert the source"
	}

	return findings.Diagnostic{
		Severity: findings.SeverityWarn,
		Code:     code,
		EntityID: id,
		Target:   target,
		Scope:    scope,
		Detail:   detail,
	}
}
