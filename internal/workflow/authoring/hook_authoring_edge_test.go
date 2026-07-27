package authoring

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestHookAuthoringEdgeHuntPreservesOverrideDiagnosticOrder(t *testing.T) {
	request := AddHookRequest{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Command: "python3 hooks/protect.py",
		Targets: []string{"opencode", "pi"},
		TargetOverrides: []declaration.HookTargetOverride{
			{Target: "pi", Matcher: "Write"},
			{Target: "opencode", Matcher: "Bash"},
		},
	}

	for attempt := range 128 {
		_, _, err := HookFromAddRequest(request, declaration.ManifestHeader{})
		if err == nil {
			t.Fatal("HookFromAddRequest returned nil error")
		}
		if !strings.Contains(err.Error(), `target "pi"`) {
			t.Fatalf("attempt %d error = %q, want first authored override target pi", attempt, err)
		}
	}
}

func TestHookAuthoringEdgeHuntRoundOnePreservesBoundaryDiagnostics(t *testing.T) {
	_, _, err := HookFromAddRequest(AddHookRequest{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Command: "python3 hooks/protect.py",
		Targets: []string{"codex"},
		TargetOverrides: []declaration.HookTargetOverride{{
			Target:  "claude-code",
			Matcher: "Write",
		}},
	}, declaration.ManifestHeader{})
	const undeclaredOverride = `hook "protect-env" target_override target "claude-code" is not declared for hook`
	if err == nil || err.Error() != undeclaredOverride {
		t.Fatalf("undeclared override error = %v, want %q", err, undeclaredOverride)
	}

	tests := []struct {
		name   string
		mutate func(*AddHookRequest)
		want   string
	}{
		{
			name: "duplicate targets remain prospective manifest validation",
			mutate: func(request *AddHookRequest) {
				request.Targets = []string{"codex", "codex"}
			},
			want: "duplicate target",
		},
		{
			name: "duplicate overrides remain prospective manifest validation",
			mutate: func(request *AddHookRequest) {
				request.TargetOverrides = []declaration.HookTargetOverride{
					{Target: "codex", Matcher: "Write"},
					{Target: "codex", Matcher: "Bash"},
				}
			},
			want: "duplicate override",
		},
		{
			name: "negative timeout remains prospective manifest validation",
			mutate: func(request *AddHookRequest) {
				request.TimeoutSeconds = -1
			},
			want: "timeout must not be negative",
		},
		{
			name: "invalid scope remains prospective manifest validation",
			mutate: func(request *AddHookRequest) {
				request.Scope = "workspace"
			},
			want: `unknown scope "workspace"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := AddHookRequest{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Command: "python3 hooks/protect.py",
				Targets: []string{"codex"},
			}
			test.mutate(&request)
			_, err := BuildAddHookChange(ManifestDocument{
				Content: []byte("version = 1\ntargets = [\"codex\"]\n"),
			}, request)
			if err == nil || !strings.HasPrefix(err.Error(), "resulting manifest is invalid: ") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildAddHookChange error = %v, want prospective manifest error containing %q", err, test.want)
			}
		})
	}
}

func TestHookAuthoringEdgeHuntRoundTwoPreservesHostDisposition(t *testing.T) {
	tests := []struct {
		target      string
		wantWarning string
		wantError   string
	}{
		{target: "codex"},
		{target: "claude-code"},
		{target: "opencode", wantWarning: `target "opencode"`},
		{target: "pi", wantWarning: `target "pi"`},
		{target: "antigravity-cli", wantError: "no direct command-hook route is admitted"},
	}

	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			_, warnings, err := HookFromAddRequest(AddHookRequest{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Command: "python3 hooks/protect.py",
				Targets: []string{test.target},
			}, declaration.ManifestHeader{})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || len(warnings) != 0 {
					t.Fatalf("admission = (%#v, %v), want error containing %q", warnings, err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("HookFromAddRequest returned error: %v", err)
			}
			if test.wantWarning == "" {
				if len(warnings) != 0 {
					t.Fatalf("warnings = %#v, want none", warnings)
				}
				return
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], test.wantWarning) {
				t.Fatalf("warnings = %#v, want one warning containing %q", warnings, test.wantWarning)
			}
		})
	}

	_, warnings, err := HookFromAddRequest(AddHookRequest{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Command: "python3 hooks/protect.py",
		Targets: []string{"pi", "codex", "opencode"},
	}, declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("mixed target admission returned error: %v", err)
	}
	if len(warnings) != 2 ||
		!strings.Contains(warnings[0], `target "pi"`) ||
		!strings.Contains(warnings[1], `target "opencode"`) {
		t.Fatalf("mixed target warnings = %#v, want authored bridge-target order", warnings)
	}
}

func TestHookAuthoringEdgeHuntRoundThreeDefersCanonicalTextValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AddHookRequest)
		want   string
	}{
		{
			name: "invalid UTF-8 status",
			mutate: func(request *AddHookRequest) {
				request.StatusMessage = string([]byte{'b', 'a', 'd', 0xff})
			},
			want: "invalid escape in string",
		},
		{
			name: "bidirectional command",
			mutate: func(request *AddHookRequest) {
				request.Command = "python3 safe\u202etxt.py"
			},
			want: "command must not contain bidirectional control characters",
		},
		{
			name: "bidirectional override matcher",
			mutate: func(request *AddHookRequest) {
				request.TargetOverrides = []declaration.HookTargetOverride{{
					Target:  "codex",
					Matcher: "safe\u202etxt",
				}}
			},
			want: "must not contain bidirectional control characters",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := AddHookRequest{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Command: "python3 hooks/protect.py",
				Targets: []string{"codex"},
			}
			test.mutate(&request)
			_, err := BuildAddHookChange(ManifestDocument{
				Content: []byte("version = 1\ntargets = [\"codex\"]\n"),
			}, request)
			if err == nil || !strings.HasPrefix(err.Error(), "resulting manifest is invalid: ") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildAddHookChange error = %v, want canonical error containing %q", err, test.want)
			}
		})
	}

	_, warnings, err := HookFromAddRequest(AddHookRequest{
		Name:    "protect-env",
		Event:   "PreToolUse",
		Command: "python3 hooks/protect.py",
	}, declaration.ManifestHeader{Targets: []string{"pi", "opencode"}})
	if err != nil {
		t.Fatalf("inherited bridge targets returned error: %v", err)
	}
	if len(warnings) != 2 ||
		!strings.Contains(warnings[0], `target "pi"`) ||
		!strings.Contains(warnings[1], `target "opencode"`) {
		t.Fatalf("inherited target warnings = %#v, want manifest target order", warnings)
	}
}
