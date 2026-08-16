package extension

import "testing"

func TestParseNPMPackageSpecOwnsSelectorsAliasesAndCredentialAuthority(t *testing.T) {
	tests := []struct {
		name             string
		value            string
		wantOK           bool
		wantName         string
		wantSelector     bool
		wantDirect       bool
		wantPublic       bool
		wantCredentialOK bool
	}{
		{name: "package", value: "tool", wantOK: true, wantName: "tool", wantDirect: true, wantPublic: true, wantCredentialOK: true},
		{name: "scoped range", value: "@acme/tool@>=1.2.3 <2", wantOK: true, wantName: "@acme/tool", wantSelector: true, wantDirect: true, wantPublic: true, wantCredentialOK: true},
		{name: "registry alias", value: "tool-alias@npm:@acme/tool@1.2.3", wantOK: true, wantName: "tool-alias", wantSelector: true, wantPublic: true, wantCredentialOK: true},
		{name: "case-insensitive registry alias", value: "tool-alias@NPM:@acme/tool@1.2.3", wantOK: true, wantName: "tool-alias", wantSelector: true, wantPublic: true, wantCredentialOK: true},
		{name: "local selector", value: "tool@../../private/plugin", wantOK: true, wantName: "tool", wantSelector: true, wantCredentialOK: true},
		{name: "dot local selector", value: "tool@.", wantOK: true, wantName: "tool", wantSelector: true, wantCredentialOK: true},
		{name: "parent local selector", value: "tool@..", wantOK: true, wantName: "tool", wantSelector: true, wantCredentialOK: true},
		{name: "hidden local selector", value: "tool@.hidden", wantOK: true, wantName: "tool", wantSelector: true, wantCredentialOK: true},
		{name: "case-insensitive file selector", value: "tool@FILE:../private/plugin", wantOK: true, wantName: "tool", wantSelector: true, wantCredentialOK: true},
		{name: "credential-free remote selector", value: "tool@https://example.com/archive.tgz", wantOK: true, wantName: "tool", wantSelector: true, wantCredentialOK: true},
		{name: "encoded at sign in remote path", value: "tool@https://example.com/tool%40scope.tgz", wantOK: true, wantName: "tool", wantSelector: true, wantCredentialOK: true},
		{name: "stable encoded percent in remote path", value: "tool@https://example.com/100%25ready.tgz", wantOK: true, wantName: "tool", wantSelector: true, wantCredentialOK: true},
		{name: "credential assignment selector", value: "tool@token:actual-secret", wantOK: true, wantName: "tool", wantSelector: true},
		{name: "spaced credential assignment selector", value: "tool@token = actual-secret", wantOK: true, wantName: "tool", wantSelector: true},
		{name: "credential assignment in alias target", value: "tool-alias@npm:tool@token = actual-secret", wantOK: true, wantName: "tool-alias", wantSelector: true},
		{name: "encoded credential assignment selector", value: "tool@token%20%3D%20actual-secret", wantOK: true, wantName: "tool", wantSelector: true},
		{name: "double-encoded credential assignment selector", value: "tool@token%2520%253D%2520actual-secret", wantOK: true, wantName: "tool", wantSelector: true},
		{name: "unsupported URL credential selector", value: "tool@foo://user:actual-secret@example.com/path", wantOK: true, wantName: "tool", wantSelector: true},
		{name: "password shaped alias target", value: "tool@npm:user:actual-secret@short-host", wantOK: true, wantName: "tool", wantSelector: true},
		{name: "invalid package name", value: "user:secret@host", wantOK: false},
		{name: "trailing separator", value: "tool@", wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec, ok := ParseNPMPackageSpec(test.value)
			if ok != test.wantOK {
				t.Fatalf("ParseNPMPackageSpec(%q) ok = %t, want %t", test.value, ok, test.wantOK)
			}
			if !ok {
				return
			}
			if spec.Name() != test.wantName ||
				spec.HasSelector() != test.wantSelector ||
				spec.DirectRegistry() != test.wantDirect ||
				spec.Public() != test.wantPublic ||
				spec.CredentialFree() != test.wantCredentialOK {
				t.Fatalf(
					"package spec = name:%q selector:%t direct:%t public:%t credential-free:%t",
					spec.Name(),
					spec.HasSelector(),
					spec.DirectRegistry(),
					spec.Public(),
					spec.CredentialFree(),
				)
			}
		})
	}
}

func TestNPMPackageSpecZeroValueCarriesNoAuthority(t *testing.T) {
	var spec NPMPackageSpec
	if spec.DirectRegistry() || spec.Public() || spec.CredentialFree() {
		t.Fatalf("zero package spec gained authority: %#v", spec)
	}
}

func TestNPMPackageSpecAliasAndOpaqueSelectorBoundaries(t *testing.T) {
	tests := []struct {
		value      string
		wantPublic bool
		wantSafe   bool
	}{
		{value: "tool-alias@npm:tool@latest", wantPublic: true, wantSafe: true},
		{value: "@acme/tool-alias@npm:@vendor/tool@^2.0.0", wantPublic: true, wantSafe: true},
		{value: "tool-alias@npm:other@npm:third@1.0.0"},
		{value: "tool@user%3Aactual-secret%40short-host"},
		{value: "tool@https://user:actual-secret@example.com/archive.tgz"},
		{value: "tool@file:../private/plugin", wantSafe: true},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			spec, ok := ParseNPMPackageSpec(test.value)
			if !ok {
				t.Fatalf("ParseNPMPackageSpec(%q) rejected package-owned selector", test.value)
			}
			if spec.Public() != test.wantPublic || spec.CredentialFree() != test.wantSafe {
				t.Fatalf(
					"ParseNPMPackageSpec(%q) public/safe = %t/%t, want %t/%t",
					test.value,
					spec.Public(),
					spec.CredentialFree(),
					test.wantPublic,
					test.wantSafe,
				)
			}
		})
	}
}
