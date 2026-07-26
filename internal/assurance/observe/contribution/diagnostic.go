package contribution

import "github.com/isty2e/daem/internal/topology"

// SourceContributionProvenance records how a diagnostic contribution fact was collected.
type SourceContributionProvenance string

const (
	// SourceContributionProvenanceArtifactInspection means a bounded provider source artifact was read.
	SourceContributionProvenanceArtifactInspection SourceContributionProvenance = "source_artifact_inspection"
)

// SourceContributionCurrentness records whether a contribution fact is current host inventory.
type SourceContributionCurrentness string

const (
	// SourceContributionNonCurrent means the row is not loaded/current host inventory.
	SourceContributionNonCurrent SourceContributionCurrentness = "non-current"
)

// SourceContributionFreshness records whether the diagnostic evidence itself is fresh.
type SourceContributionFreshness string

const (
	// SourceContributionFresh means the diagnostic source was checked in the current run.
	SourceContributionFresh SourceContributionFreshness = "fresh"
)

// SourceContributionDiagnosticRow is one provider-scoped diagnostic row.
type SourceContributionDiagnosticRow struct {
	providedBy          SourceProviderLabel
	providerSubject     topology.SubjectID
	contributionSubject topology.SubjectID
	kind                SourceContributionKind
	key                 string
	sourceMarker        string
	provenance          SourceContributionProvenance
	currentness         SourceContributionCurrentness
	freshness           SourceContributionFreshness
	artifactIdentity    string
	state               SourceContributionState
	reason              SourceContributionReason
	hasContribution     bool
}

// DiagnosticRows returns provider-scoped source-artifact diagnostic rows.
func (observation SourceContributionObservation) DiagnosticRows() []SourceContributionDiagnosticRow {
	if len(observation.contributions) == 0 {
		return []SourceContributionDiagnosticRow{observation.diagnosticRow(SourceContribution{}, topology.SubjectID{}, false)}
	}
	subjects := make(map[contributionIdentity]topology.SubjectID, len(observation.contributions))
	for _, structural := range observation.topology.Contributions() {
		subjects[contributionIdentity{kind: structural.Kind(), key: structural.Key()}] = structural.SubjectID()
	}
	rows := make([]SourceContributionDiagnosticRow, 0, len(observation.contributions))
	for _, contribution := range observation.contributions {
		subject := subjects[contributionIdentity{kind: string(contribution.Kind()), key: contribution.Key()}]
		rows = append(rows, observation.diagnosticRow(contribution, subject, true))
	}
	return rows
}

type contributionIdentity struct {
	kind string
	key  string
}

func (observation SourceContributionObservation) diagnosticRow(
	contribution SourceContribution,
	contributionSubject topology.SubjectID,
	hasContribution bool,
) SourceContributionDiagnosticRow {
	var providerSubject topology.SubjectID
	if observation.hasProvider {
		providerSubject = observation.provider.SubjectID()
	}
	return SourceContributionDiagnosticRow{
		providedBy:          observation.providerLabel,
		providerSubject:     providerSubject,
		contributionSubject: contributionSubject,
		kind:                contribution.Kind(),
		key:                 contribution.Key(),
		sourceMarker:        contribution.SourceMarker(),
		provenance:          SourceContributionProvenanceArtifactInspection,
		currentness:         SourceContributionNonCurrent,
		freshness:           SourceContributionFresh,
		artifactIdentity:    observation.artifactIdentity,
		state:               observation.state,
		reason:              observation.reason,
		hasContribution:     hasContribution,
	}
}

// ProvidedBy returns the redaction-safe provider label for this row.
func (row SourceContributionDiagnosticRow) ProvidedBy() SourceProviderLabel { return row.providedBy }

// ProviderSubject returns canonical provider topology when provenance was valid.
func (row SourceContributionDiagnosticRow) ProviderSubject() (topology.SubjectID, bool) {
	return row.providerSubject, !row.providerSubject.IsZero()
}

// ContributionSubject returns canonical provider-scoped contribution topology
// when this row names a concrete contribution.
func (row SourceContributionDiagnosticRow) ContributionSubject() (topology.SubjectID, bool) {
	return row.contributionSubject, !row.contributionSubject.IsZero()
}

// HasContribution reports whether this row names a concrete contribution.
func (row SourceContributionDiagnosticRow) HasContribution() bool { return row.hasContribution }

// Kind returns the provider-scoped contribution kind.
func (row SourceContributionDiagnosticRow) Kind() SourceContributionKind { return row.kind }

// Key returns the contribution key inside the provider namespace.
func (row SourceContributionDiagnosticRow) Key() string { return row.key }

// SourceMarker returns the bounded source-artifact marker for the row.
func (row SourceContributionDiagnosticRow) SourceMarker() string { return row.sourceMarker }

// Provenance returns how the diagnostic fact was collected.
func (row SourceContributionDiagnosticRow) Provenance() SourceContributionProvenance {
	return row.provenance
}

// Currentness returns whether this row is current host inventory.
func (row SourceContributionDiagnosticRow) Currentness() SourceContributionCurrentness {
	return row.currentness
}

// Freshness returns freshness of the diagnostic evidence itself.
func (row SourceContributionDiagnosticRow) Freshness() SourceContributionFreshness {
	return row.freshness
}

// ArtifactIdentity returns the bounded source artifact identity.
func (row SourceContributionDiagnosticRow) ArtifactIdentity() string { return row.artifactIdentity }

// State returns the source-artifact inspection state for this row.
func (row SourceContributionDiagnosticRow) State() SourceContributionState { return row.state }

// Reason returns the source-artifact blocker reason for this row.
func (row SourceContributionDiagnosticRow) Reason() SourceContributionReason { return row.reason }
