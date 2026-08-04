package refresh

import (
	"errors"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestRefusedResultKeepsOnlyBoundedRedactedFailureDetail(t *testing.T) {
	const (
		manifestPath = "/private/workspace/daem.toml"
		secret       = "do-not-publish"
	)
	result := baseResult(daempaths.Paths{
		ManifestPath:  manifestPath,
		ManifestRoot:  "/private/workspace",
		LockfilePath:  "/private/workspace/daem.lock.toml",
		StateDir:      "/private/workspace/.daem",
		StatefilePath: "/private/workspace/.daem/state.json",
		DataDir:       "/private/data/daem",
	}, ModeDryRun)
	cause := errors.New(
		"read " + manifestPath + ": token=" + secret + " " +
			strings.Repeat("x", 4096),
	)

	refused, err := refusedResult(
		result,
		ReasonManifestUnavailable,
		cause,
		"fix the selected manifest and retry",
	)
	if err == nil {
		t.Fatal("refusedResult returned nil error")
	}
	if strings.Contains(refused.FailureDetail(), manifestPath) ||
		strings.Contains(refused.FailureDetail(), secret) {
		t.Fatalf("failure detail leaked a path or secret: %q", refused.FailureDetail())
	}
	if !strings.Contains(refused.FailureDetail(), "[REDACTED]") {
		t.Fatalf("failure detail did not report redaction: %q", refused.FailureDetail())
	}
	if got := len([]rune(refused.FailureDetail())); got > maximumFailureDetailRunes {
		t.Fatalf("failure detail runes = %d, want <= %d", got, maximumFailureDetailRunes)
	}
}

func TestRedactMachineLocalPathsPreservesPortableIdentities(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: `open /Users/alice/private/config.json: denied`, want: `open [REDACTED]`},
		{input: `open /Users/alice/My Config/config.json: denied`, want: `open [REDACTED]`},
		{input: `open /Users/alice/private,token/file.json: denied`, want: `open [REDACTED]`},
		{input: `open /Users/alice/private;token/(file).json: denied`, want: `open [REDACTED]`},
		{input: `open /Users/alice/private/name: user.txt: denied`, want: `open [REDACTED]`},
		{input: "open /Users/alice/private/name: user.txt: denied\nretryable", want: "open [REDACTED]\nretryable"},
		{input: `open "/Users/alice/private/name: user.txt": denied`, want: `open "[REDACTED]": denied`},
		{input: `paths=portable,/Users/alice/private/file.json`, want: `paths=portable,[REDACTED]`},
		{input: `paths=portable,C:\\Users\\alice\\private.json`, want: `paths=portable,[REDACTED]`},
		{input: `open "/Users/alice/My Config/config.json": denied`, want: `open "[REDACTED]": denied`},
		{input: `read C:\\Users\\alice\\private.json: denied`, want: `read [REDACTED]`},
		{input: `read C:\\Users\\alice\\private,token\\file.json: denied`, want: `read [REDACTED]`},
		{input: `read "C:\\Users\\alice\\private,token\\file.json": denied`, want: `read "[REDACTED]": denied`},
		{input: `read file:///home/alice/private.json: denied`, want: `read [REDACTED]`},
		{input: `read FILE:/home/alice/private.json: denied`, want: `read [REDACTED]`},
		{input: `read ../private/config.json: denied`, want: `read [REDACTED]`},
		{input: `open:/Users/alice/private.json denied`, want: `open:[REDACTED]`},
		{input: `source https://example.test/plugin and npm:@acme/tool`, want: `source https://example.test/plugin and npm:@acme/tool`},
	}
	for _, test := range tests {
		if got := redactMachineLocalPaths(test.input); got != test.want {
			t.Errorf("redactMachineLocalPaths(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestSanitizedFailureDetailRedactsCompoundCredentialKeys(t *testing.T) {
	for _, key := range []string{"private_token", "access_token", "client_secret"} {
		detail := sanitizedFailureDetail(errors.New(key + "=boundary-secret"))
		if detail != key+"=[REDACTED]" {
			t.Errorf("sanitizedFailureDetail(%q) = %q", key, detail)
		}
	}
}

func TestSanitizedFailureDetailRejectsBidirectionalControls(t *testing.T) {
	for _, control := range []string{"\u202e", "\u2066"} {
		detail := sanitizedFailureDetail(errors.New("visible" + control + "reordered"))
		if detail != "[REDACTED]" {
			t.Errorf("sanitizedFailureDetail(%q) = %q", control, detail)
		}
	}
}

func TestSanitizedFailureDetailReappliesBoundAfterPathRedaction(t *testing.T) {
	detail := sanitizedFailureDetail(errors.New(strings.Repeat("/a ", 1024)))
	if got := len([]rune(detail)); got > maximumFailureDetailRunes {
		t.Fatalf("failure detail runes = %d, want <= %d", got, maximumFailureDetailRunes)
	}
	if strings.Contains(detail, "/a") {
		t.Fatalf("failure detail leaked a repeated path: %q", detail)
	}
}
