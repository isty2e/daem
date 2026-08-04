package delegate

import (
	"strings"
	"testing"
)

func TestPackageRefDerivesSelectorAssuranceByEcosystem(t *testing.T) {
	fullDigest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name      string
		ecosystem PackageEcosystem
		selector  string
		want      selectorAssurance
	}{
		{name: "npm absent", ecosystem: EcosystemNPM, want: selectorAssuranceFloating},
		{name: "npm latest tag", ecosystem: EcosystemNPM, selector: "latest", want: selectorAssuranceFloating},
		{name: "npm custom tag", ecosystem: EcosystemNPM, selector: "next", want: selectorAssuranceFloating},
		{name: "npm partial major", ecosystem: EcosystemNPM, selector: "1", want: selectorAssuranceFloating},
		{name: "npm partial minor", ecosystem: EcosystemNPM, selector: "1.2", want: selectorAssuranceFloating},
		{name: "npm exact", ecosystem: EcosystemNPM, selector: "1.2.3", want: selectorAssuranceExactVersion},
		{name: "npm exact v prefix", ecosystem: EcosystemNPM, selector: "v1.2.3", want: selectorAssuranceExactVersion},
		{name: "npm exact prerelease", ecosystem: EcosystemNPM, selector: "1.2.3-beta.1", want: selectorAssuranceExactVersion},
		{name: "npm exact build", ecosystem: EcosystemNPM, selector: "1.2.3+build.7", want: selectorAssuranceExactVersion},
		{name: "npm caret", ecosystem: EcosystemNPM, selector: "^1.2.3", want: selectorAssuranceFloating},
		{name: "npm tilde", ecosystem: EcosystemNPM, selector: "~1.2.3", want: selectorAssuranceFloating},
		{name: "npm comparator range", ecosystem: EcosystemNPM, selector: ">=1.2.3 <2.0.0", want: selectorAssuranceFloating},
		{name: "npm wildcard", ecosystem: EcosystemNPM, selector: "1.2.x", want: selectorAssuranceFloating},
		{name: "npm star", ecosystem: EcosystemNPM, selector: "*", want: selectorAssuranceFloating},
		{name: "npm malformed exact", ecosystem: EcosystemNPM, selector: "1.02.3", want: selectorAssuranceFloating},
		{name: "python absent", ecosystem: EcosystemPython, want: selectorAssuranceFloating},
		{name: "python exact release", ecosystem: EcosystemPython, selector: "1.2.3", want: selectorAssuranceExactVersion},
		{name: "python exact equality", ecosystem: EcosystemPython, selector: "==1.2.3", want: selectorAssuranceExactVersion},
		{name: "python exact epoch", ecosystem: EcosystemPython, selector: "1!2.0", want: selectorAssuranceExactVersion},
		{name: "python exact prerelease", ecosystem: EcosystemPython, selector: "2.0rc1", want: selectorAssuranceExactVersion},
		{name: "python exact post release", ecosystem: EcosystemPython, selector: "2.0.post1", want: selectorAssuranceExactVersion},
		{name: "python exact dev release", ecosystem: EcosystemPython, selector: "2.0.dev1", want: selectorAssuranceExactVersion},
		{name: "python exact local", ecosystem: EcosystemPython, selector: "2.0+local.1", want: selectorAssuranceExactVersion},
		{name: "python compatible release", ecosystem: EcosystemPython, selector: "~=1.4.2", want: selectorAssuranceFloating},
		{name: "python range", ecosystem: EcosystemPython, selector: ">=1.0,<2", want: selectorAssuranceFloating},
		{name: "python wildcard equality", ecosystem: EcosystemPython, selector: "==1.2.*", want: selectorAssuranceFloating},
		{name: "python exclusion", ecosystem: EcosystemPython, selector: "!=1.2.3", want: selectorAssuranceFloating},
		{name: "python arbitrary equality", ecosystem: EcosystemPython, selector: "===vendor-version", want: selectorAssuranceFloating},
		{name: "python malformed exact", ecosystem: EcosystemPython, selector: "not-a-version", want: selectorAssuranceFloating},
		{name: "container absent", ecosystem: EcosystemContainer, want: selectorAssuranceFloating},
		{name: "container latest tag", ecosystem: EcosystemContainer, selector: "latest", want: selectorAssuranceFloating},
		{name: "container version tag", ecosystem: EcosystemContainer, selector: "1.2.3", want: selectorAssuranceFloating},
		{name: "container canonical digest", ecosystem: EcosystemContainer, selector: fullDigest, want: selectorAssuranceImmutableDigest},
		{name: "container short digest", ecosystem: EcosystemContainer, selector: "sha256:abc123", want: selectorAssuranceFloating},
		{name: "container uppercase algorithm", ecosystem: EcosystemContainer, selector: "SHA256:" + strings.Repeat("a", 64), want: selectorAssuranceFloating},
		{name: "container uppercase digest", ecosystem: EcosystemContainer, selector: "sha256:" + strings.Repeat("A", 64), want: selectorAssuranceFloating},
		{name: "container wrong algorithm", ecosystem: EcosystemContainer, selector: "sha512:" + strings.Repeat("a", 128), want: selectorAssuranceFloating},
		{name: "container malformed hex", ecosystem: EcosystemContainer, selector: "sha256:" + strings.Repeat("z", 64), want: selectorAssuranceFloating},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, err := NewPackageRef(test.ecosystem, packageNameForEcosystem(test.ecosystem), test.selector)
			if err != nil {
				t.Fatalf("NewPackageRef returned error: %v", err)
			}
			if got := ref.assurance; got != test.want {
				t.Fatalf("selector assurance = %q, want %q", got, test.want)
			}
			wantPolicy := PinFloating
			if test.want == selectorAssuranceExactVersion || test.want == selectorAssuranceImmutableDigest {
				wantPolicy = PinPinned
			}
			if got := ref.PinPolicy(); got != wantPolicy {
				t.Fatalf("PinPolicy() = %q, want %q", got, wantPolicy)
			}
		})
	}
}

func packageNameForEcosystem(ecosystem PackageEcosystem) string {
	switch ecosystem {
	case EcosystemNPM:
		return "@scope/server"
	case EcosystemPython:
		return "mcp-server"
	case EcosystemContainer:
		return "localhost:5000/acme/server"
	default:
		return "server"
	}
}
