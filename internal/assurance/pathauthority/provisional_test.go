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

func darwinInsensitiveWitness(path string) string {
	volume := filepath.VolumeName(path)
	relative := strings.TrimPrefix(path, volume+string(filepath.Separator))
	componentCount := 0
	if relative != "" {
		componentCount = len(strings.Split(relative, string(filepath.Separator)))
	}
	return "darwin-case-v1:" + strings.Repeat("i", componentCount)
}
