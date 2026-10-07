package host

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestPiNativePackagePatternCoordinatesAndPrecedence(t *testing.T) {
	for _, test := range []struct {
		resource string
		patterns []string
		delta    bool
		decision nativeResourceDecision
	}{
		{"index.ts", []string{"./index.ts"}, false, nativeResourceDisabled},
		{"index.ts", []string{"+./index.ts"}, false, nativeResourceEnabled},
		{"nested/index.ts", []string{"index.ts"}, false, nativeResourceEnabled},
		{"nested/index.ts", []string{"-index.ts"}, false, nativeResourceEnabled},
		{"nested/index.ts", []string{"-nested/index.ts"}, false, nativeResourceDisabled},
		{"nested/index.ts", []string{"*.ts"}, false, nativeResourceEnabled},
		{"nested/.hidden.ts", []string{"*.ts"}, false, nativeResourceDisabled},
		{"nested/.hidden.ts", []string{"nested/.*.ts"}, false, nativeResourceEnabled},
		{"index.ts", []string{"+other.ts"}, false, nativeResourceEnabled},
		{"index.ts", []string{"-index.ts", "+index.ts"}, false, nativeResourceDisabled},
		{"index.ts", []string{"-index.ts", "+index.ts"}, true, nativeResourceEnabled},
		{"index.ts", []string{"other.ts"}, true, nativeResourceUndecided},
	} {
		patterns := make([]nativeResourcePattern, 0, len(test.patterns))
		for _, directive := range test.patterns {
			pattern, err := newNativeResourcePattern(directive)
			if err != nil {
				t.Fatal(err)
			}
			patterns = append(patterns, pattern)
		}
		entry := piNativeAdapterPackage{filtered: true, delta: test.delta}
		if got := entry.resourceDecision(test.resource, patterns); got != test.decision {
			t.Fatalf("selection for %s/%v/delta=%t = %v, want %v", test.resource, test.patterns, test.delta, got, test.decision)
		}
	}
}

func TestPiNativePackageInventoryRefusals(t *testing.T) {
	for _, test := range []struct{ name, metadata string }{
		{"absent", ""},
		{"malformed", `{"name":`},
		{"duplicate", `{"name":"pi-mcp-adapter","name":"other","version":"2.15.0","pi":{"extensions":["index.ts"]}}`},
		{"foreign", `{"name":"other","version":"2.15.0","pi":{"extensions":["index.ts"]}}`},
		{"case sensitive manifest", `{"name":"pi-mcp-adapter","version":"2.15.0","Pi":{"extensions":["index.ts"]}}`},
		{"mismatched version", `{"name":"pi-mcp-adapter","version":"2.14.0","pi":{"extensions":["index.ts"]}}`},
		{"no explicit inventory", `{"name":"pi-mcp-adapter","version":"2.15.0"}`},
		{"empty inventory", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":[]}}`},
		{"glob inventory", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["*.ts"]}}`},
		{"override inventory", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts","-index.ts"]}}`},
		{"escaping inventory", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["../index.ts"]}}`},
		{"directory inventory", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["directory.ts"]}}`},
		{"symlink inventory", `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["link.ts"]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir, agentRoot := t.TempDir(), t.TempDir()
			packageRoot := filepath.Join(workDir, ".pi", "npm", "node_modules", "pi-mcp-adapter")
			if err := os.MkdirAll(filepath.Join(packageRoot, "directory.ts"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("directory.ts", filepath.Join(packageRoot, "link.ts")); err != nil {
				t.Fatal(err)
			}
			if test.metadata != "" {
				writeEffectiveConfig(t, filepath.Join(packageRoot, "package.json"), test.metadata)
			}
			entry := piNativeAdapterPackage{source: "npm:pi-mcp-adapter@2.15.0", scope: target.ScopeProject, filtered: true, patterns: []string{"-index.ts"}}
			if selected, err := nativeAdapterResourcesSelected([]piNativeAdapterPackage{entry}, workDir, agentRoot); err == nil || selected {
				t.Fatalf("unobserved package inventory qualified Native: selected=%t, err=%v", selected, err)
			}
		})
	}
}

func TestPiNativeProjectPackageDeltaUsesUserInstallationAndSelector(t *testing.T) {
	workDir, agentRoot := t.TempDir(), t.TempDir()
	writeEffectiveConfig(t, filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter", "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["index.ts"]}}`)
	writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "npm", "node_modules", "pi-mcp-adapter", "package.json"), `{"name":"pi-mcp-adapter","version":"2.13.0","pi":{"extensions":["other.ts"]}}`)
	global := []piNativeAdapterPackage{{source: "npm:pi-mcp-adapter@^2.13.0", scope: target.ScopeGlobal}}
	project := []piNativeAdapterPackage{{source: "npm:pi-mcp-adapter@2.13.0", scope: target.ScopeProject, filtered: true, delta: true, patterns: []string{"-index.ts"}}}
	packages := selectNativeAdapterPackages(global, project)
	if selected, err := nativeAdapterResourcesSelected(packages, workDir, agentRoot); err != nil || selected {
		t.Fatalf("project delta used its own source/installation instead of the user base: selected=%t, err=%v", selected, err)
	}
}

func TestPiNativeEmptyProjectDeltaPreservesFirstUserResourceDecision(t *testing.T) {
	global := []piNativeAdapterPackage{
		{source: "npm:pi-mcp-adapter@2.15.0", scope: target.ScopeGlobal, filtered: true},
		{source: "npm:pi-mcp-adapter@2.15.0", scope: target.ScopeGlobal},
	}
	project := []piNativeAdapterPackage{{source: "npm:pi-mcp-adapter@2.13.0", scope: target.ScopeProject, delta: true}}
	if selected, err := nativeAdapterResourcesSelected(selectNativeAdapterPackages(global, project), t.TempDir(), t.TempDir()); err != nil || selected {
		t.Fatalf("empty delta discarded the first disabled user resource decision: selected=%t, err=%v", selected, err)
	}
}

func TestPiNativePackageMatchingDoesNotGuessMinimatchAliasesOrUnicode(t *testing.T) {
	for _, directive := range []string{"nested//index.ts", "!#index.ts", "**/*.ts", "/pkg/index.ts"} {
		if _, err := newNativeResourcePattern(directive); err == nil {
			t.Fatalf("unmodelled selector was guessed: %q", directive)
		}
	}
	workDir, agentRoot := t.TempDir(), t.TempDir()
	writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "npm", "node_modules", "pi-mcp-adapter", "package.json"), `{"name":"pi-mcp-adapter","version":"2.15.0","pi":{"extensions":["😀.ts"]}}`)
	entry := piNativeAdapterPackage{source: "npm:pi-mcp-adapter@2.15.0", scope: target.ScopeProject, filtered: true, patterns: []string{"!?.ts"}}
	if selected, err := nativeAdapterResourcesSelected([]piNativeAdapterPackage{entry}, workDir, agentRoot); err == nil || selected {
		t.Fatalf("Go rune matching was mistaken for upstream UTF-16 glob matching: selected=%t, err=%v", selected, err)
	}
	entry.patterns = []string{"-😀.ts"}
	if selected, err := nativeAdapterResourcesSelected([]piNativeAdapterPackage{entry}, workDir, agentRoot); err != nil || selected {
		t.Fatalf("exact Unicode exclusion did not retain literal semantics: selected=%t, err=%v", selected, err)
	}
}
