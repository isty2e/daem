package pathauthority_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestProvisionalRequiresNormalizationSensitiveDarwinAuthority(t *testing.T) {
	namespace := filepath.Join(string(filepath.Separator), "tmp", "daem", "skills")
	nonASCII := filepath.Join(namespace, "Caf\u00e9")
	ASCII := filepath.Join(namespace, "Cafe")
	darwinNamespace := pathtest.DarwinCaseSensitive(namespace)
	darwinCandidate := pathtest.DarwinCaseSensitive(nonASCII)

	tests := []struct {
		name             string
		candidate        string
		candidateWitness string
		namespaceWitness string
	}{
		{
			name:             "generic exact candidate",
			candidate:        nonASCII,
			candidateWitness: "exact-v1:",
			namespaceWitness: darwinNamespace.Witness(),
		},
		{
			name:             "generic exact namespace",
			candidate:        nonASCII,
			candidateWitness: darwinCandidate.Witness(),
			namespaceWitness: "exact-v1:",
		},
		{
			name:             "contradictory ancestor semantics",
			candidate:        nonASCII,
			candidateWitness: darwinCandidate.Witness(),
			namespaceWitness: darwinInsensitiveWitness(namespace),
		},
		{
			name:             "ASCII-only suffix",
			candidate:        ASCII,
			candidateWitness: pathtest.DarwinCaseSensitive(ASCII).Witness(),
			namespaceWitness: darwinNamespace.Witness(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pathauthority.NewProvisional(
				test.candidate,
				test.candidateWitness,
				namespace,
				test.namespaceWitness,
			); err == nil {
				t.Fatal("NewProvisional accepted incompatible path authority")
			}
		})
	}

	if _, err := pathauthority.NewProvisional(
		nonASCII,
		darwinCandidate.Witness(),
		namespace,
		darwinNamespace.Witness(),
	); err != nil {
		t.Fatalf("NewProvisional rejected valid Darwin authority: %v", err)
	}
}

func TestProvisionalCandidateWithinUsesContainmentWithoutPromotion(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "tmp", "daem")
	namespace := filepath.Join(root, "skills")
	candidate := filepath.Join(namespace, "Caf\u00e9")
	provisional, err := pathauthority.NewProvisional(
		candidate,
		pathtest.DarwinCaseSensitive(candidate).Witness(),
		namespace,
		pathtest.DarwinCaseSensitive(namespace).Witness(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !provisional.CandidateWithin(pathtest.DarwinCaseSensitive(root)) {
		t.Fatal("exact ancestor did not contain provisional candidate")
	}
	if provisional.CandidateWithin(pathtest.DarwinCaseSensitive(filepath.Join(root, "other"))) {
		t.Fatal("disjoint exact path contained provisional candidate")
	}
}

func TestProvisionalMatchesOnlyItsFormerExactCandidate(t *testing.T) {
	namespace := filepath.Join(string(filepath.Separator), "tmp", "daem", "skills")
	candidate := filepath.Join(namespace, "Caf\u00e9")
	exact := pathtest.DarwinCaseSensitive(candidate)
	provisional, err := pathauthority.NewProvisional(
		candidate,
		exact.Witness(),
		namespace,
		pathtest.DarwinCaseSensitive(namespace).Witness(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !provisional.MatchesMissingExact(exact) {
		t.Fatal("provisional candidate did not match its former exact authority")
	}
	if provisional.MatchesMissingExact(pathtest.DarwinCaseSensitive(filepath.Join(namespace, "Tea\u00e9"))) {
		t.Fatal("provisional candidate matched a different exact key")
	}
	if provisional.MatchesMissingExact(pathtest.Exact(candidate)) {
		t.Fatal("provisional candidate matched different filesystem semantics")
	}
}

func TestProvisionalEqualityKeepsUnknownUnicodeWithoutLosingStableComponents(t *testing.T) {
	namespace := filepath.Join(string(filepath.Separator), "tmp", "daem")
	for _, scenario := range []struct {
		candidate string
		selected  string
		possible  bool
	}{
		{".pi/prompts/Café.md", ".claude/settings.json", false},
		{".pi/Café.json", ".claude/settings.json", false},
		{".pi/Café.json", ".pi/settings.json", true},
		{".pi/ſettings.json", ".pi/settings.json", true},
		{".pi/Café.json", ".pi/Café.json", true},
	} {
		candidate := filepath.Join(namespace, filepath.FromSlash(scenario.candidate))
		selected := pathtest.DarwinCaseSensitive(filepath.Join(namespace, filepath.FromSlash(scenario.selected)))
		provisional, err := pathauthority.NewProvisional(candidate, pathtest.DarwinCaseSensitive(candidate).Witness(), namespace, pathtest.DarwinCaseSensitive(namespace).Witness())
		if err != nil {
			t.Fatal(err)
		}
		if provisional.CouldEqual(selected) != scenario.possible {
			t.Fatal("provisional equality lost a stable component or inferred Unicode equivalence", scenario)
		}
	}
}

func darwinInsensitiveWitness(path string) string {
	volume := filepath.VolumeName(path)
	relative := strings.TrimPrefix(path, volume+string(filepath.Separator))
	componentCount := 0
	if relative != "" {
		componentCount = len(strings.Split(relative, string(filepath.Separator)))
	}
	return "darwin-case-v1:" + strings.Repeat("i", componentCount)
}

func TestProvisionalRegionEvidenceRetainsStablePrefixesWithoutPromotion(t *testing.T) {
	base := t.TempDir()
	for _, scenario := range []struct {
		namespace, candidate, root, prefix string
		confined, overlap                  bool
	}{
		{"", "codex/extension-settings/Café.toml", "codex", "extension-settings", true, true},
		{"codex", "codex/extension-settings/Café.toml", "codex/auth.json", "", false, false},
		{"codex/knowné", "codex/knowné/Café.toml", "codex", "knowné", true, true},
		{"codex", "codex/ſessions/x", "codex/sessions", "", false, true},
		{"codex/sessions", "codex/sessions/Café", "codex/sessions", "", true, true},
		{"", "codéx/extension-settings/Café", "codex", "", false, true},
	} {
		namespace := filepath.Join(base, filepath.FromSlash(scenario.namespace))
		candidate := filepath.Join(base, filepath.FromSlash(scenario.candidate))
		root := pathtest.DarwinCaseSensitive(filepath.Join(base, filepath.FromSlash(scenario.root)))
		provisional, err := pathauthority.NewProvisional(candidate, pathtest.DarwinCaseSensitive(candidate).Witness(), namespace, pathtest.DarwinCaseSensitive(namespace).Witness())
		if err != nil {
			t.Fatal(err)
		}
		prefix, present := provisional.StableRelativePrefix(root)
		if provisional.ConfinedWithin(root) != scenario.confined || provisional.CouldOverlap(root) != scenario.overlap || filepath.ToSlash(prefix) != scenario.prefix || present != (scenario.prefix != "") {
			t.Fatal("provisional region evidence inferred normalization or lost a stable prefix", scenario, prefix, present)
		}
	}
}
