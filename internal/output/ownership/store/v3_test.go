package store

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func nativeClaim(t *testing.T, keys ...string) ownership.Claim {
	t.Helper()
	root := canonicalTestRoot(t)
	base := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "")
	path, err := output.NewKeyPath(keys...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(path)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(base.Address().PathAuthority(), footprint)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, base.Owner())
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func TestRegistryV3RoundTripsNativeFootprints(t *testing.T) {
	for _, keys := range [][]string{{""}, {"a/b", "~", "."}, {"line\nkey", "日本語"}} {
		claim := nativeClaim(t, keys...)
		registry, err := ownership.NewRegistry([]ownership.Claim{claim})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := Marshal(registry)
		if err != nil {
			t.Fatal(err)
		}
		var envelope file
		if err := json.Unmarshal(encoded, &envelope); err != nil || envelope.Version != 3 {
			t.Fatalf("native footprint published without current version: %v", err)
		}
		decoded, err := decode(encoded)
		if err != nil {
			t.Fatal(err)
		}
		actual, found := decoded.Exact(claim.Address())
		if !found || !actual.Equal(claim) {
			t.Fatal("native footprint lost during persisted round trip")
		}
		key, selected := actual.Address().Footprint().Key()
		if !selected || key.IsRoot() {
			t.Fatal("key footprint became whole-path authority")
		}
	}
}

func TestRegistryV2MigrationPreservesLiteralTildeIdentity(t *testing.T) {
	claim := nativeClaim(t, "a~1b")
	address := claim.Address().PathAuthority()
	owner := claim.Owner().StatefileAuthority()
	legacy := registryV2File{Version: 2, Claims: []registryV2ClaimRecord{{
		PathAuthority:      pathAuthorityRecord{Key: address.Key(), Witness: address.Witness()},
		ContentPath:        "/a~1b",
		StatefileAuthority: pathAuthorityRecord{Key: owner.Key(), Witness: owner.Witness()},
		ManifestPath:       claim.Owner().ManifestPath(), State: string(ownership.ClaimActive),
	}}}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if actual, found := decoded.Exact(claim.Address()); !found || !actual.Equal(claim) {
		t.Fatal("v2 literal tilde was reinterpreted as an RFC escape")
	}
	upgraded, err := Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	decodedAgain, err := decode(upgraded)
	if err != nil {
		t.Fatal(err)
	}
	if actual, found := decodedAgain.Exact(claim.Address()); !found || !actual.Equal(claim) {
		t.Fatal("v2 to v3 publication changed ownership")
	}
}

func TestRegistryVersionGatesDoNotInferAddressEncoding(t *testing.T) {
	claim := nativeClaim(t, "")
	registry, err := ownership.NewRegistry([]ownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	futureVersion, err := json.Marshal(currentVersion + 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []json.RawMessage{json.RawMessage("2"), futureVersion} {
		envelope["version"] = version
		malformed, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decode(malformed); err == nil {
			t.Fatalf("tagged native data admitted under version %s", version)
		}
	}
	for _, footprint := range []json.RawMessage{
		json.RawMessage(`{"kind":"key","key_path":""}`),
		json.RawMessage(`{"kind":"path","key_path":"/"}`),
		json.RawMessage(`{"kind":"key","key_path":"/bad~2"}`),
	} {
		var records []map[string]json.RawMessage
		if err := json.Unmarshal(envelope["claims"], &records); err != nil {
			t.Fatal(err)
		}
		records[0]["footprint"] = footprint
		claims, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		envelope["version"] = json.RawMessage("3")
		envelope["claims"] = claims
		malformed, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decode(malformed); err == nil {
			t.Fatalf("invalid footprint acquired authority: %s", footprint)
		}
	}
}
