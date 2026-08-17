package contribution

import (
	"fmt"
	"sort"

	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// SourceContributionState classifies source-artifact contribution inspection.
type SourceContributionState string

const (
	SourceContributionDeclared    SourceContributionState = "source-declared"
	SourceContributionUnavailable SourceContributionState = "source-artifact-unavailable"
	SourceContributionAmbiguous   SourceContributionState = "source-artifact-ambiguous"
	SourceContributionBlocked     SourceContributionState = "source-artifact-blocked"
)

// SourceContributionReason is a stable source-artifact inspection reason.
type SourceContributionReason string

const (
	SourceContributionReasonNone                       SourceContributionReason = ""
	SourceContributionReasonProviderProvenanceRequired SourceContributionReason = "PROVIDER_PROVENANCE_REQUIRED"
	SourceContributionReasonArtifactUnavailable        SourceContributionReason = "SOURCE_ARTIFACT_UNAVAILABLE"
	SourceContributionReasonArtifactAmbiguous          SourceContributionReason = "SOURCE_ARTIFACT_AMBIGUOUS"
	SourceContributionReasonArtifactMalformed          SourceContributionReason = "SOURCE_ARTIFACT_MALFORMED"
	SourceContributionReasonArtifactPathBlocked        SourceContributionReason = "SOURCE_ARTIFACT_PATH_BLOCKED"
	SourceContributionReasonArtifactUnstable           SourceContributionReason = "SOURCE_ARTIFACT_UNSTABLE"
	SourceContributionReasonArtifactBudgetExceeded     SourceContributionReason = "SOURCE_ARTIFACT_BUDGET_EXCEEDED"
	SourceContributionReasonUnsupportedShape           SourceContributionReason = "UNSUPPORTED_CONTRIBUTION_SHAPE"
)

// SourceContributionObservationSpec contains source-artifact inspection facts.
type SourceContributionObservationSpec struct {
	Provider         extensiontopology.Carrier
	ProviderLabel    SourceProviderLabel
	State            SourceContributionState
	Reason           SourceContributionReason
	ArtifactIdentity string
	Contributions    []SourceContribution
}

// SourceContributionObservation is a non-current provider contribution source-inspection fact.
type SourceContributionObservation struct {
	provider         extensiontopology.Carrier
	hasProvider      bool
	providerLabel    SourceProviderLabel
	state            SourceContributionState
	reason           SourceContributionReason
	artifactIdentity string
	contributions    []SourceContribution
	topology         extensiontopology.ContributionModel
}

// NewSourceContributionObservation validates and constructs a source-inspection fact.
func NewSourceContributionObservation(spec SourceContributionObservationSpec) (SourceContributionObservation, error) {
	if !ValidSourceToken(string(spec.ProviderLabel)) {
		return SourceContributionObservation{}, fmt.Errorf("source contribution provider label is unsafe")
	}
	hasProvider := !spec.Provider.SubjectID().IsZero()
	if hasProvider {
		if err := spec.Provider.Validate(); err != nil {
			return SourceContributionObservation{}, fmt.Errorf("source contribution provider: %w", err)
		}
		if string(spec.ProviderLabel) != spec.Provider.Source().Ref() {
			return SourceContributionObservation{}, fmt.Errorf(
				"source contribution provider label %q does not match canonical provider source %q",
				spec.ProviderLabel,
				spec.Provider.Source().Ref(),
			)
		}
	}
	if !validSourceContributionState(spec.State) {
		return SourceContributionObservation{}, fmt.Errorf("source contribution provider %q has unsupported state %q", spec.ProviderLabel, spec.State)
	}
	if !validSourceContributionReason(spec.Reason) {
		return SourceContributionObservation{}, fmt.Errorf("source contribution provider %q has unsupported reason %q", spec.ProviderLabel, spec.Reason)
	}

	switch spec.State {
	case SourceContributionDeclared:
		if !hasProvider {
			return SourceContributionObservation{}, fmt.Errorf("source-declared contribution provider %q requires canonical topology", spec.ProviderLabel)
		}
		if spec.Reason != SourceContributionReasonNone {
			return SourceContributionObservation{}, fmt.Errorf("source-declared contribution provider %q cannot carry reason %q", spec.ProviderLabel, spec.Reason)
		}
		if !ValidSourceToken(spec.ArtifactIdentity) {
			return SourceContributionObservation{}, fmt.Errorf("source-declared contribution provider %q has unsafe artifact identity", spec.ProviderLabel)
		}
	case SourceContributionUnavailable, SourceContributionAmbiguous, SourceContributionBlocked:
		if spec.Reason == SourceContributionReasonNone {
			return SourceContributionObservation{}, fmt.Errorf("blocked contribution provider %q requires a reason", spec.ProviderLabel)
		}
		if spec.ArtifactIdentity != "" && !ValidSourceToken(spec.ArtifactIdentity) {
			return SourceContributionObservation{}, fmt.Errorf("blocked contribution provider %q has unsafe artifact identity", spec.ProviderLabel)
		}
		if len(spec.Contributions) != 0 {
			return SourceContributionObservation{}, fmt.Errorf("blocked contribution provider %q cannot carry contributions", spec.ProviderLabel)
		}
		if !hasProvider && spec.Reason != SourceContributionReasonProviderProvenanceRequired {
			return SourceContributionObservation{}, fmt.Errorf(
				"source contribution provider %q lacks canonical topology for reason %q",
				spec.ProviderLabel,
				spec.Reason,
			)
		}
	}
	if !sourceContributionStateAdmitsReason(spec.State, spec.Reason, hasProvider) {
		return SourceContributionObservation{}, fmt.Errorf(
			"source contribution provider %q state %q does not admit reason %q with canonical_provider=%t",
			spec.ProviderLabel,
			spec.State,
			spec.Reason,
			hasProvider,
		)
	}

	contributions := append([]SourceContribution(nil), spec.Contributions...)
	sort.Slice(contributions, func(left int, right int) bool {
		switch {
		case contributions[left].Kind() != contributions[right].Kind():
			return contributions[left].Kind() < contributions[right].Kind()
		case contributions[left].Key() != contributions[right].Key():
			return contributions[left].Key() < contributions[right].Key()
		default:
			return contributions[left].SourceMarker() < contributions[right].SourceMarker()
		}
	})

	var contributionTopology extensiontopology.ContributionModel
	if hasProvider {
		contributionSpecs := make([]extensiontopology.ContributionSpec, 0, len(contributions))
		for _, contribution := range contributions {
			contributionSpecs = append(contributionSpecs, extensiontopology.ContributionSpec{
				Kind: string(contribution.Kind()),
				Key:  contribution.Key(),
			})
		}
		var err error
		contributionTopology, err = extensiontopology.ProviderContributions(spec.Provider, contributionSpecs)
		if err != nil {
			return SourceContributionObservation{}, fmt.Errorf(
				"source contribution provider %q topology: %w",
				spec.ProviderLabel,
				err,
			)
		}
	}
	return SourceContributionObservation{
		provider:         spec.Provider,
		hasProvider:      hasProvider,
		providerLabel:    spec.ProviderLabel,
		state:            spec.State,
		reason:           spec.Reason,
		artifactIdentity: spec.ArtifactIdentity,
		contributions:    contributions,
		topology:         contributionTopology,
	}, nil
}

func sourceContributionStateAdmitsReason(
	state SourceContributionState,
	reason SourceContributionReason,
	hasProvider bool,
) bool {
	switch state {
	case SourceContributionDeclared:
		return reason == SourceContributionReasonNone && hasProvider
	case SourceContributionUnavailable:
		return reason == SourceContributionReasonArtifactUnavailable && hasProvider
	case SourceContributionAmbiguous:
		return reason == SourceContributionReasonArtifactAmbiguous && hasProvider
	case SourceContributionBlocked:
		if reason == SourceContributionReasonProviderProvenanceRequired {
			return !hasProvider
		}
		switch reason {
		case SourceContributionReasonArtifactMalformed,
			SourceContributionReasonArtifactPathBlocked,
			SourceContributionReasonArtifactUnstable,
			SourceContributionReasonArtifactBudgetExceeded,
			SourceContributionReasonUnsupportedShape:
			return hasProvider
		default:
			return false
		}
	default:
		return false
	}
}

func validSourceContributionState(state SourceContributionState) bool {
	switch state {
	case SourceContributionDeclared,
		SourceContributionUnavailable,
		SourceContributionAmbiguous,
		SourceContributionBlocked:
		return true
	default:
		return false
	}
}

func validSourceContributionReason(reason SourceContributionReason) bool {
	switch reason {
	case SourceContributionReasonNone,
		SourceContributionReasonProviderProvenanceRequired,
		SourceContributionReasonArtifactUnavailable,
		SourceContributionReasonArtifactAmbiguous,
		SourceContributionReasonArtifactMalformed,
		SourceContributionReasonArtifactPathBlocked,
		SourceContributionReasonArtifactUnstable,
		SourceContributionReasonArtifactBudgetExceeded,
		SourceContributionReasonUnsupportedShape:
		return true
	default:
		return false
	}
}
