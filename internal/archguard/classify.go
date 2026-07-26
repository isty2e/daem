package archguard

func classifyFindings(rawFindings []rawFinding) Report {
	report := Report{}
	for _, raw := range rawFindings {
		switch raw.disposition {
		case findingDispositionViolation:
			report.Violations = append(report.Violations, raw.finding)
		case findingDispositionReviewRequired:
			report.DensityReviewRequirements = append(report.DensityReviewRequirements, raw.finding)
		case findingDispositionWarning:
			report.DensityWarnings = append(report.DensityWarnings, raw.finding)
		}
	}
	report.Violations = sortedFindings(dedupFindings(report.Violations))
	report.DensityReviewRequirements = sortedFindings(dedupFindings(report.DensityReviewRequirements))
	report.DensityWarnings = sortedFindings(dedupFindings(report.DensityWarnings))
	return report
}
