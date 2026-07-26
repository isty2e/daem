package archguard

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestManagedPathKernelCannotRegainFamilyOrHostBranches(t *testing.T) {
	root := findRepoRoot(t)
	files := managedPathKernelFiles(t, root)
	forbiddenImportFragments := []string{
		"/internal/desired/skill",
		"/internal/desired/instructions",
		"/internal/desired/hook",
		"/internal/desired/hookasset",
		"/internal/resource/skill",
		"/internal/resource/instructions",
		"/internal/resource/hook",
		"/internal/topology/hook",
		"/internal/effect/payload/skill",
		"/internal/effect/payload/instructions",
		"/internal/effect/payload/hookasset",
		"/internal/output/project",
	}
	forbiddenIdentifiers := map[string]struct{}{
		"Skill": {}, "Instructions": {}, "Hook": {}, "HookAsset": {},
		"KindSkill": {}, "KindInstructions": {}, "KindHook": {}, "KindHookAsset": {},
		"ResourceKindSkill": {}, "ResourceKindInstructions": {},
		"ResourceKindHook": {}, "ResourceKindHookAsset": {},
		"TargetAntigravityCLI": {}, "TargetClaudeCode": {}, "TargetCodex": {},
		"TargetOpenCode": {}, "TargetPi": {},
	}
	forbiddenStringLiterals := map[string]struct{}{
		"skill": {}, "instructions": {}, "hook": {}, "hook_asset": {},
		"antigravity-cli": {}, "claude-code": {}, "codex": {}, "opencode": {}, "pi": {},
	}

	for _, relative := range files {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(relative))
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse shared managed-path source: %v", err)
			}
			for _, spec := range parsed.Imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("decode import %s: %v", spec.Path.Value, err)
				}
				for _, forbidden := range forbiddenImportFragments {
					if strings.Contains(importPath, forbidden) {
						t.Errorf("shared managed-path source imports family adapter %q", importPath)
					}
				}
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.Ident:
					if _, forbidden := forbiddenIdentifiers[value.Name]; forbidden {
						t.Errorf("shared managed-path source uses family or host identifier %q", value.Name)
					}
				case *ast.BasicLit:
					if value.Kind != token.STRING {
						return true
					}
					literal, err := strconv.Unquote(value.Value)
					if err != nil {
						t.Errorf("decode string literal %s: %v", value.Value, err)
						return true
					}
					if _, forbidden := forbiddenStringLiterals[literal]; forbidden {
						t.Errorf("shared managed-path source uses family or host literal %q", literal)
					}
				}
				return true
			})
		})
	}
}

func TestManagedPathPayloadAdaptersCannotRegainParallelAuthoredIdentity(t *testing.T) {
	root := findRepoRoot(t)
	for _, relative := range []string{
		"internal/effect/payload/build/skill.go",
		"internal/effect/payload/build/instructions.go",
		"internal/effect/payload/build/hook_asset.go",
	} {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(relative))
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse managed-path payload adapter: %v", err)
			}
			for _, spec := range parsed.Imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatalf("decode import %s: %v", spec.Path.Value, err)
				}
				if strings.HasSuffix(importPath, "/internal/resource") {
					t.Errorf("managed-path payload adapter imports retired resource identity %q", importPath)
				}
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				field, ok := node.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				identifier, ok := field.Key.(*ast.Ident)
				if ok && (identifier.Name == "ResourceID" || identifier.Name == "EntityID") {
					t.Errorf("managed-path payload adapter emits parallel authored identity field %q", identifier.Name)
				}
				return true
			})
		})
	}
}

func TestRetiredManagedPathPayloadFamilyPackagesStayAbsent(t *testing.T) {
	root := findRepoRoot(t)
	for _, relative := range []string{
		"internal/effect/payload/skill",
		"internal/effect/payload/instructions",
		"internal/effect/payload/hookasset",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("retired managed-path payload family package %q exists or cannot be inspected: %v", relative, err)
		}
	}
}

func TestSharedLockedFileMaterializationStaysFamilyAndEffectNeutral(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, "internal", "effect", "payload", "build", "exact_file.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse shared locked-file materializer: %v", err)
	}
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("decode import %s: %v", spec.Path.Value, err)
		}
		for _, forbidden := range []string{
			"/internal/compat/",
			"/internal/desired/",
			"/internal/effect/execute",
			"/internal/effect/mutation",
			"/internal/output",
			"/internal/realization/profile",
			"/internal/target",
			"/internal/workflow",
		} {
			if strings.Contains(importPath, forbidden) {
				t.Errorf("shared locked-file materializer imports family, placement, or Effect authority %q", importPath)
			}
		}
	}
}

func managedPathKernelFiles(t *testing.T, root string) []string {
	t.Helper()
	patterns := []string{
		"internal/realization/lock/managed_path*.go",
		"internal/assurance/observe/managed_path*.go",
		"internal/assurance/observe/live/managed_path*.go",
		"internal/reconcile/managed_path*.go",
		"internal/effect/execute/managed_path*.go",
		"internal/effect/journal/managed_path*.go",
		"internal/workflow/readiness/managed_path*.go",
		"internal/workflow/apply/managed_path*.go",
	}
	files := map[string]struct{}{
		"internal/effect/journal/path_mutation.go": {},
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatalf("glob shared managed-path sources %q: %v", pattern, err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatalf("relativize managed-path source %q: %v", path, err)
			}
			files[filepath.ToSlash(relative)] = struct{}{}
		}
	}
	result := make([]string, 0, len(files))
	for path := range files {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}
