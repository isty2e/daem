package rootedpath

import (
	"strings"
	"testing"
)

func TestAuthorityProvenanceRoundTripMatchesAuthority(t *testing.T) {
	authority := provenanceTestAuthority(t)
	derived, err := authority.Provenance()
	if err != nil {
		t.Fatalf("Provenance returned error: %v", err)
	}

	restored, err := NewAuthorityProvenance(
		derived.PhysicalRoot(),
		derived.ObjectFingerprint(),
		derived.MountFingerprint(),
	)
	if err != nil {
		t.Fatalf("NewAuthorityProvenance returned error: %v", err)
	}
	if err := restored.Match(authority); err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if restored.ObjectFingerprint() == restored.MountFingerprint() {
		t.Fatal("domain-separated object and mount fingerprints are equal")
	}
}

func TestAuthorityProvenanceRejectsNoncanonicalEvidence(t *testing.T) {
	valid, err := provenanceTestAuthority(t).Provenance()
	if err != nil {
		t.Fatalf("Provenance returned error: %v", err)
	}

	tests := []struct {
		name     string
		root     string
		object   string
		mount    string
		wantKind FailureKind
	}{
		{name: "relative root", root: "project", object: valid.ObjectFingerprint(), mount: valid.MountFingerprint(), wantKind: FailureInvalidRoot},
		{name: "uppercase digest", root: valid.PhysicalRoot(), object: strings.ToUpper(valid.ObjectFingerprint()), mount: valid.MountFingerprint(), wantKind: FailureInvalidRoot},
		{name: "short digest", root: valid.PhysicalRoot(), object: "sha256:00", mount: valid.MountFingerprint(), wantKind: FailureInvalidRoot},
		{name: "same domains", root: valid.PhysicalRoot(), object: valid.ObjectFingerprint(), mount: valid.ObjectFingerprint(), wantKind: FailureInvalidRoot},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAuthorityProvenance(test.root, test.object, test.mount)
			if !hasFailureKind(err, test.wantKind) {
				t.Fatalf("error = %v, want kind %s", err, test.wantKind)
			}
		})
	}
}

func TestAuthorityProvenanceDistinguishesRootAndMountDrift(t *testing.T) {
	base := provenanceTestAuthority(t)
	provenance, err := base.Provenance()
	if err != nil {
		t.Fatalf("Provenance returned error: %v", err)
	}

	replaced := base
	replaced.object = identityToken{9}
	if err := provenance.Match(replaced); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("object drift error = %v, want %s", err, FailureRootReplaced)
	}

	movedMount := base
	movedMount.mount = identityToken{8}
	if err := provenance.Match(movedMount); !hasFailureKind(err, FailureMountChanged) {
		t.Fatalf("mount drift error = %v, want %s", err, FailureMountChanged)
	}

	differentPath := base
	differentPath.physicalRoot += "-other"
	if err := provenance.Match(differentPath); !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("path drift error = %v, want %s", err, FailureRootReplaced)
	}
}

func provenanceTestAuthority(t *testing.T) Authority {
	t.Helper()
	return mustAuthority(t, "/project", identityToken{1}, identityToken{2})
}
