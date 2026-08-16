package clipresent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
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
