package credentialtext

import (
	"strings"
	"testing"
)

func TestCredentialRecognitionUsesIdentifierBoundaries(t *testing.T) {
	tests := []struct {
		value      string
		credential bool
		assignment bool
	}{
		{value: "npm:tool@token:actual-secret", credential: true},
		{value: `plugins\client-secret=actual-secret`, credential: true, assignment: true},
		{value: "private_token=secret", credential: true, assignment: true},
		{value: "authorization_code=secret", credential: true, assignment: true},
		{value: "authCode=secret", credential: true, assignment: true},
		{value: `{"token":"secret"}`, credential: true},
		{value: "--token=actual-secret", credential: true, assignment: true},
		{value: "--api-key=actual-secret", credential: true, assignment: true},
		{value: "-password=actual-secret", credential: true, assignment: true},
		{value: "npm:tool@--client-secret=actual-secret", credential: true, assignment: true},
		{value: `run "--authCode=quoted-secret" done`, credential: true, assignment: true},
		{value: "--verbose --count 3", assignment: false},
		{value: "token_count=2", assignment: true},
		{value: "mytoken=value", assignment: true},
		{value: "package@1.2.3"},
	}
	for _, test := range tests {
		if got := ContainsCredential(test.value); got != test.credential {
			t.Errorf("ContainsCredential(%q) = %t, want %t", test.value, got, test.credential)
		}
		if got := ContainsAssignment(test.value); got != test.assignment {
			t.Errorf("ContainsAssignment(%q) = %t, want %t", test.value, got, test.assignment)
		}
	}
}

func TestCredentialAssignmentRecognitionLeavesColonToOwningGrammars(t *testing.T) {
	for _, value := range []string{
		"token=actual-secret",
		"token%3Dactual-secret",
	} {
		if !ContainsCredentialAssignment(value) {
			t.Errorf("ContainsCredentialAssignment(%q) = false, want true", value)
		}
	}
	for _, value := range []string{
		"token:selector-data",
		"https://token:443/repo",
	} {
		if ContainsCredentialAssignment(value) {
			t.Errorf("ContainsCredentialAssignment(%q) = true, want false", value)
		}
	}
}

func TestRedactPreservesNonCredentialEvidence(t *testing.T) {
	value := `status=failed npm:tool@token:actual-secret plugins\client-secret="quoted value"`
	got, redacted := Redact(value, "[REDACTED]", nil)
	want := `status=failed npm:tool@token:[REDACTED] plugins\client-secret=[REDACTED]`
	if !redacted || got != want {
		t.Fatalf("Redact() = %q/%t, want %q/true", got, redacted, want)
	}
}

func TestRedactOptionStyleCredentials(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "npm install --token=actual-secret ok", want: "npm install --token=[REDACTED] ok"},
		{value: "--api-key=actual-secret", want: "--api-key=[REDACTED]"},
		{value: "npm:tool@--client-secret=actual-secret", want: "npm:tool@--client-secret=[REDACTED]"},
		{value: "x--token=actual-secret", want: "x--token=[REDACTED]"},
	}
	for _, test := range tests {
		got, redacted := Redact(test.value, "[REDACTED]", nil)
		if !redacted || got != test.want {
			t.Errorf("Redact(%q) = %q/%t, want %q/true", test.value, got, redacted, test.want)
		}
	}
}

func TestCanonicalDecodeMapsDecodedBytesToRawSpans(t *testing.T) {
	decoded, offsets, stable := CanonicalDecode("a%3Db%2Fc")
	if decoded != "a=b/c" || !stable {
		t.Fatalf("decoded = %q/%t, want %q/true", decoded, stable, "a=b/c")
	}
	wantOffsets := []int32{0, 1, 4, 5, 8, 9}
	if len(offsets) != len(wantOffsets) {
		t.Fatalf("offsets = %v, want %v", offsets, wantOffsets)
	}
	for index, want := range wantOffsets {
		if offsets[index] != want {
			t.Fatalf("offsets[%d] = %d, want %d", index, offsets[index], want)
		}
	}

	unchanged, nilOffsets, unchangedStable := CanonicalDecode("plain-text")
	if unchanged != "plain-text" || nilOffsets != nil || !unchangedStable {
		t.Fatalf("plain decode = %q/%v/%t, want identity mapping", unchanged, nilOffsets, unchangedStable)
	}
	malformed, malformedOffsets, malformedStable := CanonicalDecode("bad%zz-escape")
	if malformed != "bad%zz-escape" || malformedOffsets != nil || malformedStable {
		t.Fatalf("malformed decode = %q/%v/%t, want literal passthrough and unstable form", malformed, malformedOffsets, malformedStable)
	}
}

func TestCanonicalDecodeReachesBoundedFixedPoint(t *testing.T) {
	// Two encoding layers resolve within the shared round budget and keep a
	// composed map back to the raw spans.
	decoded, offsets, stable := CanonicalDecode("a%253Db")
	if decoded != "a=b" || !stable {
		t.Fatalf("decoded = %q/%t, want %q/true", decoded, stable, "a=b")
	}
	if want := []int32{0, 1, 6, 7}; len(offsets) != len(want) {
		t.Fatalf("offsets = %v, want %v", offsets, want)
	} else {
		for index, wantOffset := range want {
			if offsets[index] != wantOffset {
				t.Fatalf("offsets[%d] = %d, want %d", index, offsets[index], wantOffset)
			}
		}
	}

	// Beyond the budget the form stays unstable so admission and disclosure
	// boundaries fail closed instead of trusting an unresolved escape. The
	// inspector itself stays best-effort; the stable flag carries the
	// fail-closed decision to each boundary.
	deep := "a%25252525253Db"
	_, _, deepStable := CanonicalDecode(deep)
	if deepStable {
		t.Fatalf("CanonicalDecode(%q) reported a stable form, want unresolved escapes", deep)
	}
}

func TestRedactMergesOverlappingCredentialSpans(t *testing.T) {
	// URL userinfo and the credential field inside it overlap; the merged
	// render must not re-emit the longer span's tail.
	got, redacted := Redact("https://token=secret@password=actual-secret", "[REDACTED]", nil)
	if got != "https://[REDACTED]" || !redacted {
		t.Fatalf("Redact = %q/%t, want %q/true", got, redacted, "https://[REDACTED]")
	}

	var fields []string
	for index := range 4 {
		fields = append(fields, "token=secret"+string(rune('0'+index))+"%20suffix"+string(rune('0'+index)))
	}
	got, redacted = Redact(strings.Join(fields, " "), "[REDACTED]", nil)
	if !redacted || strings.Contains(got, "secret") || strings.Contains(got, "suffix") {
		t.Fatalf("Redact = %q/%t, want every encoded field fully covered", got, redacted)
	}
}

func TestRedactSurvivesMalformedAndDeepEscapes(t *testing.T) {
	// An unresolved escape leaves the value uninspectable, so projection
	// fails closed and withholds the whole value.
	got, redacted := Redact("--token%3Dactual-secret trailing%zz", "[REDACTED]", nil)
	if got != "[REDACTED]" || !redacted {
		t.Fatalf("Redact = %q/%t, want the uninspectable value withheld", got, redacted)
	}
	got, redacted = Redact("--token%25252525253Dactual-secret", "[REDACTED]", nil)
	if got != "[REDACTED]" || !redacted {
		t.Fatalf("Redact = %q/%t, want the beyond-budget value withheld", got, redacted)
	}
	got, redacted = Redact("--token%253Dactual-secret", "[REDACTED]", nil)
	if got != "--token%253D[REDACTED]" || !redacted {
		t.Fatalf("Redact = %q/%t, want doubly encoded credential redacted", got, redacted)
	}
}

func TestCanonicalDecodeTreatsDecodedPercentLiteralsAsStable(t *testing.T) {
	tests := []struct {
		value   string
		decoded string
		stable  bool
	}{
		{value: "100%25ready", decoded: "100%ready", stable: true},
		{value: "https://example.com/acme/100%25-tool.git", decoded: "https://example.com/acme/100%-tool.git", stable: true},
		{value: "%25zz", decoded: "%zz", stable: true},
		{value: "%2525zz", decoded: "%zz", stable: true},
		{value: "%2525", decoded: "%", stable: true},
		{value: "100%25ready%253Aok", decoded: "100%ready:ok", stable: true},
		{value: "100%zz", decoded: "100%zz", stable: false},
		{value: "trailing%2", decoded: "trailing%2", stable: false},
		{value: "a%25%zz", decoded: "a%25%zz", stable: false},
		{value: "%25252525253D", decoded: "%253D", stable: false},
	}
	for _, test := range tests {
		decoded, _, stable := CanonicalDecode(test.value)
		if stable != test.stable {
			t.Errorf("CanonicalDecode(%q) stable = %t, want %t", test.value, stable, test.stable)
		}
		if test.stable && decoded != test.decoded {
			t.Errorf("CanonicalDecode(%q) = %q, want %q", test.value, decoded, test.decoded)
		}
	}
}

func TestRedactWithholdsUnsupportedKeyValueToLineEnd(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "ключ: Bearer actual-secret", want: "ключ: [REDACTED]"},
		{value: "ключ: Bearer actual-secret\nnext line", want: "ключ: [REDACTED]\nnext line"},
		{value: "ключ: \"Bearer actual-secret\" trailing", want: "ключ: [REDACTED]"},
		{value: "ключ=actual-secret tail", want: "ключ=[REDACTED]"},
		{value: "ключ: \"Bearer first-part\nactual-secret\" tail", want: "ключ: [REDACTED]"},
		{value: "ключ: prefix \"first\r\nactual-secret\"", want: "ключ: [REDACTED]"},
		{value: "ключ: prefix \"first\nactual-secret\"\nnext line", want: "ключ: [REDACTED]\nnext line"},
		{value: "ключ: prefix \"public\" suffix \"first\r\nactual-secret\"", want: "ключ: [REDACTED]"},
	}
	for _, test := range tests {
		got, redacted := Redact(test.value, "[REDACTED]", nil)
		if got != test.want || !redacted {
			t.Errorf("Redact(%q) = %q/%t, want %q redacted", test.value, got, redacted, test.want)
		}
	}
}

func TestRedactRecognizesControlSplitCredentialKeys(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "--to%00ken=actual-secret", want: "--to%00ken=[REDACTED]"},
		{value: "--to%09ken=actual-secret", want: "--to%09ken=[REDACTED]"},
		{value: "--to%0Aken=actual-secret", want: "--to%0Aken=[REDACTED]"},
		{value: "--to%00ken=actual-secret leftover", want: "--to%00ken=[REDACTED]"},
		{value: "access%00key=actual-secret", want: "access%00key=[REDACTED]"},
		{value: "se%00cret=actual-secret", want: "se%00cret=[REDACTED]"},
		{value: "authori%00zation: Bearer actual-secret", want: "authori%00zation: [REDACTED]"},
		{value: "--to%0Dken=actual-secret", want: "--to%0Dken=[REDACTED]"},
		{value: "foo\ntoken=actual-secret", want: "foo\ntoken=[REDACTED]"},
		{value: "Bearer%00actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea%09rer actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea%0Arer actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea%0Drer actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea\trer actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea\nrer actual-secret leftover", want: "[REDACTED]"},
	}
	for _, test := range tests {
		got, redacted := Redact(test.value, "[REDACTED]", nil)
		if got != test.want || !redacted {
			t.Errorf("Redact(%q) = %q/%t, want %q redacted", test.value, got, redacted, test.want)
		}
	}
	for _, value := range []string{
		"name%09path=/tmp/foo",
		"status=failed leftover",
		"--to%00ken leftover",
	} {
		got, redacted := Redact(value, "[REDACTED]", nil)
		if redacted || got != value {
			t.Errorf("Redact(%q) = %q/%t, want unchanged", value, got, redacted)
		}
	}
}

func TestRedactTracksMultilineQuotedKnownCredentials(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "Authorization: \"Bearer first-part\nactual-secret\"", want: "Authorization: [REDACTED]"},
		{value: "Authorization: Bearer actual-secret\nnext line", want: "Authorization: [REDACTED]\nnext line"},
		{value: "Authorization: \"Bearer first\" inherited-secret", want: "Authorization: [REDACTED]"},
		{value: "Authorization: Bearer \"first\r\nactual-secret\"", want: "Authorization: [REDACTED]"},
		{value: "Authorization: Bearer \"first\nactual-secret\" leftover", want: "Authorization: [REDACTED]"},
		{value: "Authorization: Bearer \"unclosed\nactual-secret", want: "Authorization: [REDACTED]"},
		{value: "Authorization: Digest realm=\"public\", response=\"first\r\nactual-secret\"", want: "Authorization: [REDACTED]"},
		{value: "Authorization: \"a\" leftover \"first\nactual-secret\"", want: "Authorization: [REDACTED]"},
		{value: "token: Bearer actual-secret", want: "token: [REDACTED]"},
		{value: "password=Bearer actual-secret", want: "password=[REDACTED]"},
		{value: "secret: Bearer \"first\r\nactual-secret\"", want: "secret: [REDACTED]"},
		{value: "token: Digest realm=\"public\", response=\"first\r\nactual-secret\"", want: "token: [REDACTED]"},
		{value: "token: Bearer actual-secret\nstatus=failed", want: "token: [REDACTED]\nstatus=failed"},
		{value: "Authorization: Digest realm=%22public%22, response=%22first%0D%0Aactual-secret%22", want: "Authorization: [REDACTED]"},
		{value: "password=\"first actual-secret", want: "password=[REDACTED]"},
		{value: "password='first actual-secret", want: "password=[REDACTED]"},
		{value: "password=\"first\nactual-secret", want: "password=[REDACTED]"},
		{value: "secret=\"first actual-secret\nstatus=failed", want: "secret=[REDACTED]"},
	}
	for _, test := range tests {
		got, redacted := Redact(test.value, "[REDACTED]", nil)
		if got != test.want || !redacted {
			t.Errorf("Redact(%q) = %q/%t, want %q redacted", test.value, got, redacted, test.want)
		}
	}
}

func TestCredentialGrammarClassifiesUnsupportedKeysClosed(t *testing.T) {
	for _, value := range []string{
		"github:acme/_token=actual-secret",
		"github:acme/" + strings.Repeat("a", 129) + "-token=actual-secret",
		"ключ=actual-secret",
		"ключ: actual-secret",
		"--to%00ken=actual-secret",
		"--to%09ken=actual-secret",
		"--to%0Aken=actual-secret",
	} {
		if !ContainsCredential(value) {
			t.Errorf("ContainsCredential(%q...) = false, want fail-closed true", value[:min(len(value), 40)])
		}
	}
	for _, value := range []string{
		"https://example.com/tool",
		"12:30",
		"C:/path",
		"a+b=x",
		"git@github.com:acme/pi-tools",
	} {
		if ContainsCredential(value) {
			t.Errorf("ContainsCredential(%q) = true, want classified non-credential", value)
		}
	}
}

func TestRedactCoversUnderscoreAndUnsupportedKeyAssignments(t *testing.T) {
	got, redacted := Redact("x _token=actual-secret y", "[REDACTED]", nil)
	if got != "x _token=[REDACTED] y" || !redacted {
		t.Fatalf("Redact = %q/%t, want underscore-led key value redacted", got, redacted)
	}
	got, redacted = Redact("ключ=actual-secret", "[REDACTED]", nil)
	if got != "ключ=[REDACTED]" || !redacted {
		t.Fatalf("Redact = %q/%t, want unsupported key value redacted", got, redacted)
	}
}

func TestRedactCoversExplicitSecretsAcrossDecodedForms(t *testing.T) {
	got, redacted := Redact("loading actual%20secret%2Fvalue done", "[REDACTED]", []string{"actual secret/value"})
	if got != "loading [REDACTED] done" || !redacted {
		t.Fatalf("Redact = %q/%t, want encoded explicit secret mapped back to its raw span", got, redacted)
	}
}

func TestContainsCredentialRecognizesEncodedShapes(t *testing.T) {
	for _, value := range []string{
		"npm:tool@%74oken=actual-secret",
		"npm:tool@token%3Dactual-secret",
		"npm:tool@token%3aactual-secret",
		"--token%3Dactual-secret",
	} {
		if !ContainsCredential(value) {
			t.Errorf("ContainsCredential(%q) = false, want true", value)
		}
	}
	if ContainsCredential("npm:tool%401.2.3") {
		t.Errorf("ContainsCredential recognized a benign encoded selector")
	}
}

func TestContainsURLUserInfoRecognizesEncodedAndMalformedShapes(t *testing.T) {
	for _, value := range []string{
		"https://user:secret@example.com/tool",
		"https://user:secret%40example.com/tool",
		"git:https://user:secret@[example.com/tool",
		"ssh://git@example.com/tool",
		"git:user:actual-secret@github.com/acme/tool",
		"user:actual-secret@github.com/acme/tool",
		"git:user:actual-secret%40github.com/acme/tool",
	} {
		if !ContainsURLUserInfo(value) {
			t.Errorf("ContainsURLUserInfo(%q) = false, want true", value)
		}
	}
	for _, value := range []string{
		"https://example.com/tool",
		"user@example.com",
		"git:git@github.com:acme/tools.git",
		"git@github.com:acme/pi-tools",
	} {
		if ContainsURLUserInfo(value) {
			t.Errorf("ContainsURLUserInfo(%q) = true, want false", value)
		}
	}
}

func TestInspectPasswordUserInfoRejectsPasswordLocatorsNotTransportUsers(t *testing.T) {
	for _, value := range []string{
		"https://user:secret@example.com/tool",
		"git:user:actual-secret@github.com/acme/tool",
		"git:user:actual-secret@github.com/acme/tool#https://example.test/ref",
		"user:actual-secret@github.com/acme/tool",
		"user:actual-secret@short-host/acme/tool",
		"user:actual-secret@[2001:db8::1]/acme/tool",
		"git:user:actual-secret@short-host:acme/tool",
		"git:user:actual-secret@[2001:db8::1]:acme/tool",
		"git:user:actual-secret%40github.com/acme/tool",
		"git:user:actual-secret@github.com",
		"user:actual-secret@short-host:443/acme/tool",
		"user:actual-secret@[2001:db8::1]:443/acme/tool",
		"https://example.test/#git:user:actual-secret@github.com/acme/tool",
		"https://example.test/?git:user:actual-secret@github.com/acme/tool",
		"user:actual-secret@short-host",
		"https://example.test/#user:actual-secret@short-host",
		"[git:user:actual-secret@short-host]",
		"npm:user:actual-secret@short-host",
		"NPM:user:actual-secret@short-host",
	} {
		if got := InspectPasswordUserInfo(value); got != UserInfoPresent {
			t.Errorf("InspectPasswordUserInfo(%q) present = false, want true", value)
		}
	}
	for _, value := range []string{
		"ssh://git@example.com/tool",
		"git:git@github.com:acme/tools.git",
		"git@github.com:acme/pi-tools",
		"user@example.com",
		"https://example.com/tool",
	} {
		if got := InspectPasswordUserInfo(value); got != UserInfoAbsent {
			t.Errorf("InspectPasswordUserInfo(%q) present = true, want false", value)
		}
	}
}

func TestInspectPasswordUserInfoSeparatesPresenceFromInspectability(t *testing.T) {
	inspection := InspectPasswordUserInfo(
		"git:user:actual-secret@github.com/acme/tool#https://example.test/ref",
	)
	if inspection != UserInfoPresent {
		t.Fatalf("InspectPasswordUserInfo credential = %v, want present", inspection)
	}

	inspection = InspectPasswordUserInfo("github:acme/tool#release%252525252525value")
	if inspection != UserInfoUninspectable {
		t.Fatalf("InspectPasswordUserInfo unresolved = %v, want uninspectable", inspection)
	}
}

func TestRedactUsesSchemeLessPasswordObservationsAsSpans(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{
			value: "fatal: git:user:actual-secret@github.com/acme/tool#https://example.test/ref",
			want:  "fatal: git:[REDACTED]@github.com/acme/tool#https://example.test/ref",
		},
		{
			value: "fatal: user:actual-secret@short-host/acme/tool",
			want:  "fatal: [REDACTED]@short-host/acme/tool",
		},
		{
			value: "fatal: user:actual-secret@[2001:db8::1]/acme/tool",
			want:  "fatal: [REDACTED]@[2001:db8::1]/acme/tool",
		},
		{
			value: "fatal: git:user:actual-secret@short-host:acme/tool",
			want:  "fatal: git:[REDACTED]@short-host:acme/tool",
		},
		{
			value: "fatal: git:user:actual-secret%40github.com/acme/tool",
			want:  "fatal: git:[REDACTED]%40github.com/acme/tool",
		},
		{
			value: "fatal: git:user:actual-secret@github.com",
			want:  "fatal: git:[REDACTED]@github.com",
		},
		{
			value: "fatal: user:actual-secret@short-host:443/acme/tool",
			want:  "fatal: [REDACTED]@short-host:443/acme/tool",
		},
		{
			value: "fatal: user:actual-secret@[2001:db8::1]:443/acme/tool",
			want:  "fatal: [REDACTED]@[2001:db8::1]:443/acme/tool",
		},
		{
			value: "fatal: https://example.test/#git:user:actual-secret@github.com/acme/tool",
			want:  "fatal: https://example.test/#git:[REDACTED]@github.com/acme/tool",
		},
		{
			value: "fatal: https://example.test/?git:user:actual-secret@github.com/acme/tool",
			want:  "fatal: https://example.test/?git:[REDACTED]@github.com/acme/tool",
		},
		{
			value: "fatal: user:actual-secret@short-host",
			want:  "fatal: [REDACTED]@short-host",
		},
		{
			value: "fatal: https://example.test/#user:actual-secret@short-host",
			want:  "fatal: https://example.test/#[REDACTED]@short-host",
		},
		{
			value: "fatal: [git:user:actual-secret@short-host]",
			want:  "fatal: [git:[REDACTED]@short-host]",
		},
		{
			value: "fatal: [git:user:actual-secret@short-host]: denied",
			want:  "fatal: [git:[REDACTED]@short-host]: denied",
		},
		{
			value: "fatal: npm:user:actual-secret@short-host",
			want:  "fatal: npm:[REDACTED]@short-host",
		},
		{
			value: "fatal: git:user:actual-secret@[2001:db8::1",
			want:  "fatal: git:[REDACTED]@[2001:db8::1",
		},
		{
			value: "fatal: [tag]git:user:actual-secret@short-host",
			want:  "fatal: [tag]git:[REDACTED]@short-host",
		},
	} {
		got, redacted := Redact(test.value, "[REDACTED]", nil)
		if got != test.want || !redacted {
			t.Errorf("Redact(%q) = %q/%t, want %q/true", test.value, got, redacted, test.want)
		}
	}
}

func TestInspectPasswordUserInfoHandlesIncompleteBracketInOneSegmentPass(t *testing.T) {
	value := "git:user:actual-secret@[" + strings.Repeat("aaaa:", 1<<12) + "/repo"
	if inspection := InspectPasswordUserInfo(value); inspection != UserInfoUninspectable {
		t.Fatalf("InspectPasswordUserInfo incomplete bracket = %v, want uninspectable", inspection)
	}
}

func TestInspectPasswordUserInfoPreservesSchemeLessGrammarAuthority(t *testing.T) {
	for _, value := range []string{
		"git:user:actual-secret@short-host:repo/path",
		"github:user:actual-secret@short-host:repo/path",
		"user:actual-secret@short+host",
		"[git:user:actual-secret@short-host]: denied",
		"[git:user:actual-secret@short-host].",
		"[tag]git:user:actual-secret@short-host",
		"[tag][git:user:actual-secret@short-host]",
		"GIT:user:actual-secret@short-host:repo/path",
		"github:user:actual-secret@[2001:db8::1]:repo/path",
		"[tag][git:user:actual-secret@short-host]: denied",
		"g%69t:user:actual-secret@short-host:repo/path",
	} {
		if inspection := InspectPasswordUserInfo(value); inspection != UserInfoPresent {
			t.Errorf("InspectPasswordUserInfo(%q) = %v, want present", value, inspection)
		}
	}

	if inspection := InspectPasswordUserInfo("git:user:actual-secret@[2001:db8::1"); inspection != UserInfoUninspectable {
		t.Fatalf("InspectPasswordUserInfo(incomplete authority) = %v, want uninspectable", inspection)
	}
	if inspection := InspectPasswordUserInfo("user:actual-secret@short-host:not-a-port/repo"); inspection != UserInfoUninspectable {
		t.Fatalf("InspectPasswordUserInfo(unsupported suffix) = %v, want uninspectable", inspection)
	}
	if inspection := InspectPasswordUserInfo("user:actual-secret@short!host"); inspection != UserInfoUninspectable {
		t.Fatalf("InspectPasswordUserInfo(unsupported host) = %v, want uninspectable", inspection)
	}
	if inspection := InspectPasswordUserInfo("git:user:actual-secret@short-host%FF"); inspection != UserInfoUninspectable {
		t.Fatalf("InspectPasswordUserInfo(invalid canonical form with credential) = %v, want uninspectable", inspection)
	}
	if inspection := InspectPasswordUserInfo("git:git@short+host:repo/path"); inspection != UserInfoAbsent {
		t.Fatalf("InspectPasswordUserInfo(credential-free scp) = %v, want absent", inspection)
	}
}

func TestRedactTreatsLogicalBearerAsOneCredentialField(t *testing.T) {
	for _, value := range []string{
		"token: Bea\u200brer actual-secret suffix",
		"Authorization: Bea\u2060rer actual-secret suffix",
		"token: Bea%E2%80%8Brer actual-secret suffix",
		"Authorization: Bearer actual\u200bsecret suffix",
		"Bea%09rer actual-secret leftover",
		"Bea%0Arer actual-secret leftover",
		"Bea%0Drer actual-secret leftover",
		"Bea\trer actual-secret leftover",
		"Bearer\tactual-secret leftover",
	} {
		got, redacted := Redact(value, "[REDACTED]", nil)
		if !redacted || strings.Contains(got, "actual-secret") || strings.Contains(got, "suffix") {
			t.Errorf("Redact(%q) = %q/%t, disclosed logical Bearer field", value, got, redacted)
		}
	}
}

func TestLineScopedValueEndScalesLinearlyWithQuotes(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(`Authorization: `)
	for index := 0; index < 4096; index++ {
		builder.WriteString(`"public" `)
	}
	builder.WriteString(`response="first` + "\r\n" + `actual-secret"`)
	got, redacted := Redact(builder.String(), "[REDACTED]", nil)
	if !redacted || strings.Contains(got, "actual-secret") {
		t.Fatalf("Redact quoted-heavy authorization = %q/%t, leaked secret", got, redacted)
	}
}

func TestRedactMapsEncodedMatchesBackToRawSpans(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "npm install --token%3Dactual-secret ok", want: "npm install --token%3D[REDACTED] ok"},
		{value: "npm:tool@%74oken=actual-secret", want: "npm:tool@%74oken=[REDACTED]"},
		{value: "git clone https://user:actual-secret@example.com/repo", want: "git clone https://[REDACTED]@example.com/repo"},
		{value: "git clone https://user:actual-secret%40example.com/repo", want: "git clone https://[REDACTED]%40example.com/repo"},
		{value: "progress%20report --verbose", want: "progress%20report --verbose"},
		{value: "ssh://git@example.com/repo", want: "ssh://git@example.com/repo"},
	}
	for _, test := range tests {
		got, redacted := Redact(test.value, "[REDACTED]", nil)
		wantRedacted := got != test.value
		if got != test.want || redacted != wantRedacted {
			t.Errorf("Redact(%q) = %q/%t, want %q/%t", test.value, got, redacted, test.want, wantRedacted)
		}
	}
}
