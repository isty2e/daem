package declaration_test

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type editorUseStep struct {
	kind byte
	name string
}

type editorUseAlternative struct {
	path   []editorUseStep
	origin string
}

type editorRequiredUse struct {
	path         []editorUseStep
	property     string
	origin       string
	alternatives []editorUseAlternative
}

func editorUseSite(path []editorUseStep) string {
	var result strings.Builder
	result.WriteByte('$')
	for _, step := range path {
		switch step.kind {
		case 'p':
			result.WriteByte('.')
			result.WriteString(step.name)
		case 'a':
			result.WriteString("[*]")
		case 'm':
			result.WriteString(".*")
		}
	}
	return result.String()
}

func editorPointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func editorResolveSchemaRef(root map[string]any, reference string) (map[string]any, error) {
	if !strings.HasPrefix(reference, "#/") {
		return nil, fmt.Errorf("nonlocal or unsupported schema reference %q", reference)
	}
	pointer, err := url.PathUnescape(reference[1:])
	if err != nil {
		return nil, err
	}
	var current any = root
	for _, encoded := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		switch container := current.(type) {
		case map[string]any:
			var exists bool
			current, exists = container[token]
			if !exists {
				return nil, fmt.Errorf("unresolved schema reference %q", reference)
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(container) {
				return nil, fmt.Errorf("invalid schema array reference %q", reference)
			}
			current = container[index]
		default:
			return nil, fmt.Errorf("unsupported schema reference shape %q", reference)
		}
	}
	node, ok := current.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema reference %q is not an object", reference)
	}
	return node, nil
}

func editorRequiredUses(root map[string]any) ([]editorRequiredUse, error) {
	var result []editorRequiredUse
	active := make(map[string]bool)
	var walk func(map[string]any, []editorUseStep, string, []editorUseAlternative) error
	walk = func(node map[string]any, path []editorUseStep, origin string, alternatives []editorUseAlternative) error {
		if raw, exists := node["$ref"]; exists {
			for keyword := range node {
				if keyword != "$ref" && !(origin == "#" && (keyword == "$schema" || keyword == "definitions")) {
					return fmt.Errorf("ignored reference sibling %s at %s", keyword, origin)
				}
			}
			reference, ok := raw.(string)
			if !ok || active[reference] {
				return fmt.Errorf("invalid or cyclic reference at %s", editorUseSite(path))
			}
			target, err := editorResolveSchemaRef(root, reference)
			if err != nil {
				return err
			}
			// Only the active stack detects cycles; another input path must expand this ref again.
			active[reference] = true
			err = walk(target, path, reference, alternatives)
			delete(active, reference)
			return err
		}

		for keyword, raw := range node {
			switch keyword {
			case "required":
				fields, ok := raw.([]any)
				if !ok {
					return fmt.Errorf("invalid required rule at %s", origin)
				}
				for _, field := range fields {
					name, ok := field.(string)
					if !ok {
						return fmt.Errorf("non-string required property at %s", origin)
					}
					result = append(result, editorRequiredUse{
						path: slices.Clone(path), property: name, origin: origin,
						alternatives: slices.Clone(alternatives),
					})
				}
			case "properties":
				properties, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid properties at %s", origin)
				}
				for name, child := range properties {
					object, ok := child.(map[string]any)
					if !ok {
						return fmt.Errorf("invalid property schema at %s", origin)
					}
					next := append(slices.Clone(path), editorUseStep{kind: 'p', name: name})
					if err := walk(object, next, origin+"/properties/"+editorPointerToken(name), alternatives); err != nil {
						return err
					}
				}
			case "items", "additionalProperties":
				if _, boolean := raw.(bool); keyword == "additionalProperties" && boolean {
					continue
				}
				child, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("unsupported %s at %s", keyword, origin)
				}
				kind := byte('a')
				if keyword == "additionalProperties" {
					kind = 'm'
				}
				next := append(slices.Clone(path), editorUseStep{kind: kind})
				if err := walk(child, next, origin+"/"+keyword, alternatives); err != nil {
					return err
				}
			case "oneOf", "anyOf":
				branches, ok := raw.([]any)
				if !ok {
					return fmt.Errorf("invalid alternatives at %s", origin)
				}
				for index, rawBranch := range branches {
					branch, ok := rawBranch.(map[string]any)
					if !ok {
						return fmt.Errorf("invalid alternative at %s", origin)
					}
					branchOrigin := origin + "/" + keyword + "/" + strconv.Itoa(index)
					next := append(slices.Clone(alternatives), editorUseAlternative{path: slices.Clone(path), origin: branchOrigin})
					if err := walk(branch, path, branchOrigin, next); err != nil {
						return err
					}
				}
			case "$schema", "definitions", "type", "enum", "description":
			default:
				return fmt.Errorf("unsupported traversal keyword %q at %s", keyword, origin)
			}
		}
		return nil
	}
	if err := walk(root, nil, "#", nil); err != nil {
		return nil, err
	}
	slices.SortFunc(result, func(left, right editorRequiredUse) int {
		return strings.Compare(editorUseSite(left.path)+left.origin+left.property, editorUseSite(right.path)+right.origin+right.property)
	})
	return result, nil
}

func TestManifestEditorSchemaRequiredUseExpansion(t *testing.T) {
	shared := map[string]any{"type": "object", "required": []any{"path"}}
	reference := func() map[string]any { return map[string]any{"$ref": "#/definitions/Source"} }
	root := map[string]any{
		"definitions": map[string]any{"Source": shared, "Unused": map[string]any{"required": []any{"unused"}}},
		"properties": map[string]any{
			"skill":        map[string]any{"items": map[string]any{"properties": map[string]any{"source": reference()}}},
			"instructions": map[string]any{"additionalProperties": map[string]any{"properties": map[string]any{"source": reference()}}},
			"hook_asset":   map[string]any{"additionalProperties": map[string]any{"properties": map[string]any{"source": reference()}}},
		},
	}
	uses, err := editorRequiredUses(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, use := range uses {
		paths = append(paths, editorUseSite(use.path)+"."+use.property)
	}
	slices.Sort(paths)
	want := []string{"$.hook_asset.*.source.path", "$.instructions.*.source.path", "$.skill[*].source.path"}
	if !slices.Equal(paths, want) {
		t.Fatalf("shared-reference coverage=%v, want %v", paths, want)
	}

	branches := map[string]any{"properties": map[string]any{
		"choice": map[string]any{"anyOf": []any{
			map[string]any{"type": "object", "required": []any{"first"}},
			map[string]any{"type": "object", "required": []any{"second"}},
		}},
	}}
	alternatives, err := editorRequiredUses(branches)
	if err != nil || len(alternatives) != 2 {
		t.Fatalf("object alternative coverage=%v error=%v", alternatives, err)
	}
	for _, use := range alternatives {
		if editorUseSite(use.path) != "$.choice" || len(use.alternatives) != 1 {
			t.Fatalf("object alternative lost its use site: %#v", use)
		}
	}
	if alternatives[0].origin == alternatives[1].origin {
		t.Fatal("different object alternatives were deduplicated")
	}

	for _, invalid := range []map[string]any{
		{"$ref": "#/definitions/Absent"},
		{"$ref": "https://example.invalid/schema"},
		{"$ref": "#/definitions/Loop", "definitions": map[string]any{"Loop": map[string]any{"$ref": "#/definitions/Loop"}}},
		{"items": []any{map[string]any{"type": "string"}}},
		{"allOf": []any{map[string]any{"type": "object"}}},
		{"$ref": "#/definitions/A", "required": []any{"ignored"}, "definitions": map[string]any{"A": map[string]any{"type": "object"}}},
	} {
		if _, err := editorRequiredUses(invalid); err == nil {
			t.Fatalf("unsupported traversal produced a result: %v", invalid)
		}
	}
}
