package manifest

import (
	"strings"
	"testing"
)

func TestParseRejectsUnknownKeys(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]
unexpected = true
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "unknown manifest key") {
		t.Fatalf("error = %q, want unknown manifest key", err)
	}
}

func TestParseRejectsUnknownDefaultsKey(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[defaults]
unsupported = true
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "unknown manifest key") {
		t.Fatalf("error = %q, want unknown manifest key", err)
	}
}

func TestParseRejectsFutureSourceSelectionTablesUntilImplemented(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "skill_set",
			manifest: `
version = 1
targets = ["codex"]

[[skill_set]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
`,
			want: "skill_set",
		},
		{
			name: "source",
			manifest: `
version = 1
targets = ["codex"]

[[source]]
name = "local-skills"
path = "skills"
mode = "vendor"
`,
			want: "source",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.manifest))
			if err == nil {
				t.Fatal("Bytes returned nil error")
			}
			if !strings.Contains(err.Error(), "unknown manifest key") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want unknown manifest key for %s", err, test.want)
			}
		})
	}
}

func TestParseRejectsHookSource(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{
			name: "populated source",
			manifest: `
version = 1
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "python3 hooks/protect.py"
source = { path = "hooks/protect.py", mode = "vendor" }
`,
		},
		{
			name: "empty source table",
			manifest: `
version = 1
targets = ["pi"]

[[hook]]
name = "notify"
event = "pre_apply"
command = "python3 hooks/notify.py"
source = {}
targets = ["pi"]
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.manifest))
			if err == nil {
				t.Fatal("Bytes returned nil error")
			}

			if !strings.Contains(err.Error(), "unknown manifest key \"hook.source\"") {
				t.Fatalf("error = %q, want strict unknown hook source diagnostic", err)
			}
		})
	}
}

func TestParseRejectsPortableProjectLocalLink(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "experimental-review"
source = { path = "../skills/experimental-review", mode = "link" }
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "portable = false") {
		t.Fatalf("error = %q, want portable false diagnostic", err)
	}
}

func TestParseRejectsHookOverrideForUndeclaredTarget(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[hook]]
name = "bd-prime-session"
event = "SessionStart"
command = "bd prime"

[[hook.target_override]]
target = "claude-code"
matcher = "startup"
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "is not declared for hook") {
		t.Fatalf("error = %q, want undeclared target diagnostic", err)
	}
}
