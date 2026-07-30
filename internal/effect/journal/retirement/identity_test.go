package retirement

import (
	"fmt"
	"strings"
	"testing"
)

const (
	testOperationID = "20260730T120000.000000000Z-apply"
	testFingerprint = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testDigest      = "95d40777cfa826d5b79feaba2d46b29c138d139d2e13e2dab4ccf18f85402d44"
)

func TestIdentityGoldenNamesAndDigest(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)

	if got := identity.OperationID(); got != testOperationID {
		t.Fatalf("OperationID = %q, want %q", got, testOperationID)
	}
	if got := identity.JournalAuthorityFingerprint(); got != testFingerprint {
		t.Fatalf("JournalAuthorityFingerprint = %q, want %q", got, testFingerprint)
	}
	if got := identity.digest; got != testDigest {
		t.Fatalf("Digest = %q, want independently calculated %q", got, testDigest)
	}

	wantNames := map[string]string{
		"control": "retirement-v1-" + testDigest,
		"residue": ".daem-journal-residue-v1-" + testDigest,
		"gc":      ".daem-journal-gc-v1-" + testDigest,
	}
	gotNames := map[string]string{
		"control": identity.ControlName(),
		"residue": identity.ResidueName(),
		"gc":      identity.GCName(),
	}
	for kind, want := range wantNames {
		if got := gotNames[kind]; got != want {
			t.Fatalf("%s name = %q, want %q", kind, got, want)
		}
	}
}

func TestIdentityRejectsMalformedInputs(t *testing.T) {
	validFingerprint := testFingerprint
	invalidOperationIDs := []string{
		"",
		".",
		"..",
		".hidden",
		"contains/slash",
		"contains\\backslash",
		"contains space",
		"non-ascii-\u00e9",
		"retirement-v1-" + testDigest,
		"retirement-future",
		".daem-journal-residue-v1-" + testDigest,
		".daem-journal-gc-v1-" + testDigest,
		".daem-tombstone-legacy",
	}
	for _, operationID := range invalidOperationIDs {
		t.Run(fmt.Sprintf("operation_%q", operationID), func(t *testing.T) {
			if err := ValidateOperationID(operationID); err == nil {
				t.Fatalf("ValidateOperationID(%q) succeeded", operationID)
			}
			if _, err := NewIdentity(operationID, validFingerprint); err == nil {
				t.Fatalf("NewIdentity(%q, valid fingerprint) succeeded", operationID)
			}
		})
	}

	validOperationID := testOperationID
	invalidFingerprints := []string{
		"",
		"sha256:",
		"sha256:" + strings.Repeat("1", 63),
		"sha256:" + strings.Repeat("1", 65),
		"sha256:" + strings.Repeat("A", 64),
		"sha256:" + strings.Repeat("g", 64),
		"SHA256:" + strings.Repeat("1", 64),
		strings.Repeat("1", 64),
	}
	for _, fingerprint := range invalidFingerprints {
		t.Run(fmt.Sprintf("fingerprint_%q", fingerprint), func(t *testing.T) {
			if _, err := NewIdentity(validOperationID, fingerprint); err == nil {
				t.Fatalf("NewIdentity(valid operation, %q) succeeded", fingerprint)
			}
		})
	}
}

func TestInspectNameClassifiesReservedAndUnrelatedNames(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)
	tests := []struct {
		name       string
		kind       NameKind
		hasDigest  bool
		belongs    bool
		isReserved bool
	}{
		{name: identity.ControlName(), kind: NameControl, hasDigest: true, belongs: true, isReserved: true},
		{name: identity.ResidueName(), kind: NameResidue, hasDigest: true, belongs: true, isReserved: true},
		{name: identity.GCName(), kind: NameGC, hasDigest: true, belongs: true, isReserved: true},
		{name: "retirement-v1-short", kind: NameMalformed, isReserved: true},
		{name: "retirement-v2-" + testDigest, kind: NameMalformed, isReserved: true},
		{name: ".daem-journal-residue-v1-" + strings.ToUpper(testDigest), kind: NameMalformed, isReserved: true},
		{name: ".daem-journal-gc-v2-" + testDigest, kind: NameMalformed, isReserved: true},
		{name: ".daem-tombstone-legacy", kind: NameLegacyTombstone, isReserved: true},
		{name: ".unrelated-hidden", kind: NameUnrelated},
		{name: "ordinary-operation", kind: NameUnrelated},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name := InspectName(test.name)
			if got := name.Kind(); got != test.kind {
				t.Fatalf("Kind = %q, want %q", got, test.kind)
			}
			if got := name.Value(); got != test.name {
				t.Fatalf("Value = %q, want %q", got, test.name)
			}
			digest, hasDigest := name.Digest()
			if hasDigest != test.hasDigest {
				t.Fatalf("Digest present = %t, want %t (digest %q)", hasDigest, test.hasDigest, digest)
			}
			if got := name.BelongsTo(identity); got != test.belongs {
				t.Fatalf("BelongsTo = %t, want %t", got, test.belongs)
			}
			if got := IsReservedName(test.name); got != test.isReserved {
				t.Fatalf("IsReservedName = %t, want %t", got, test.isReserved)
			}
		})
	}
}

func TestIdentityNamesDoNotCollideAcrossCorpus(t *testing.T) {
	names := make(map[string]string)
	for index := range 256 {
		operationID := fmt.Sprintf("20260730T120000.%09dZ-apply", index)
		fingerprint := fmt.Sprintf("sha256:%064x", index+1)
		identity := mustIdentity(t, operationID, fingerprint)

		for kind, name := range map[string]string{
			"control": identity.ControlName(),
			"residue": identity.ResidueName(),
			"gc":      identity.GCName(),
		} {
			if previous, collision := names[name]; collision {
				t.Fatalf("%s name %q collides with %s", kind, name, previous)
			}
			names[name] = operationID + "/" + kind
		}
	}
}

func TestIdentityValidationRejectsForgedDigest(t *testing.T) {
	identity := mustIdentity(t, testOperationID, testFingerprint)
	identity.digest = strings.Repeat("0", 64)

	if identity.valid() {
		t.Fatal("identity with forged digest remained valid")
	}
	if identity.Equal(identity) {
		t.Fatal("identity with forged digest compared equal")
	}
	if InspectName("retirement-v1-" + strings.Repeat("0", 64)).BelongsTo(identity) {
		t.Fatal("reserved name derived authority from a forged identity")
	}
	if identity.ControlName() != "" || identity.ResidueName() != "" || identity.GCName() != "" {
		t.Fatal("forged identity emitted reserved artifact names")
	}
}

func TestNameValidationRejectsForgedNormalizedValue(t *testing.T) {
	forged := Name{
		value:  "retirement-v1-" + testDigest,
		kind:   NameGC,
		digest: testDigest,
	}
	if digest, ok := forged.Digest(); ok || digest != "" {
		t.Fatalf("forged Name.Digest = %q, %t", digest, ok)
	}
	if blocker, ok := BlockerForName(Name{}); !ok ||
		!strings.Contains(blocker.detail, "uninitialized") {
		t.Fatalf("zero Name blocker = %#v, %t", blocker, ok)
	}
}

func mustIdentity(t *testing.T, operationID string, fingerprint string) Identity {
	t.Helper()

	identity, err := NewIdentity(operationID, fingerprint)
	if err != nil {
		t.Fatalf("NewIdentity returned error: %v", err)
	}
	return identity
}
