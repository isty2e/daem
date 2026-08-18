package contribution

import (
	"slices"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestSourceContributionObservationCorrelatesCanonicalProviderAndContributions(t *testing.T) {
	provider := sourceModelProvider(t, "alpha@market")
	skill := sourceModelContribution(t, SourceContributionSkill, "review")
	mcp := sourceModelContribution(t, SourceContributionMCPServer, "context7")

	observation, err := NewSourceContributionObservation(SourceContributionObservationSpec{
		Provider:         provider,
		ProviderLabel:    "alpha@market",
		State:            SourceContributionDeclared,
		ArtifactIdentity: "plugins/cache/market/alpha/local",
		Contributions:    []SourceContribution{skill, mcp},
	})
	if err != nil {
		t.Fatalf("NewSourceContributionObservation returned error: %v", err)
	}

	rows := observation.DiagnosticRows()
	if len(rows) != 2 {
		t.Fatalf("DiagnosticRows = %#v, want two", rows)
	}
	for _, row := range rows {
		providerSubject, providerOK := row.ProviderSubject()
		contributionSubject, contributionOK := row.ContributionSubject()
		if !providerOK || providerSubject != provider.SubjectID() {
			t.Fatalf("row provider = %s/%t, want %s", providerSubject, providerOK, provider.SubjectID())
		}
		if !contributionOK || contributionSubject.Kind() != topology.SubjectContribution {
			t.Fatalf("row contribution = %s/%t, want canonical contribution", contributionSubject, contributionOK)
		}
	}
}

func TestSourceContributionObservationRejectsMissingMismatchedAndDuplicateTopology(t *testing.T) {
	provider := sourceModelProvider(t, "alpha@market")
	contribution := sourceModelContribution(t, SourceContributionSkill, "review")

	cases := []struct {
		name string
		spec SourceContributionObservationSpec
		want string
	}{
		{
			name: "declared missing provider",
			spec: SourceContributionObservationSpec{
				ProviderLabel:    "alpha@market",
				State:            SourceContributionDeclared,
				ArtifactIdentity: "artifact",
			},
			want: "requires canonical topology",
		},
		{
			name: "provider label mismatch",
			spec: SourceContributionObservationSpec{
				Provider:         provider,
				ProviderLabel:    "beta@market",
				State:            SourceContributionDeclared,
				ArtifactIdentity: "artifact",
			},
			want: "does not match canonical provider source",
		},
		{
			name: "duplicate provider contribution",
			spec: SourceContributionObservationSpec{
				Provider:         provider,
				ProviderLabel:    "alpha@market",
				State:            SourceContributionDeclared,
				ArtifactIdentity: "artifact",
				Contributions:    []SourceContribution{contribution, contribution},
			},
			want: "duplicate",
		},
		{
			name: "non-provenance blocker missing provider",
			spec: SourceContributionObservationSpec{
				ProviderLabel: "alpha@market",
				State:         SourceContributionBlocked,
				Reason:        SourceContributionReasonArtifactMalformed,
			},
			want: "lacks canonical topology",
		},
		{
			name: "unavailable with malformed reason",
			spec: SourceContributionObservationSpec{
				Provider:      provider,
				ProviderLabel: "alpha@market",
				State:         SourceContributionUnavailable,
				Reason:        SourceContributionReasonArtifactMalformed,
			},
			want: "does not admit reason",
		},
		{
			name: "known provider with provenance blocker",
			spec: SourceContributionObservationSpec{
				Provider:      provider,
				ProviderLabel: "alpha@market",
				State:         SourceContributionBlocked,
				Reason:        SourceContributionReasonProviderProvenanceRequired,
			},
			want: "does not admit reason",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewSourceContributionObservation(testCase.spec)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want substring %q", err, testCase.want)
			}
		})
	}
}

func TestSourceContributionObservationAllowsOnlyProvenanceBlockerWithoutProvider(t *testing.T) {
	observation, err := NewSourceContributionObservation(SourceContributionObservationSpec{
		ProviderLabel: "<invalid-provider>",
		State:         SourceContributionBlocked,
		Reason:        SourceContributionReasonProviderProvenanceRequired,
	})
	if err != nil {
		t.Fatalf("NewSourceContributionObservation returned error: %v", err)
	}
	row := observation.DiagnosticRows()[0]
	if _, ok := row.ProviderSubject(); ok {
		t.Fatal("ProviderSubject unexpectedly available")
	}
	if _, ok := row.ContributionSubject(); ok {
		t.Fatal("ContributionSubject unexpectedly available")
	}
}

func TestSourceContributionObservationEnforcesCompleteStateReasonMatrix(t *testing.T) {
	provider := sourceModelProvider(t, "alpha@market")
	contribution := sourceModelContribution(t, SourceContributionSkill, "review")
	valid := []SourceContributionObservationSpec{
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionDeclared,
			ArtifactIdentity: "artifact",
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionUnavailable,
			Reason: SourceContributionReasonArtifactUnavailable,
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionAmbiguous,
			Reason: SourceContributionReasonArtifactAmbiguous,
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionBlocked,
			Reason: SourceContributionReasonArtifactMalformed,
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionBlocked,
			Reason: SourceContributionReasonArtifactPathBlocked,
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionBlocked,
			Reason: SourceContributionReasonArtifactUnstable,
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionBlocked,
			Reason: SourceContributionReasonArtifactBudgetExceeded,
		},
		{
			ProviderLabel: "<invalid-provider>", State: SourceContributionBlocked,
			Reason: SourceContributionReasonProviderProvenanceRequired,
		},
	}
	for index, spec := range valid {
		if _, err := NewSourceContributionObservation(spec); err != nil {
			t.Fatalf("valid state/reason[%d] rejected: %v", index, err)
		}
	}

	invalid := []SourceContributionObservationSpec{
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionDeclared,
			Reason: SourceContributionReasonArtifactUnavailable, ArtifactIdentity: "artifact",
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionUnavailable,
			Reason: SourceContributionReasonArtifactAmbiguous,
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionAmbiguous,
			Reason: SourceContributionReasonArtifactUnavailable,
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionBlocked,
			Reason: SourceContributionReasonArtifactUnavailable,
		},
		{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionBlocked,
			Reason: SourceContributionReasonArtifactMalformed, Contributions: []SourceContribution{contribution},
		},
	}
	for index, spec := range invalid {
		if _, err := NewSourceContributionObservation(spec); err == nil {
			t.Fatalf("invalid state/reason[%d] accepted: %#v", index, spec)
		}
	}
}

func TestSourceContributionObservationKeepsSourceMarkerOutOfStructuralIdentity(t *testing.T) {
	provider := sourceModelProvider(t, "alpha@market")
	first, err := NewSourceContribution(SourceContributionSpec{
		Kind: SourceContributionSkill, Key: "review", SourceMarker: "skills/review/SKILL.md",
	})
	if err != nil {
		t.Fatalf("NewSourceContribution(first): %v", err)
	}
	second, err := NewSourceContribution(SourceContributionSpec{
		Kind: SourceContributionSkill, Key: "review", SourceMarker: "alternate/review/SKILL.md",
	})
	if err != nil {
		t.Fatalf("NewSourceContribution(second): %v", err)
	}

	subjectFor := func(contribution SourceContribution) topology.SubjectID {
		t.Helper()
		observation, observationErr := NewSourceContributionObservation(SourceContributionObservationSpec{
			Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionDeclared,
			ArtifactIdentity: "artifact", Contributions: []SourceContribution{contribution},
		})
		if observationErr != nil {
			t.Fatalf("NewSourceContributionObservation: %v", observationErr)
		}
		row := observation.DiagnosticRows()[0]
		if row.Currentness() != SourceContributionNonCurrent ||
			row.Freshness() != SourceContributionFresh ||
			row.Provenance() != SourceContributionProvenanceArtifactInspection {
			t.Fatalf("diagnostic strength = %q/%q/%q, want non-current/fresh/source inspection", row.Currentness(), row.Freshness(), row.Provenance())
		}
		subject, ok := row.ContributionSubject()
		if !ok {
			t.Fatal("diagnostic row lacks canonical contribution subject")
		}
		return subject
	}
	if firstSubject, secondSubject := subjectFor(first), subjectFor(second); firstSubject != secondSubject {
		t.Fatalf("source marker leaked into identity: %s / %s", firstSubject, secondSubject)
	}
}

func TestSourceContributionObservationIsInputOrderIndependent(t *testing.T) {
	provider := sourceModelProvider(t, "alpha@market")
	first := sourceModelContribution(t, SourceContributionSkill, "review")
	second := sourceModelContribution(t, SourceContributionMCPServer, "context7")

	forward, err := NewSourceContributionObservation(SourceContributionObservationSpec{
		Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionDeclared,
		ArtifactIdentity: "artifact", Contributions: []SourceContribution{first, second},
	})
	if err != nil {
		t.Fatalf("forward observation: %v", err)
	}
	reverse, err := NewSourceContributionObservation(SourceContributionObservationSpec{
		Provider: provider, ProviderLabel: "alpha@market", State: SourceContributionDeclared,
		ArtifactIdentity: "artifact", Contributions: []SourceContribution{second, first},
	})
	if err != nil {
		t.Fatalf("reverse observation: %v", err)
	}
	if got, want := forward.DiagnosticRows(), reverse.DiagnosticRows(); !slices.Equal(got, want) {
		t.Fatalf("diagnostic row order depends on input order: %#v / %#v", got, want)
	}
}

func TestValidSourceTokenRejectsUnicodeControlsAndInvalidUTF8(t *testing.T) {
	for _, value := range []string{
		"alpha\u202emarket",
		"alpha\u2066market",
		string([]byte{'a', 0xff, 'b'}),
	} {
		if ValidSourceToken(value) {
			t.Fatalf("ValidSourceToken(%q) = true, want false", value)
		}
	}
}

func sourceModelProvider(t *testing.T, label string) extensiontopology.Carrier {
	t.Helper()
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindMarketplace, label)
	if err != nil {
		t.Fatalf("NewSourceRef(%q): %v", label, err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey(%q): %v", label, err)
	}
	provider, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatalf("NewCarrier(%q): %v", label, err)
	}
	return provider
}

func sourceModelContribution(
	t *testing.T,
	kind SourceContributionKind,
	key string,
) SourceContribution {
	t.Helper()
	contribution, err := NewSourceContribution(SourceContributionSpec{
		Kind:         kind,
		Key:          key,
		SourceMarker: string(kind) + "/" + key,
	})
	if err != nil {
		t.Fatalf("NewSourceContribution(%s/%s): %v", kind, key, err)
	}
	return contribution
}
