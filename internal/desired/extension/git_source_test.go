package extension

import (
	"strings"
	"testing"
	"time"
)

func TestParseGitSourceOwnsLocatorRefAndPrivacy(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantOK     bool
		wantHost   string
		wantPath   string
		wantRef    string
		wantPublic bool
	}{
		{name: "git prefixed shorthand", source: "git:github.com/acme/tools.git@v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "git prefixed scp", source: "git:git@github.com:acme/tools.git@v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "explicit git URL", source: "https://github.com/acme/tools.git@v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "git plus https URL", source: "git+https://github.com/acme/tools.git#v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "git plus ssh URL", source: "git+ssh://git@github.com/acme/tools.git#v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "git plus http URL", source: "git+http://github.com/acme/tools.git#v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "single segment repository", source: "git+https://git.example/repo.git#v1", wantOK: true, wantHost: "git.example", wantPath: "repo", wantRef: "v1", wantPublic: true},
		{name: "literal percent in URL path", source: "git+https://example.com/acme/100%25-tool.git#v1", wantOK: true, wantHost: "example.com", wantPath: "acme/100%-tool", wantRef: "v1", wantPublic: true},
		{name: "literal percent precedes in-path ref", source: "git+https://example.com/acme/100%25-tool.git@v1", wantOK: true, wantHost: "example.com", wantPath: "acme/100%-tool", wantRef: "v1", wantPublic: true},
		{name: "encoded literal percent remains data", source: "git+https://example.com/acme/100%2525-tool.git#v1", wantOK: true, wantHost: "example.com", wantPath: "acme/100%25-tool", wantRef: "v1", wantPublic: true},
		{name: "literal percent before non-escape remains data", source: "git+https://example.com/acme/100%25zz-tool.git#v1", wantOK: true, wantHost: "example.com", wantPath: "acme/100%zz-tool", wantRef: "v1", wantPublic: true},
		{name: "encoded Unicode remains repository data", source: "git+https://example.com/acme/%E2%98%83-tool.git#v1", wantOK: true, wantHost: "example.com", wantPath: "acme/\u2603-tool", wantRef: "v1", wantPublic: true},
		{name: "encoded LF path stays git and private", source: "git+https://example.com/acme/tool%0Aforged.git#v1", wantOK: true, wantHost: "example.com", wantPath: "acme/tool\nforged", wantRef: "v1", wantPublic: false},
		{name: "encoded Bidi path stays git and private", source: "git+https://example.com/acme/tool%E2%80%AEforged.git#v1", wantOK: true, wantHost: "example.com", wantPath: "acme/tool\u202eforged", wantRef: "v1", wantPublic: false},
		{name: "home.arpa locator stays private", source: "git+https://router.home.arpa/acme/tool.git#v1", wantOK: true, wantHost: "router.home.arpa", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "CGNAT locator stays private", source: "git+https://100.64.0.1/acme/tool.git#v1", wantOK: true, wantHost: "100.64.0.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "trailing-dot loopback stays private", source: "git+https://127.0.0.1./acme/tool.git#v1", wantOK: true, wantHost: "127.0.0.1.", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "IPv4 multicast locator stays private", source: "git+https://224.0.0.1/acme/tool.git#v1", wantOK: true, wantHost: "224.0.0.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "IPv6 multicast locator stays private", source: "git+https://[ff02::1]/acme/tool.git#v1", wantOK: true, wantHost: "ff02::1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "IPv4 broadcast locator stays private", source: "git+https://255.255.255.255/acme/tool.git#v1", wantOK: true, wantHost: "255.255.255.255", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "legacy IPv4 loopback alias stays private", source: "git+https://127.1/acme/tool.git#v1", wantOK: true, wantHost: "127.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "legacy IPv4 private alias stays private", source: "git+https://10.1/acme/tool.git#v1", wantOK: true, wantHost: "10.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "hex IPv4 loopback alias stays private", source: "git+https://0x7f.0.0.1/acme/tool.git#v1", wantOK: true, wantHost: "0x7f.0.0.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "this-net IPv4 locator stays private", source: "git+https://0.1.2.3/acme/tool.git#v1", wantOK: true, wantHost: "0.1.2.3", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "benchmarking IPv4 locator stays private", source: "git+https://198.18.0.1/acme/tool.git#v1", wantOK: true, wantHost: "198.18.0.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "documentation IPv6 locator stays private", source: "git+https://[2001:db8::1]/acme/tool.git#v1", wantOK: true, wantHost: "2001:db8::1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "TEST-NET-1 locator stays private", source: "git+https://192.0.2.1/acme/tool.git#v1", wantOK: true, wantHost: "192.0.2.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "public IPv4 locator stays public", source: "git+https://8.8.8.8/acme/tool.git#v1", wantOK: true, wantHost: "8.8.8.8", wantPath: "acme/tool", wantRef: "v1", wantPublic: true},
		{name: "octal IPv4 alias stays private", source: "git+https://0177.0.0.1/acme/tool.git#v1", wantOK: true, wantHost: "0177.0.0.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "port-bearing loopback shorthand stays private", source: "git:127.0.0.1:8080/repo", wantOK: true, wantHost: "127.0.0.1:8080", wantPath: "repo", wantPublic: false},
		{name: "port-bearing home.arpa shorthand stays private", source: "git:router.home.arpa:2222/repo", wantOK: true, wantHost: "router.home.arpa:2222", wantPath: "repo", wantPublic: false},
		{name: "port-bearing loopback URL stays private", source: "git+https://127.0.0.1:8080/acme/tool.git#v1", wantOK: true, wantHost: "127.0.0.1", wantPath: "acme/tool", wantRef: "v1", wantPublic: false},
		{name: "port-bearing public URL stays public", source: "git+https://github.com:443/acme/tool.git#v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tool", wantRef: "v1", wantPublic: true},
		{name: "unparseable host-port stays private", source: "git:127.0.0.1:8080:extra/repo", wantOK: true, wantHost: "127.0.0.1:8080:extra", wantPath: "repo", wantPublic: false},
		{name: "encoded at sign stays repository data", source: "git+https://github.com/acme/tools%40scope.git", wantOK: true, wantHost: "github.com", wantPath: "acme/tools@scope", wantPublic: true},
		{name: "encoded at sign precedes literal ref", source: "git+https://github.com/acme/tools%40scope.git@v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools@scope", wantRef: "v1", wantPublic: true},
		{name: "encoded hash stays repository data", source: "git+https://github.com/acme/tools%23archive.git", wantOK: true, wantHost: "github.com", wantPath: "acme/tools#archive", wantPublic: true},
		{name: "git plus https userinfo has no authority", source: "git+https://user@example.com/acme/tools.git", wantOK: true, wantHost: "example.com", wantPath: "acme/tools"},
		{name: "git plus https password has no authority", source: "git+https://user:actual-secret@example.com/acme/tools.git", wantOK: true, wantHost: "example.com", wantPath: "acme/tools"},
		{name: "git plus ssh transport user", source: "git+ssh://git@github.com/acme/tools.git", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantPublic: true},
		{name: "git plus encoded password rejected", source: "git+https://user%3Aactual-secret%40example.com/acme/tools.git", wantOK: false},
		{name: "git plus local ref stays private", source: "git+https://github.com/acme/tools.git#/Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "/Users/alice/private", wantPublic: false},
		{name: "localhost locator stays private", source: "git+https://localhost/acme/tools.git#v1", wantOK: true, wantHost: "localhost", wantPath: "acme/tools", wantRef: "v1", wantPublic: false},
		{name: "loopback locator stays private", source: "git+https://127.0.0.1/acme/tools.git#v1", wantOK: true, wantHost: "127.0.0.1", wantPath: "acme/tools", wantRef: "v1", wantPublic: false},
		{name: "private network locator stays private", source: "git+https://10.0.0.1/acme/tools.git#v1", wantOK: true, wantHost: "10.0.0.1", wantPath: "acme/tools", wantRef: "v1", wantPublic: false},
		{name: "IPv6 loopback locator stays private", source: "git+https://[::1]/acme/tools.git#v1", wantOK: true, wantHost: "::1", wantPath: "acme/tools", wantRef: "v1", wantPublic: false},
		{name: "mDNS locator stays private", source: "git+https://gitbox.local/acme/tools.git#v1", wantOK: true, wantHost: "gitbox.local", wantPath: "acme/tools", wantRef: "v1", wantPublic: false},
		{name: "short scp host stays private", source: "git@short-host:acme/tools.git", wantOK: true, wantHost: "short-host", wantPath: "acme/tools", wantPublic: false},
		{name: "file URL locator path stays private", source: "github:file:///Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "file:///Users/alice/private", wantPublic: false},
		{name: "home relative locator path stays private", source: "git:github.com/~/private", wantOK: true, wantHost: "github.com", wantPath: "~/private", wantPublic: false},
		{name: "encoded home locator path stays private", source: "git+https://example.com/%257E/private.git", wantOK: true, wantHost: "example.com", wantPath: "%7E/private", wantPublic: false},
		{name: "encoded file locator path stays private", source: "git+https://example.com/file%3A///Users/alice/private.git", wantOK: true, wantHost: "example.com", wantPath: "file:///Users/alice/private", wantPublic: false},
		{name: "nested file locator path stays private", source: "github:acme/file:///Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "acme/file:///Users/alice/private", wantPublic: false},
		{name: "Windows locator path stays private", source: "git:github.com/C:/Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "C:/Users/alice/private", wantPublic: false},
		{name: "nested Windows locator path stays private", source: "git:github.com/acme/C:/Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "acme/C:/Users/alice/private", wantPublic: false},
		{name: "dot relative locator path stays private", source: "git:github.com/./private", wantOK: true, wantHost: "github.com", wantPath: "./private", wantPublic: false},
		{name: "github shorthand", source: "github:acme/tools", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantPublic: true},
		{name: "no ref", source: "git:github.com/acme/tools", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantPublic: true},
		{name: "branch ref with slash", source: "git:github.com/acme/tools@feature/x", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "feature/x", wantPublic: true},
		{name: "machine-local absolute ref", source: "git:github.com/acme/tools@/Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "/Users/alice/private", wantPublic: false},
		{name: "file scheme ref", source: "git:github.com/acme/tools@file:/Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "file:/Users/alice/private", wantPublic: false},
		{name: "scheme-prefixed ref", source: "git:github.com/acme/tools@cdb:export", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "cdb:export", wantPublic: false},
		{name: "encoded traversal ref", source: "git:github.com/acme/tools@..%2f..%2fescape", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "..%2f..%2fescape", wantPublic: false},
		{name: "encoded at-sign ref", source: "git:github.com/acme/tools@v1%40{push}", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1%40{push}", wantPublic: false},
		{name: "encoded control ref", source: "git:github.com/acme/tools@v1%00x", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1%00x", wantPublic: false},
		{name: "encoded branch slash stays public", source: "git:github.com/acme/tools@feature%2fx", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "feature%2fx", wantPublic: true},
		{name: "doubly encoded local ref", source: "git:github.com/acme/tools@%252FUsers%252Falice%252Fprivate", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "%252FUsers%252Falice%252Fprivate", wantPublic: false},
		{name: "unresolved deep escape ref", source: "git:github.com/acme/tools@%25252525253Dx", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "%25252525253Dx", wantPublic: false},
		{name: "bare scp source", source: "git@github.com:acme/pi-tools", wantOK: true, wantHost: "github.com", wantPath: "acme/pi-tools", wantPublic: true},
		{name: "scp hash ref", source: "git:git@github.com:acme/tools.git#v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "shorthand hash ref", source: "github:acme/tools#v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "hash ref carries file scheme local path", source: "github:acme/tool#file:/Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "acme/tool", wantRef: "file:/Users/alice/private", wantPublic: false},
		{name: "hash ref carries absolute local path", source: "github:acme/tool#/Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "acme/tool", wantRef: "/Users/alice/private", wantPublic: false},
		{name: "ref suffix with local fragment part", source: "github:acme/tool@v1#file:/Users/alice/private", wantOK: true, wantHost: "github.com", wantPath: "acme/tool", wantRef: "v1#file:/Users/alice/private", wantPublic: false},
		{name: "ref suffix with plain fragment part", source: "github:acme/tool@v1#main", wantOK: true, wantHost: "github.com", wantPath: "acme/tool", wantRef: "v1#main", wantPublic: true},
		{name: "URL fragment becomes ref", source: "git:https://example.com/acme/tool.git#v1", wantOK: true, wantHost: "example.com", wantPath: "acme/tool", wantRef: "v1", wantPublic: true},
		{name: "URL ref suffix with fragment rejected", source: "git:https://example.com/acme/tool.git@v1#file:/Users/alice/private", wantOK: false},
		{name: "URL ref suffix with plain fragment rejected", source: "git:https://example.com/acme/tool.git@v1#main", wantOK: false},
		{name: "home relative ref", source: "git:github.com/acme/tools@~/private", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "~/private", wantPublic: false},
		{name: "traversal ref", source: "git:github.com/acme/tools@feature/../../escape", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "feature/../../escape", wantPublic: false},
		{name: "windows path ref", source: "git:github.com/acme/tools@C:/private/repo", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "C:/private/repo", wantPublic: false},
		{name: "nested windows path ref", source: "git:github.com/acme/tools@feature/C:/private/repo", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "feature/C:/private/repo", wantPublic: false},
		{name: "nested at-sign ref", source: "git:github.com/acme/tools@v1@{push}", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1@{push}", wantPublic: false},
		{name: "nested URL userinfo has no authority", source: "git:https://user:actual-secret@example.com/acme/tool.git", wantOK: true, wantHost: "example.com", wantPath: "acme/tool"},
		{name: "nested URL bare userinfo has no authority", source: "git:https://user@example.com/acme/tool.git", wantOK: true, wantHost: "example.com", wantPath: "acme/tool"},
		{name: "shorthand password userinfo has no authority", source: "git:user:actual-secret@github.com/acme/tool", wantOK: true, wantHost: "github.com", wantPath: "acme/tool"},
		{name: "encoded shorthand password userinfo rejected", source: "git:user:actual-secret%40github.com/acme/tool", wantOK: false},
		{name: "nested URL query rejected", source: "git:https://example.com/acme/tool.git?token=secret", wantOK: false},
		{name: "ssh user stays admissible", source: "ssh://git@github.com/acme/tools.git@v1", wantOK: true, wantHost: "github.com", wantPath: "acme/tools", wantRef: "v1", wantPublic: true},
		{name: "unsafe traversal path rejected", source: "git:git@evil.example:../../victim/repo", wantOK: false},
		{name: "double encoded traversal path rejected", source: "git+https://example.com/acme/%252e%252e/escape.git#v1", wantOK: false},
		{name: "double encoded absolute path rejected", source: "git+https://example.com/%252FUsers/alice/private.git#v1", wantOK: false},
		{name: "double encoded Windows separator rejected", source: "git+https://example.com/%255CUsers/alice/private.git#v1", wantOK: false},
		{name: "hostless git prefix rejected", source: "git:tools", wantOK: false},
		{name: "local path is not git", source: "./plugins/tool", wantOK: false},
		{name: "npm spec is not git", source: "npm:tool", wantOK: false},
	}
	for _, test := range tests {
		source, ok := ParseGitSource(test.source)
		if ok != test.wantOK {
			t.Errorf("%s: ParseGitSource(%q) ok = %t, want %t", test.name, test.source, ok, test.wantOK)
			continue
		}
		if !test.wantOK {
			continue
		}
		if source.host != test.wantHost ||
			source.path != test.wantPath ||
			source.ref != test.wantRef ||
			source.Public() != test.wantPublic {
			t.Errorf(
				"%s: ParseGitSource(%q) = host %q path %q ref %q public %t, want host %q path %q ref %q public %t",
				test.name, test.source,
				source.host, source.path, source.ref, source.Public(),
				test.wantHost, test.wantPath, test.wantRef, test.wantPublic,
			)
		}
		if source.Identity() != test.wantHost+"/"+test.wantPath {
			t.Errorf("%s: Identity() = %q, want %q", test.name, source.Identity(), test.wantHost+"/"+test.wantPath)
		}
	}
}

func TestParseGitSourceIdentityNeverCarriesUserinfo(t *testing.T) {
	source, ok := ParseGitSource("ssh://git@github.com/acme/tools.git")
	if !ok {
		t.Fatal("ParseGitSource rejected ssh user source")
	}
	if source.Identity() != "github.com/acme/tools" {
		t.Fatalf("Identity() = %q, want userinfo-free locator", source.Identity())
	}
}

func TestParseGitSourceSeparatesStructureFromCredentialAuthority(t *testing.T) {
	for _, value := range []string{
		"git+https://user@example.com/acme/tools.git",
		"git+https://user:actual-secret@example.com/acme/tools.git",
		"git:https://user:actual-secret@example.com/acme/tool.git",
		"git:user:actual-secret@github.com/acme/tool",
	} {
		source, ok := ParseGitSource(value)
		if !ok {
			t.Fatalf("ParseGitSource(%q) lost structural Git identity", value)
		}
		if source.CredentialFree() || source.Public() {
			t.Fatalf("ParseGitSource(%q) gained credential/public authority", value)
		}
	}
}

func TestPathSafeGitHostMatchesPortableInstallLayout(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "github.com", want: true},
		{host: "short+host", want: true},
		{host: "127.0.0.1", want: true},
		{host: "224.0.0.1", want: true},
		{host: "127.1", want: true},
		{host: "0x7f.0.0.1", want: true},
		{host: "127.0.0.1:8080", want: false},
		{host: "2001:db8::1", want: false},
		{host: "GitHub.com", want: false},
		{host: "", want: false},
	}
	for _, test := range tests {
		if got := PathSafeGitHost(test.host); got != test.want {
			t.Errorf("PathSafeGitHost(%q) = %t, want %t", test.host, got, test.want)
		}
	}
}

func TestPublicGitHostClassifiesAliasesAndSpecialPurpose(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "github.com", want: true},
		{host: "8.8.8.8", want: true},
		{host: "127.1", want: false},
		{host: "10.1", want: false},
		{host: "0x7f.0.0.1", want: false},
		{host: "0177.0.0.1", want: false},
		{host: "0.1.2.3", want: false},
		{host: "198.18.0.1", want: false},
		{host: "192.0.2.1", want: false},
		{host: "2001:db8::1", want: false},
		{host: "64:ff9b:1::1", want: false},
		{host: "100:0:0:1::1", want: false},
		{host: "3fff::1", want: false},
		{host: "5f00::1", want: false},
		{host: "2002::1", want: false},
		{host: "192.0.0.1", want: false},
		{host: "192.0.0.9", want: true},
		{host: "192.0.0.10", want: true},
		{host: "64:ff9b::1", want: true},
		{host: "2001:1::1", want: true},
		{host: "127.0.0.1 :8080", want: false},
		{host: "8.8.8.8:443", want: true},
		{host: "8.8.8.8..", want: true},
		{host: "127.0.0.1..", want: false},
	}
	for _, test := range tests {
		if got := publicGitHost(test.host); got != test.want {
			t.Errorf("publicGitHost(%q) = %t, want %t", test.host, got, test.want)
		}
	}
}

func TestPublicGitLocatorPathScalesLinearlyWithSegments(t *testing.T) {
	path := strings.Repeat("a/", 16384) + "tool"
	start := time.Now()
	if !publicGitLocatorPath(path) {
		t.Fatal("publicGitLocatorPath rejected a long public locator")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("publicGitLocatorPath took %s for %d bytes, want linear scan", elapsed, len(path))
	}
}
