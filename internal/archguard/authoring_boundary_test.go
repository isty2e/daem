package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

var forbiddenAuthoringDeclarationNames = []string{
	"Skill",
	"SkillGroup",
	"Hook",
	"HookTargetOverride",
	"Instruction",
	"InstructionSource",
	"InstructionRendering",
	"MCPServer",
	"MCPServerEnvReference",
	"Extension",
	"ExtensionSource",
}

var forbiddenAuthoringDeclarationFingerprints = []struct {
	family string
	fields []string
}{
	{family: "skill", fields: []string{"ID", "Name", "Source", "Targets", "Scope", "InstallMode", "Portable", "CompatRepair"}},
	{family: "skill group", fields: []string{"Names", "Include", "Exclude", "Source", "Targets", "Scope", "InstallMode", "Portable", "CompatRepair"}},
	{family: "hook", fields: []string{"Name", "Event", "Matcher", "Type", "Command", "TimeoutSeconds", "StatusMessage", "Targets", "Scope", "TargetOverrides"}},
	{family: "hook target override", fields: []string{"Target", "Condition", "Matcher"}},
	{family: "instruction", fields: []string{"Source", "Targets", "Scope", "Target"}},
	{family: "instruction source", fields: []string{"Path", "Mode", "Git", "Ref", "S3", "VersionID", "Region", "Format"}},
	{family: "instruction rendering", fields: []string{"RenderTo", "Mode"}},
	{family: "MCP server", fields: []string{"Name", "Targets", "Scope", "Transport", "Command", "Args", "Env"}},
	{family: "extension", fields: []string{"ID", "Carrier", "Targets", "Scope", "Source", "OnAbsent"}},
	{family: "extension source", fields: []string{"Marketplace", "HostSource"}},
}

func TestAuthoringBoundaryDoesNotReintroduceDeclarationModels(t *testing.T) {
	for _, record := range loadRepoPackageRecords(t) {
		internalPackage, internal := internalPath(record.ImportPath)
		if !internal || internalPackage != "internal/workflow/authoring" {
			continue
		}
		for _, fileName := range record.GoFiles {
			if fileName == "codec_conversion.go" {
				t.Errorf("workflow authoring reintroduced forbidden conversion file %q", fileName)
			}
			content, ok := packageFileContent(record, fileName)
			if !ok {
				t.Fatalf("read workflow authoring file %q", fileName)
			}
			file, err := parser.ParseFile(token.NewFileSet(), fileName, content, 0)
			if err != nil {
				t.Fatalf("parse workflow authoring file %q: %v", fileName, err)
			}
			for _, declaration := range file.Decls {
				generic, ok := declaration.(*ast.GenDecl)
				if !ok || generic.Tok != token.TYPE {
					continue
				}
				for _, spec := range generic.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if reason := forbiddenAuthoringDeclarationModel(typeSpec); reason != "" {
						t.Errorf("workflow authoring owns forbidden declaration model %q in %s: %s", typeSpec.Name.Name, fileName, reason)
					}
				}
			}
		}
	}
}

func TestAuthoringBoundaryGuardRejectsRenamedAndAliasedDeclarationModels(t *testing.T) {
	source := `package authoring

import "example/internal/declaration"

type AuthoredHook struct {
	Name string
	Event string
	Matcher string
	Type string
	Command string
	TimeoutSeconds int
	StatusMessage string
	Targets []string
	Scope string
	TargetOverrides []string
}

type HookCopy = declaration.Hook
`
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	var reasons []string
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || generic.Tok != token.TYPE {
			continue
		}
		for _, spec := range generic.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			reasons = append(reasons, forbiddenAuthoringDeclarationModel(typeSpec))
		}
	}
	if len(reasons) != 2 ||
		!slices.Contains(reasons, "duplicates hook declaration fields") ||
		!slices.Contains(reasons, "aliases or wraps declaration model Hook") {
		t.Fatalf("guard reasons = %#v, want renamed struct and alias rejection", reasons)
	}
}

func forbiddenAuthoringDeclarationModel(typeSpec *ast.TypeSpec) string {
	if slices.Contains(forbiddenAuthoringDeclarationNames, typeSpec.Name.Name) {
		return "uses a reserved declaration model name"
	}
	if selector, ok := typeSpec.Type.(*ast.SelectorExpr); ok && slices.Contains(forbiddenAuthoringDeclarationNames, selector.Sel.Name) {
		return "aliases or wraps declaration model " + selector.Sel.Name
	}
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return ""
	}
	fields := make(map[string]struct{})
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			fields[name.Name] = struct{}{}
		}
	}
	for _, fingerprint := range forbiddenAuthoringDeclarationFingerprints {
		matches := true
		for _, name := range fingerprint.fields {
			if _, exists := fields[name]; !exists {
				matches = false
				break
			}
		}
		if matches {
			return "duplicates " + fingerprint.family + " declaration fields"
		}
	}
	return ""
}
