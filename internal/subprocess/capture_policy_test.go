package subprocess

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPolicySanitizeHostileCorpus(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		secrets   []string
		want      string
		redacted  bool
		truncated bool
	}{
		{name: "unchanged", value: "ordinary output", want: "ordinary output"},
		{name: "explicit", value: "value=long-secret", secrets: []string{"long-secret"}, want: "value=[REDACTED]", redacted: true},
		{name: "overlapping explicit", value: "abcdef abc", secrets: []string{"abc", "abcdef", "abc", ""}, want: "[REDACTED] [REDACTED]", redacted: true},
		{name: "quoted JSON key", value: `{"token":"unknown value"}`, want: `{"token":[REDACTED]}`, redacted: true},
		{name: "quoted JSON key preserves sibling", value: `{"token":"unknown value","status":"failed"}`, want: `{"token":[REDACTED],"status":"failed"}`, redacted: true},
		{name: "quoted YAML value", value: `secret: 'quoted value'`, want: "secret: [REDACTED]", redacted: true},
		{name: "escaped quote", value: `password="a\"b"`, want: "password=[REDACTED]", redacted: true},
		{name: "unterminated quote", value: `password="unterminated`, want: "password=[REDACTED]", redacted: true},
		{name: "case and key spelling", value: "API-KEY = value", want: "API-KEY = [REDACTED]", redacted: true},
		{name: "private token", value: "private_token=boundary-secret", want: "private_token=[REDACTED]", redacted: true},
		{name: "access token", value: `"access-token":"boundary-secret"`, want: `"access-token":[REDACTED]`, redacted: true},
		{name: "client secret", value: "client_secret: boundary-secret", want: "client_secret: [REDACTED]", redacted: true},
		{name: "AWS secret access key", value: "AWS_SECRET_ACCESS_KEY=boundary-secret", want: "AWS_SECRET_ACCESS_KEY=[REDACTED]", redacted: true},
		{name: "AWS access key ID", value: "AWS_ACCESS_KEY_ID=boundary-secret", want: "AWS_ACCESS_KEY_ID=[REDACTED]", redacted: true},
		{name: "secret key", value: "SECRET_KEY=boundary-secret", want: "SECRET_KEY=[REDACTED]", redacted: true},
		{name: "camel access token", value: "accessToken=boundary-secret", want: "accessToken=[REDACTED]", redacted: true},
		{name: "camel client secret", value: "clientSecret=boundary-secret", want: "clientSecret=[REDACTED]", redacted: true},
		{name: "camel API key", value: "APIKey=boundary-secret", want: "APIKey=[REDACTED]", redacted: true},
		{name: "camel secret key", value: "secretKey=boundary-secret", want: "secretKey=[REDACTED]", redacted: true},
		{name: "kebab private key", value: "private-key=boundary-secret", want: "private-key=[REDACTED]", redacted: true},
		{name: "passphrase", value: "sshPassphrase=boundary-secret", want: "sshPassphrase=[REDACTED]", redacted: true},
		{name: "authorization scheme", value: "Authorization: Bearer boundary-secret", want: "Authorization: [REDACTED]", redacted: true},
		{name: "authorization preserves later line", value: "Authorization: Bearer boundary-secret\nstatus=failed", want: "Authorization: [REDACTED]\nstatus=failed", redacted: true},
		{name: "credential after benign field", value: "status=failed Authorization: Bearer boundary-secret", want: "status=failed Authorization: [REDACTED]", redacted: true},
		{name: "credentials on separate lines", value: "token=first\nstatus=failed Authorization: Basic second", want: "token=[REDACTED]\nstatus=failed Authorization: [REDACTED]", redacted: true},
		{name: "credential preserves later evidence", value: "access_token=boundary-secret cache=/machine/path", want: "access_token=[REDACTED] cache=/machine/path", redacted: true},
		{name: "multiline separator", value: "token: \n value", want: "token: \n [REDACTED]", redacted: true},
		{name: "CRLF separator", value: "token:\r\n value", want: "token:\r\n [REDACTED]", redacted: true},
		{name: "unicode value", value: "token=秘密", want: "token=[REDACTED]", redacted: true},
		{name: "explicit value matches key", value: "token=unlisted-value", secrets: []string{"token"}, want: "[REDACTED]=[REDACTED]", redacted: true},
		{name: "embedded key text", value: "mytoken=value", want: "mytoken=value"},
		{name: "non-secret compound key", value: "token_count=2", want: "token_count=2"},
		{name: "benign SGR removed", value: "\x1b[31mfailed\x1b[0m", want: "failed"},
		{name: "SGR split explicit", value: "sec\x1b[31mret", secrets: []string{"secret"}, want: "[REDACTED]", redacted: true},
		{name: "SGR inside explicit secret", value: "sec\x1b[31mret", secrets: []string{"sec\x1b[31mret"}, want: "[REDACTED]", redacted: true},
		{name: "cursor control rejected", value: "visible\x1b[2Drewrite", want: "[REDACTED]", redacted: true},
		{name: "OSC control rejected", value: "visible\x1b]8;;https://example.test\x07link", want: "[REDACTED]", redacted: true},
		{name: "incomplete escape rejected", value: "visible\x1b[31", want: "[REDACTED]", redacted: true},
		{name: "standalone carriage return rejected", value: "secret\rcover", want: "[REDACTED]", redacted: true},
		{name: "bidi override rejected", value: "visible\u202ereordered", want: "[REDACTED]", redacted: true},
		{name: "bidi isolate rejected", value: "visible\u2066isolated", want: "[REDACTED]", redacted: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := NewCapturePolicy(test.secrets, 1024).Sanitize(test.value, false)
			if result.Text() != test.want || result.Redacted() != test.redacted || result.Truncated() != test.truncated {
				t.Fatalf("result = %q redacted=%t truncated=%t, want %q/%t/%t", result.Text(), result.Redacted(), result.Truncated(), test.want, test.redacted, test.truncated)
			}
		})
	}
}

func TestPolicySanitizeRedactsBeforeRuneBounding(t *testing.T) {
	result := NewCapturePolicy([]string{"secret"}, 8).Sanitize("123456secret-tail", false)
	if strings.Contains(result.Text(), "se") || result.Text() != "123456[R\n[truncated]" {
		t.Fatalf("text = %q, want redaction before bounding", result.Text())
	}
	if !result.Redacted() || !result.Truncated() {
		t.Fatalf("flags = redacted:%t truncated:%t, want true/true", result.Redacted(), result.Truncated())
	}

	unicodeResult := NewCapturePolicy(nil, 2).Sanitize("ééé", false)
	if unicodeResult.Text() != "éé\n[truncated]" || !unicodeResult.Truncated() {
		t.Fatalf("unicode result = %q truncated=%t", unicodeResult.Text(), unicodeResult.Truncated())
	}
}

func TestPolicySanitizeRedactsTruncatedSecretPrefix(t *testing.T) {
	result := NewCapturePolicy([]string{"super-secret"}, 1024).Sanitize("runner saw super-", true)
	if result.Text() != "runner saw [REDACTED]" || !result.Redacted() || !result.Truncated() {
		t.Fatalf("result = %q redacted=%t truncated=%t", result.Text(), result.Redacted(), result.Truncated())
	}

	unchanged := NewCapturePolicy([]string{"super-secret"}, 1024).Sanitize("runner stopped elsewhere", true)
	if unchanged.Text() != "runner stopped elsewhere" || unchanged.Redacted() || !unchanged.Truncated() {
		t.Fatalf("unrelated result = %q redacted=%t truncated=%t", unchanged.Text(), unchanged.Redacted(), unchanged.Truncated())
	}
}

func TestPolicySanitizeRedactsByteSplitUnicodeSecret(t *testing.T) {
	const secret = "秘密-token"
	prefix := string([]byte(secret)[:4])
	result := NewCapturePolicy([]string{secret}, 1024).Sanitize("runner saw "+prefix, true)
	if result.Text() != "runner saw [REDACTED]" || !result.Redacted() || !result.Truncated() {
		t.Fatalf("result = %q redacted=%t truncated=%t", result.Text(), result.Redacted(), result.Truncated())
	}
	if !utf8.ValidString(result.Text()) {
		t.Fatalf("result is not valid UTF-8: %q", result.Text())
	}
}

func TestPolicySanitizeNormalizesInvalidUTF8AndCopiesSecrets(t *testing.T) {
	secrets := []string{"copy-me"}
	policy := NewCapturePolicy(secrets, 1024)
	secrets[0] = "changed"
	result := policy.Sanitize(string([]byte{'x', 0xff, 'y'})+" copy-me", false)
	if !utf8.ValidString(result.Text()) || result.Text() != "x�y [REDACTED]" || !result.Redacted() {
		t.Fatalf("result = %q valid=%t redacted=%t", result.Text(), utf8.ValidString(result.Text()), result.Redacted())
	}
}

func TestPolicySanitizeZeroLimitRetainsNoPayload(t *testing.T) {
	result := NewCapturePolicy([]string{"secret"}, 0).Sanitize("secret", false)
	if result.Text() != "\n[truncated]" || !result.Redacted() || !result.Truncated() {
		t.Fatalf("result = %q redacted=%t truncated=%t", result.Text(), result.Redacted(), result.Truncated())
	}
}

func TestPolicySanitizeNegativeLimitRetainsNoPayload(t *testing.T) {
	result := NewCapturePolicy(nil, -1).Sanitize("visible", false)
	if result.Text() != "\n[truncated]" || result.Redacted() || !result.Truncated() {
		t.Fatalf("result = %q redacted=%t truncated=%t", result.Text(), result.Redacted(), result.Truncated())
	}
}
