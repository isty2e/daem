package clipresent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestIdentityDisclosurePreservesOnlySafeBoundedIdentities(t *testing.T) {
	longIdentity := strings.Repeat("a", maximumIdentityDisclosureBytes+1)
	for _, test := range []struct {
		name     string
		value    string
		redacted bool
	}{
		{name: "npm package", value: "npm:@acme/tool"},
		{name: "npm selector", value: "npm:@acme/tool@>=1.0.0"},
		{name: "credential-free git URL", value: "git:https://example.test/acme/tool.git"},
		{name: "quotes and backslashes", value: `npm:quote"and\slash`},
		{name: "URL userinfo", value: "https://user:secret@example.test/plugin", redacted: true},
		{name: "nested URL userinfo", value: "git:https://user:secret@example.test/plugin", redacted: true},
		{name: "URL query", value: "https://example.test/plugin?token=secret", redacted: true},
		{name: "encoded secret fragment", value: "https://example.test/plugin#token%3Dsecret", redacted: true},
		{name: "encoded colon delimiter", value: "npm:tool@token%3Asecret", redacted: true},
		{name: "option-style credential", value: "npm:tool@--client-secret=actual-secret", redacted: true},
		{name: "encoded credential key", value: "git:example.com/acme/%74oken=actual-secret", redacted: true},
		{name: "encoded userinfo", value: "https://user:secret%40example.test/plugin", redacted: true},
		{name: "encoded absolute path", value: "%2FUsers%2Falice%2Fprivate%2Fplugin.ts", redacted: true},
		{name: "hash ref file scheme local path", value: "github:acme/tool#file:/Users/alice/private", redacted: true},
		{name: "hash ref absolute local path", value: "github:acme/tool#/Users/alice/private", redacted: true},
		{name: "at ref local path", value: "git:github.com/acme/tools@/Users/alice/private", redacted: true},
		{name: "scheme boundary not file", value: "profile:/settings", redacted: false},
		{name: "underscore-led credential assignment", value: "github:acme/_token=actual-secret", redacted: true},
		{name: "unsupported key assignment", value: "github:acme/ключ=actual-secret", redacted: true},
		{name: "password-shaped git shorthand", value: "git:user:actual-secret@github.com/acme/tool", redacted: true},
		{name: "encoded LF git path", value: "git+https://example.com/acme/tool%0Aforged.git#v1", redacted: true},
		{name: "encoded Bidi git path", value: "git+https://example.com/acme/tool%E2%80%AEforged.git#v1", redacted: true},
		{name: "secret assignment", value: "npm:tool#api_key=secret", redacted: true},
		{name: "generic assignment", value: "npm:tool;credential=secret", redacted: true},
		{name: "secret colon", value: "npm:tool;authorization:Bearer-secret", redacted: true},
		{name: "absolute path", value: "/Users/alice/private/plugin.ts", redacted: true},
		{name: "file URL", value: "file:///Users/alice/private/plugin.ts", redacted: true},
		{name: "nested file URL", value: "git:file:///Users/alice/private/plugin.ts", redacted: true},
		{name: "local identity", value: "local:project:/Users/alice/private/plugin.ts", redacted: true},
		{name: "relative path", value: "../private/plugin.ts", redacted: true},
		{name: "Windows path", value: `C:\Users\alice\private\plugin.ts`, redacted: true},
		{name: "overlong", value: longIdentity, redacted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			disclosure := identityDisclosureFor(test.value)
			if disclosure.Redacted() != test.redacted {
				t.Fatalf(
					"Redacted() = %t, want %t for %q",
					disclosure.Redacted(),
					test.redacted,
					test.value,
				)
			}
			if !test.redacted {
				if disclosure.Value() != test.value {
					t.Fatalf("Value() = %q, want %q", disclosure.Value(), test.value)
				}
				return
			}
			digest := sha256.Sum256([]byte(test.value))
			want := "redacted:sha256:" + hex.EncodeToString(digest[:])
			if disclosure.Value() != want ||
				identityDisclosureFor(test.value).Value() != want ||
				strings.Contains(disclosure.Value(), test.value) {
				t.Fatalf("redacted disclosure = %#v, want %q", disclosure, want)
			}
		})
	}
}

func TestVerboseIdentityDisclosureRedactsSensitiveCredentialShapes(t *testing.T) {
	for _, value := range []string{
		"npm:tool@token%3Aactual-secret",
		"npm:tool@token%3Dactual-secret",
		"npm:tool@--client-secret=actual-secret",
		"--token=actual-secret",
		"git:example.com/acme/%74oken=actual-secret",
		"https://user:secret%40example.test/plugin",
		"github:acme/tool#token%25252525252525253Dactual-secret",
		"git:example.com/acme/tool%2",
		"git:user:actual-secret@github.com/acme/tool",
		"git:user:actual-secret@github.com",
		"user:actual-secret@short-host:443/acme/tool",
		"user:actual-secret@[2001:db8::1]:443/acme/tool",
		"https://example.test/#git:user:actual-secret@github.com/acme/tool",
		"https://example.test/?git:user:actual-secret@github.com/acme/tool",
		"user:actual-secret@short-host",
		"https://example.test/#user:actual-secret@short-host",
		"[git:user:actual-secret@short-host]",
		"git:user:actual-secret@short-host:repo/path",
		"github:user:actual-secret@short-host:repo/path",
		"user:actual-secret@short+host",
		"[git:user:actual-secret@short-host]:",
		"git:user:actual-secret@[2001:db8::1",
	} {
		disclosure := verboseIdentityDisclosureFor(value)
		if !disclosure.Redacted() {
			t.Errorf("verboseIdentityDisclosureFor(%q) not redacted", value)
		}
	}
	if disclosure := verboseIdentityDisclosureFor("git:https://example.test/acme/tool.git"); disclosure.Redacted() {
		t.Errorf("verboseIdentityDisclosureFor redacted credential-free URL: %q", disclosure.Value())
	}
	if disclosure := verboseIdentityDisclosureFor("git:git@short+host:acme/tool.git"); disclosure.Redacted() {
		t.Errorf("verboseIdentityDisclosureFor redacted credential-free scp locator: %q", disclosure.Value())
	}
	if disclosure := verboseIdentityDisclosureFor("github:acme/tool#100%25ready"); disclosure.Redacted() {
		t.Errorf("verboseIdentityDisclosureFor redacted stable percent literal: %q", disclosure.Value())
	}
}

func TestCarrierDerivedIdentityDisclosureBoundsBeforeDecode(t *testing.T) {
	overlong := strings.Repeat("%25", maximumCarrierDerivedDisclosureBytes)
	if len(overlong) <= maximumCarrierDerivedDisclosureBytes {
		t.Fatal("test fixture is not over the derived identity bound")
	}
	disclosure := grammarProvenCarrierDerivedIdentityDisclosureFor(overlong, "source")
	if !disclosure.Redacted() {
		t.Fatal("overlong derived identity was disclosed")
	}
}

func TestGrammarProvenCarrierDerivedIdentityStillChecksNonSourceFacts(t *testing.T) {
	source := "team/plugin:beta@market.name/path"
	value := `{"source_ref":"` + source + `","status":"token=actual-secret"}`
	disclosure := grammarProvenCarrierDerivedIdentityDisclosureFor(value, source)
	if !disclosure.Redacted() {
		t.Fatalf("derived identity disclosed a non-source credential: %q", disclosure.Value())
	}
}

func TestCarrierSourceDisclosureUsesTheSelectedSourceGrammar(t *testing.T) {
	marketplace, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"team/plugin:beta@market.name/path",
	)
	if err != nil {
		t.Fatalf("NewSourceRef marketplace: %v", err)
	}
	if disclosure := carrierSourceRefDisclosureFor(marketplace); disclosure.Redacted() {
		t.Fatalf("marketplace source was reinterpreted as generic userinfo: %q", disclosure.Value())
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		target.ScopeGlobal,
		marketplace,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey marketplace: %v", err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatalf("NewCarrier marketplace: %v", err)
	}
	projection := carrierSourceIdentityDisclosureFor(carrier)
	if projection.verboseSourceRef.Redacted() ||
		projection.verboseSourceRef.Value() != marketplace.Ref() {
		t.Fatalf("verbose marketplace source disclosure = %#v", projection.verboseSourceRef)
	}
	if !projection.sourceRef.Redacted() {
		t.Fatal("grammar-unproven marketplace source became public in default output")
	}

	npm, err := desiredextension.NewAuthoredSourceRef(
		desiredextension.SourceKindHostSource,
		"npm:tool-alias@npm:@acme/tool@1.2.3",
	)
	if err != nil {
		t.Fatalf("NewAuthoredSourceRef npm source: %v", err)
	}
	if disclosure := carrierSourceRefDisclosureFor(npm); disclosure.Redacted() {
		t.Fatalf("grammar-proven npm source was reinterpreted as generic userinfo: %q", disclosure.Value())
	}

	legacy, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"git:user:actual-secret@github.com/acme/tool#https://example.test/ref",
	)
	if err != nil {
		t.Fatalf("NewSourceRef legacy host source: %v", err)
	}
	if disclosure := carrierSourceRefDisclosureFor(legacy); !disclosure.Redacted() {
		t.Fatalf("credential-bearing host source was disclosed: %q", disclosure.Value())
	}

	encodedDelimiter, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"plugin@https://user:actual-secret%40example.com",
	)
	if err != nil {
		t.Fatalf("NewSourceRef encoded marketplace source: %v", err)
	}
	if disclosure := carrierSourceRefDisclosureFor(encodedDelimiter); !disclosure.Redacted() {
		t.Fatalf("canonical marketplace delimiter drift was disclosed: %q", disclosure.Value())
	}

	for _, ref := range []string{
		"npm:tool@token = actual-secret",
		"npm:alias@npm:tool@token = actual-secret",
	} {
		legacyNPM, err := desiredextension.NewSourceRef(
			desiredextension.SourceKindHostSource,
			ref,
		)
		if err != nil {
			t.Fatalf("NewSourceRef legacy npm source %q: %v", ref, err)
		}
		if disclosure := carrierSourceRefDisclosureFor(legacyNPM); !disclosure.Redacted() {
			t.Fatalf("credential-bearing npm source was disclosed: %q", disclosure.Value())
		}
	}
}

func TestCarrierSourceDisclosureKeepsPrivateGitLocatorsOutOfPublicProjection(t *testing.T) {
	for _, ref := range []string{
		"github:file:///Users/alice/private",
		"git:github.com/~/private",
		"git+https://router.home.arpa/acme/tool.git#v1",
		"git+https://100.64.0.1/acme/tool.git#v1",
		"git+https://127.0.0.1./acme/tool.git#v1",
		"git+https://224.0.0.1/acme/tool.git#v1",
		"git+https://[ff02::1]/acme/tool.git#v1",
		"git:127.0.0.1:8080/repo",
		"git:router.home.arpa:2222/repo",
		"git+https://127.1/acme/tool.git#v1",
		"git+https://10.1/acme/tool.git#v1",
		"git+https://0x7f.0.0.1/acme/tool.git#v1",
		"git+https://0.1.2.3/acme/tool.git#v1",
		"git+https://198.18.0.1/acme/tool.git#v1",
		"git+https://[2001:db8::1]/acme/tool.git#v1",
		"git+https://[64:ff9b:1::1]/acme/tool.git#v1",
		"git+https://[100:0:0:1::1]/acme/tool.git#v1",
		"git+https://[3fff::1]/acme/tool.git#v1",
		"git+https://[5f00::1]/acme/tool.git#v1",
	} {
		t.Run(ref, func(t *testing.T) {
			source, err := desiredextension.NewAuthoredSourceRef(
				desiredextension.SourceKindHostSource,
				ref,
			)
			if err != nil {
				t.Fatalf("NewAuthoredSourceRef: %v", err)
			}
			key, err := desiredextension.NewCarrierKey(
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				target.ScopeGlobal,
				source,
			)
			if err != nil {
				t.Fatalf("NewCarrierKey: %v", err)
			}
			carrier, err := extensiontopology.NewCarrier(key)
			if err != nil {
				t.Fatalf("NewCarrier: %v", err)
			}

			disclosure := carrierSourceIdentityDisclosureFor(carrier)
			if !disclosure.sourceRef.Redacted() ||
				!disclosure.sourceNamespace.Redacted() ||
				disclosure.carrierSubject == nil ||
				!disclosure.carrierSubject.NameRedacted {
				t.Fatalf("public disclosure retained private locator: %#v", disclosure)
			}
			if disclosure.verboseSourceRef.Redacted() ||
				disclosure.verboseSourceRef.Value() != ref {
				t.Fatalf("verbose source disclosure = %#v, want exact non-secret locator", disclosure.verboseSourceRef)
			}
		})
	}
}

func TestCarrierSourceDisclosureKeepsIANAReachableGitLocatorsPublic(t *testing.T) {
	for _, ref := range []string{
		"git+https://192.0.0.9/acme/tool.git#v1",
		"git+https://192.0.0.10/acme/tool.git#v1",
		"git+https://[64:ff9b::1]/acme/tool.git#v1",
		"git+https://[2001:1::1]/acme/tool.git#v1",
	} {
		t.Run(ref, func(t *testing.T) {
			source, err := desiredextension.NewAuthoredSourceRef(
				desiredextension.SourceKindHostSource,
				ref,
			)
			if err != nil {
				t.Fatalf("NewAuthoredSourceRef: %v", err)
			}
			key, err := desiredextension.NewCarrierKey(
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				target.ScopeGlobal,
				source,
			)
			if err != nil {
				t.Fatalf("NewCarrierKey: %v", err)
			}
			carrier, err := extensiontopology.NewCarrier(key)
			if err != nil {
				t.Fatalf("NewCarrier: %v", err)
			}

			disclosure := carrierSourceIdentityDisclosureFor(carrier)
			if disclosure.sourceRef.Redacted() ||
				disclosure.sourceRef.Value() != ref ||
				disclosure.carrierSubject == nil ||
				disclosure.carrierSubject.NameRedacted {
				t.Fatalf("reachable locator was withheld: %#v", disclosure)
			}
		})
	}
}
