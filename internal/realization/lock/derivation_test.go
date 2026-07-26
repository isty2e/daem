package lock

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestDeterministicTransformRequiresExpectedOutputIdentity(t *testing.T) {
	_, err := NewDeterministicTransformDerivation(DeterministicTransform{
		InputIdentity:          mustExactArtifactIdentity(t, "local:skill", "rev", "sha256:input"),
		RecipeHash:             "sha256:recipe",
		AlgorithmID:            "compat.skill.repair",
		AlgorithmVersion:       "v1",
		ExecutionDomain:        "daem:compat/skill/repair",
		ExpectedOutputIdentity: artifact.ExactIdentity{},
	})
	if err == nil || !strings.Contains(err.Error(), "expected output identity: exact artifact source id is required") {
		t.Fatalf("NewDeterministicTransformDerivation error = %v, want expected output identity diagnostic", err)
	}
}

func TestDirectResolutionDerivationOwnsExactIdentityOnly(t *testing.T) {
	identity := mustExactArtifactIdentity(t, "local:skill", "rev", "sha256:artifact")
	derivation, err := NewDirectResolutionDerivation(identity)
	if err != nil {
		t.Fatalf("NewDirectResolutionDerivation returned error: %v", err)
	}
	got, ok := derivation.DirectResolution()
	if !ok {
		t.Fatal("DirectResolution returned false")
	}
	if !got.Equal(identity) {
		t.Fatalf("direct resolution identity = %#v, want %#v", got, identity)
	}
	if _, ok := derivation.DeterministicTransform(); ok {
		t.Fatal("direct resolution derivation exposed deterministic transform body")
	}
}

func mustExactArtifactIdentity(t *testing.T, sourceID string, resolvedRef string, contentHash string) artifact.ExactIdentity {
	t.Helper()
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID(sourceID),
		artifact.ResolvedRef(resolvedRef),
		artifact.ArtifactKindDirectory,
		testExactHash(contentHash),
	)
	if err != nil {
		t.Fatalf("artifact.NewExactIdentity returned error: %v", err)
	}
	return identity
}
