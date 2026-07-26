package extension_test

import (
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestProviderContributionsBuildsCanonicalProviderScopedModel(t *testing.T) {
	provider := contributionProvider(t, "alpha@market")
	model, err := extensiontopology.ProviderContributions(provider, []extensiontopology.ContributionSpec{
		{Kind: "skill", Key: "review"},
		{Kind: "mcp-server", Key: "context7"},
	})
	if err != nil {
		t.Fatalf("ProviderContributions returned error: %v", err)
	}
	contributions := model.Contributions()
	if len(contributions) != 2 {
		t.Fatalf("Contributions = %#v, want two contributions", contributions)
	}
	for _, contribution := range contributions {
		if contribution.SubjectID().Kind() != topology.SubjectContribution || contribution.Validate() != nil {
			t.Fatalf("contribution = %#v, want valid provider-scoped identity", contribution)
		}
	}
	contributions[0] = extensiontopology.Contribution{}
	if model.Contributions()[0].SubjectID().IsZero() {
		t.Fatal("Contributions returned mutable model storage")
	}
}

func TestProviderContributionsKeepsEqualVisibleKeysDistinctAcrossProviders(t *testing.T) {
	alpha := contributionProvider(t, "alpha@market")
	beta := contributionProvider(t, "beta@market")
	spec := []extensiontopology.ContributionSpec{{Kind: "skill", Key: "review"}}

	alphaModel, err := extensiontopology.ProviderContributions(alpha, spec)
	if err != nil {
		t.Fatalf("ProviderContributions(alpha): %v", err)
	}
	betaModel, err := extensiontopology.ProviderContributions(beta, spec)
	if err != nil {
		t.Fatalf("ProviderContributions(beta): %v", err)
	}
	alphaContribution := alphaModel.Contributions()[0]
	betaContribution := betaModel.Contributions()[0]
	if alphaContribution.SubjectID() == betaContribution.SubjectID() {
		t.Fatalf(
			"provider-scoped contribution identities collapsed: alpha=%s beta=%s",
			alphaContribution.SubjectID(),
			betaContribution.SubjectID(),
		)
	}
}

func TestProviderContributionsIsOrderIndependentAndRejectsDuplicates(t *testing.T) {
	provider := contributionProvider(t, "alpha@market")
	first := extensiontopology.ContributionSpec{Kind: "skill", Key: "review"}
	second := extensiontopology.ContributionSpec{Kind: "mcp-server", Key: "context7"}
	forward, err := extensiontopology.ProviderContributions(provider, []extensiontopology.ContributionSpec{first, second})
	if err != nil {
		t.Fatalf("ProviderContributions(forward): %v", err)
	}
	reverse, err := extensiontopology.ProviderContributions(provider, []extensiontopology.ContributionSpec{second, first})
	if err != nil {
		t.Fatalf("ProviderContributions(reverse): %v", err)
	}
	forwardContributions := forward.Contributions()
	reverseContributions := reverse.Contributions()
	if len(forwardContributions) != len(reverseContributions) {
		t.Fatalf("contribution count depends on input order: forward=%v reverse=%v", forwardContributions, reverseContributions)
	}
	for index := range forwardContributions {
		if forwardContributions[index].SubjectID() != reverseContributions[index].SubjectID() {
			t.Fatalf("contribution order depends on input order: forward=%v reverse=%v", forwardContributions, reverseContributions)
		}
	}

	_, forwardErr := extensiontopology.ProviderContributions(provider, []extensiontopology.ContributionSpec{first, first, second})
	_, reverseErr := extensiontopology.ProviderContributions(provider, []extensiontopology.ContributionSpec{second, first, first})
	if forwardErr == nil || reverseErr == nil ||
		forwardErr.Error() != reverseErr.Error() ||
		!strings.Contains(forwardErr.Error(), "duplicate") {
		t.Fatalf("duplicate failure is unstable: %v / %v", forwardErr, reverseErr)
	}
}

func TestProviderContributionIdentityIgnoresDiagnosticSourceMarker(t *testing.T) {
	provider := contributionProvider(t, "alpha@market")
	first, err := extensiontopology.ProviderContributions(provider, []extensiontopology.ContributionSpec{
		{Kind: "skill", Key: "review"},
	})
	if err != nil {
		t.Fatalf("ProviderContributions(first): %v", err)
	}
	second, err := extensiontopology.ProviderContributions(provider, []extensiontopology.ContributionSpec{
		{Kind: "skill", Key: "review"},
	})
	if err != nil {
		t.Fatalf("ProviderContributions(second): %v", err)
	}
	if first.Contributions()[0].SubjectID() != second.Contributions()[0].SubjectID() {
		t.Fatalf(
			"equal provider/kind/key produced unstable identities: %s / %s",
			first.Contributions()[0].SubjectID(),
			second.Contributions()[0].SubjectID(),
		)
	}

	distinctKind, err := extensiontopology.ProviderContributions(provider, []extensiontopology.ContributionSpec{
		{Kind: "hook", Key: "review"},
	})
	if err != nil {
		t.Fatalf("ProviderContributions(distinct kind): %v", err)
	}
	if first.Contributions()[0].SubjectID() == distinctKind.Contributions()[0].SubjectID() {
		t.Fatal("contribution identity collapsed equal keys across distinct kinds")
	}
}

func TestProviderContributionsRejectsUnsafeIdentityAndZeroProvider(t *testing.T) {
	provider := contributionProvider(t, "alpha@market")
	for _, spec := range []extensiontopology.ContributionSpec{
		{Kind: "", Key: "review"},
		{Kind: "skill", Key: ""},
		{Kind: " skill", Key: "review"},
		{Kind: "skill", Key: "review\nsecret"},
		{Kind: "skill", Key: "review\u202e"},
		{Kind: "skill", Key: string([]byte{'r', 0xff})},
	} {
		if _, err := extensiontopology.NewContribution(provider, spec); err == nil {
			t.Fatalf("NewContribution accepted unsafe spec %#v", spec)
		}
	}
	if _, err := extensiontopology.ProviderContributions(extensiontopology.Carrier{}, nil); err == nil {
		t.Fatal("ProviderContributions accepted zero provider")
	}
}

func contributionProvider(t *testing.T, sourceRef string) extensiontopology.Carrier {
	t.Helper()
	source := desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, sourceRef)
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey: %v", err)
	}
	provider, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatalf("NewCarrier: %v", err)
	}
	return provider
}
