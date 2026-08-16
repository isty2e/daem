package extension

import (
	"strings"
	"testing"
	"time"
)

func TestValidateNestedURLCredentialsClassifiesEachEmbeddedURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "credential userinfo", value: "git:https://user:secret@example.com/acme/tool.git", wantErr: "inline credentials"},
		{name: "http transport bare user", value: "git:https://user@example.com/acme/tool.git", wantErr: "inline credentials"},
		{name: "malformed authority with userinfo", value: "git:https://user:secret@[example.com/acme/tool.git", wantErr: "malformed URL authority"},
		{name: "malformed authority with bracketed host", value: "git:https://[example.com/acme/tool.git", wantErr: "malformed URL authority"},
		{name: "ssh transport user stays admissible", value: "ssh://git@github.com/acme/tools.git"},
		{name: "second embedded URL carries credential", value: "git:https://github.com/acme/tools git+https://user:pw@other.example/tool", wantErr: "inline credentials"},
		{name: "file URL has no authority", value: "git:file:///Users/alice/private"},
		{name: "malformed URL without authority shape", value: "git:x://%zz"},
		{name: "no embedded URL", value: "github:acme/tools"},
	}
	for _, test := range tests {
		err := validateNestedURLCredentials(test.value)
		if test.wantErr == "" {
			if err != nil {
				t.Errorf("%s: validateNestedURLCredentials(%q) = %v, want nil", test.name, test.value, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), test.wantErr) {
			t.Errorf("%s: validateNestedURLCredentials(%q) = %v, want containing %q", test.name, test.value, err, test.wantErr)
		}
	}
}

func TestValidateNestedURLCredentialsScalesLinearlyWithMarkers(t *testing.T) {
	// The previous scanner re-parsed the whole remaining suffix at every
	// protocol marker, so validation time grew quadratically with marker
	// count. One large source could then amplify validation and lock CPU
	// use; this probe keeps the scan on its linear budget.
	value := strings.Repeat("https://example.com/acme/tool ", 16384)
	start := time.Now()
	err := validateNestedURLCredentials(value)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("validateNestedURLCredentials returned error: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("validateNestedURLCredentials took %v for %d markers, want linear scan", elapsed, 16384)
	}
}
