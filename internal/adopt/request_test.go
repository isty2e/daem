package adopt

import (
	"path/filepath"
	"testing"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestRequestOwnsSelectionsAndDefensivelyDisclosesThem(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	targets := []targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetClaudeCode}
	scopes := []targetpkg.Scope{targetpkg.ScopeProject, targetpkg.ScopeGlobal}
	request, err := NewRequest(targets, scopes, output, sourceDirectory, true)
	if err != nil {
		t.Fatal(err)
	}

	targets[0] = targetpkg.TargetOpenCode
	scopes[0] = targetpkg.ScopeGlobal
	if got := request.Targets(); got[0] != targetpkg.TargetCodex {
		t.Fatalf("request target changed through constructor input: %v", got)
	}
	if got := request.Scopes(); got[0] != targetpkg.ScopeProject {
		t.Fatalf("request scope changed through constructor input: %v", got)
	}

	disclosedTargets := request.Targets()
	disclosedScopes := request.Scopes()
	disclosedTargets[0] = targetpkg.TargetOpenCode
	disclosedScopes[0] = targetpkg.ScopeGlobal
	if got := request.Targets(); got[0] != targetpkg.TargetCodex {
		t.Fatalf("request target changed through accessor result: %v", got)
	}
	if got := request.Scopes(); got[0] != targetpkg.ScopeProject {
		t.Fatalf("request scope changed through accessor result: %v", got)
	}
	if request.Output() != output || request.SourceDirectory().Root() != filepath.Join(root, "daem.d") || !request.Merge() {
		t.Fatalf("request scalar disclosure is inconsistent")
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("validated request became invalid: %v", err)
	}
}

func TestRequestRejectsPartialAndDuplicateSelections(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		targets []targetpkg.Target
		scopes  []targetpkg.Scope
	}{
		{name: "missing targets", scopes: []targetpkg.Scope{targetpkg.ScopeProject}},
		{name: "missing scopes", targets: []targetpkg.Target{targetpkg.TargetCodex}},
		{
			name:    "duplicate target",
			targets: []targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetCodex},
			scopes:  []targetpkg.Scope{targetpkg.ScopeProject},
		},
		{
			name:    "duplicate scope",
			targets: []targetpkg.Target{targetpkg.TargetCodex},
			scopes:  []targetpkg.Scope{targetpkg.ScopeProject, targetpkg.ScopeProject},
		},
		{
			name:    "unsupported target",
			targets: []targetpkg.Target{"unknown"},
			scopes:  []targetpkg.Scope{targetpkg.ScopeProject},
		},
		{
			name:    "unsupported scope",
			targets: []targetpkg.Target{targetpkg.TargetCodex},
			scopes:  []targetpkg.Scope{"unknown"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRequest(test.targets, test.scopes, output, sourceDirectory, false); err == nil {
				t.Fatal("NewRequest succeeded, want error")
			}
		})
	}
	if err := (Request{}).Validate(); err == nil {
		t.Fatal("zero Request validated")
	}
}

func TestSourceDirectoryRejectsEscapesAndReservedStateRoots(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")

	tests := []struct {
		name      string
		output    string
		sourceDir string
	}{
		{name: "relative output", output: "daem.toml", sourceDir: filepath.Join(root, "daem.d")},
		{name: "relative source", output: output, sourceDir: "daem.d"},
		{name: "outside output directory", output: output, sourceDir: filepath.Join(filepath.Dir(root), "outside")},
		{name: "reserved state root", output: output, sourceDir: filepath.Join(root, ".daem", "imported")},
		{name: "output inside source", output: filepath.Join(root, "generated", "daem.toml"), sourceDir: filepath.Join(root, "generated")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSourceDirectory(test.output, test.sourceDir); err == nil {
				t.Fatal("NewSourceDirectory succeeded, want error")
			}
		})
	}

	sourceDirectory, err := NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	for _, relativePath := range []string{"", ".", "..", "../escape", "/absolute"} {
		t.Run(relativePath, func(t *testing.T) {
			if _, err := sourceDirectory.Resolve(relativePath); err == nil {
				t.Fatalf("Resolve(%q) succeeded, want error", relativePath)
			}
		})
	}
	resolved, err := sourceDirectory.Resolve("instructions/codex.md")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Join(root, "daem.d", "instructions", "codex.md") {
		t.Fatalf("resolved path = %q", resolved)
	}
}
