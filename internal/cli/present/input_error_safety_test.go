package clipresent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/supply/source"
)

func TestErrorRedactsInputCredentials(t *testing.T) {
	for _, input := range []struct {
		name string
		uri  string
	}{
		{name: "userinfo", uri: "s3://review-user:review-secret@bucket/key"},
		{name: "encoded userinfo", uri: "s3://review-user:review%2Dsecret@bucket/key"},
		{name: "query", uri: "s3://bucket/key?token=review-secret"},
		{name: "malformed path", uri: "s3://review-user:review-secret@bucket/%zz"},
		{name: "long inspectable userinfo", uri: "s3://review-user:review-secret" + strings.Repeat("x", 6<<10) + "@bucket/key"},
		{name: "oversized userinfo", uri: "s3://review-user:review-secret" + strings.Repeat("x", 32<<10) + "@bucket/key"},
	} {
		t.Run(input.name, func(t *testing.T) {
			_, cause := source.ParseS3ObjectURI(input.uri)
			if cause == nil {
				t.Fatal("credential fixture unexpectedly accepted")
			}
			for _, err := range []error{cause, fmt.Errorf("invalid manifest: %w", cause)} {
				got := Error(err)
				for _, secret := range []string{"review-user", "review-secret", "review%2Dsecret"} {
					if strings.Contains(got, secret) {
						t.Error("human error exposed a credential-shaped input fragment")
					}
				}
				if !strings.Contains(got, "[REDACTED]") {
					t.Error("human error did not disclose redaction")
				}
			}
		})
	}
}

func TestErrorBoundsInputDetailAndKeepsSyntaxGuidance(t *testing.T) {
	const detailBudget = 4096
	large := strings.Repeat("x", 32<<10)
	plain := Error(errors.New("mcp_server[0].transport: unsupported value " + strings.Repeat("x", 2*detailBudget)))
	if got := utf8.RuneCountInString(plain); got > detailBudget+len(errorEvidenceTruncationMarker) {
		t.Errorf("untyped error has %d runes, want a bounded detail", got)
	}
	if !strings.HasPrefix(plain, "mcp_server[0].transport:") || !strings.HasSuffix(plain, errorEvidenceTruncationMarker) {
		t.Error("bounded error lost its field context or truncation indication")
	}

	positioned := tomlstrict.Admit(context.Background(), []byte("broken=[\n"), tomlstrict.StandardLimits())
	line, column, located := tomlstrict.ErrorPosition(positioned)
	if !located {
		t.Fatal("fixture did not produce a positioned syntax error")
	}
	_, guidance, found := strings.Cut(Error(positioned), "\nnext:")
	if !found {
		t.Fatal("reference syntax error has no repair guidance")
	}
	got := Error(fmt.Errorf("%s: %w", large, positioned))
	if !strings.Contains(got, fmt.Sprintf("(line %d, column %d)", line, column)) || !strings.HasSuffix(got, "\nnext:"+guidance) {
		t.Error("bounding a cause lost its owned position or repair guidance")
	}
	if count := utf8.RuneCountInString(got); count > detailBudget+len(errorEvidenceTruncationMarker)+len(guidance)+96 {
		t.Errorf("positioned diagnostic has %d runes, want bounded detail plus metadata", count)
	}
}

func TestBoundedErrorEvidenceWithholdsIncompleteCredentialText(t *testing.T) {
	const maximumRunes = 4096
	uri := "s3://review-user:review-secret" + strings.Repeat("x", 32<<10) + "@bucket/key"
	_, cause := source.ParseS3ObjectURI(uri)
	_, joinedCause := source.ParseS3ObjectURI("s3://review-user:review-secret" + strings.Repeat("x", 15<<10) + "@bucket/key")
	if cause == nil || joinedCause == nil {
		t.Fatal("credential fixtures unexpectedly accepted")
	}
	typedCalls := 0
	for _, input := range []struct {
		name  string
		cause error
	}{
		{name: "static text", cause: cause},
		{name: "typed partial text", cause: terminalWrappedEvidenceError{message: uri, calls: &typedCalls}},
		{name: "aggregate boundary", cause: errors.Join(errors.New(strings.Repeat("x", 2<<10)), joinedCause)},
	} {
		t.Run(input.name, func(t *testing.T) {
			got := BoundedErrorEvidence(input.cause, maximumRunes)
			for _, secret := range []string{"review-user", "review-secret"} {
				if strings.Contains(got, secret) {
					t.Error("incomplete evidence disclosed a credential-shaped fragment")
				}
			}
			if !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, "[truncated]") {
				t.Error("incomplete evidence did not indicate withheld text")
			}
			if utf8.RuneCountInString(got) > maximumRunes+len(errorEvidenceTruncationMarker) {
				t.Error("withheld evidence exceeded its display budget")
			}
		})
	}
	if typedCalls != 0 {
		t.Error("typed evidence invoked an arbitrary Error formatter")
	}

	opaqueCalls := 0
	const complete = "complete safe input context"
	got := BoundedErrorEvidence(errors.Join(errors.New(complete), opaqueEvidenceError{calls: &opaqueCalls}), maximumRunes)
	if opaqueCalls != 0 || !strings.Contains(got, complete) || !strings.Contains(got, "[truncated]") {
		t.Error("omitting an opaque node lost a complete safe observation or invoked its formatter")
	}
}
