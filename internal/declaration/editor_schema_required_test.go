package declaration_test

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/isty2e/daem/internal/declaration/manifest"
)

type editorOmissionWitness struct {
	content string
	indices []int
	keys    []string
}

func editorWitnessValue(value any, path []editorUseStep, witness editorOmissionWitness) (any, error) {
	arrayIndex, mapIndex := 0, 0
	for _, step := range path {
		switch step.kind {
		case 'p':
			object, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("fixture path %s is not an object", step.name)
			}
			var exists bool
			value, exists = object[step.name]
			if !exists {
				return nil, fmt.Errorf("fixture is missing path %s", step.name)
			}
		case 'a':
			if arrayIndex >= len(witness.indices) {
				return nil, fmt.Errorf("fixture has no concrete array index")
			}
			index := witness.indices[arrayIndex]
			arrayIndex++
			switch array := value.(type) {
			case []any:
				if index < 0 || index >= len(array) {
					return nil, fmt.Errorf("fixture array index out of range")
				}
				value = array[index]
			case []map[string]any:
				if index < 0 || index >= len(array) {
					return nil, fmt.Errorf("fixture table-array index out of range")
				}
				value = array[index]
			default:
				return nil, fmt.Errorf("fixture path is not an array")
			}
		case 'm':
			if mapIndex >= len(witness.keys) {
				return nil, fmt.Errorf("fixture has no concrete map key")
			}
			object, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("fixture path is not a table")
			}
			var exists bool
			value, exists = object[witness.keys[mapIndex]]
			mapIndex++
			if !exists {
				return nil, fmt.Errorf("fixture map key is absent")
			}
		default:
			return nil, fmt.Errorf("unknown fixture path step")
		}
	}
	return value, nil
}

func editorWitnessContainer(value any, path []editorUseStep, witness editorOmissionWitness) (map[string]any, error) {
	value, err := editorWitnessValue(value, path, witness)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("fixture use site is not an object")
	}
	return object, nil
}

func editorWitnessActive(t *testing.T, compiler *jsonschema.Compiler, raw map[string]any, use editorRequiredUse, witness editorOmissionWitness) bool {
	t.Helper()
	for _, alternative := range use.alternatives {
		container, err := editorWitnessValue(raw, alternative.path, witness)
		if err != nil {
			t.Fatal(err)
		}
		value, err := editorJSONValue(container)
		if err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile(editorSchemaResource + alternative.origin)
		if err != nil {
			t.Fatal(err)
		}
		if !editorSchemaAccepts(t, schema, value) {
			return false
		}
	}
	return true
}

func TestManifestEditorSchemaRequiredAtEveryUseSite(t *testing.T) {
	document, compiler, schema := loadEditorSchema(t)
	uses, err := editorRequiredUses(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) == 0 {
		t.Fatal("schema has no required-field behavior to exercise")
	}
	const codex = "version = 1\ntargets = [\"codex\"]\n"
	const skill = codex + "[[skill]]\nname = \"review\"\nsource = { path = \"./missing\", mode = \"vendor\" }\n"
	const group = codex + "[[skill_group]]\nnames = [\"review\"]\nsource = { path = \"./missing\", mode = \"vendor\" }\n"
	const hook = codex + "[[hook]]\nname = \"guard\"\nevent = \"PreToolUse\"\ncommand = \"true\"\n[[hook.target_override]]\ntarget = \"codex\"\n"
	const asset = codex + "[hook_asset.guard]\nsource = \"./missing.sh\"\nkind = \"file\"\n"
	const instructions = codex + "[instructions.project]\nsource = \"./missing.md\"\n"
	const mcp = `version = 1
targets = ["claude-code"]
[[mcp_server]]
name = "docs"
transport = "stdio"
scope = "project"
command = { path = "/missing/schema-tool" }
env = { TOKEN = { from_env = "source_Name" } }
`
	const ambient = `version = 1
targets = ["claude-code"]
[[mcp_server]]
name = "docs"
transport = "stdio"
command = "npx"
`
	const extension = `version = 1
targets = ["pi"]
[[extension]]
id = "tooling"
carrier = "pi-package"
scope = "project"
source = { host_source = "npm:@acme/tooling@1.2.3" }
`
	witnesses := map[string][]editorOmissionWitness{
		"$":                            {{content: codex}},
		"$.skill[*]":                   {{content: skill, indices: []int{0}}},
		"$.skill_group[*]":             {{content: group, indices: []int{0}}},
		"$.hook[*]":                    {{content: hook, indices: []int{0}}},
		"$.hook[*].target_override[*]": {{content: hook, indices: []int{0, 0}}},
		"$.hook_asset.*":               {{content: asset, keys: []string{"guard"}}},
		"$.instructions.*":             {{content: instructions, keys: []string{"project"}}},
		"$.mcp_server[*]":              {{content: mcp, indices: []int{0}}},
		"$.mcp_server[*].command":      {{content: mcp, indices: []int{0}}, {content: ambient, indices: []int{0}}},
		"$.mcp_server[*].env.*":        {{content: mcp, indices: []int{0}, keys: []string{"TOKEN"}}},
		"$.extension[*]":               {{content: extension, indices: []int{0}}},
	}

	for site, candidates := range witnesses {
		for _, witness := range candidates {
			content := []byte(witness.content)
			if _, err := manifest.Decode(content); err != nil {
				t.Fatalf("invalid witness baseline %s: %v", site, err)
			}
			if !editorSchemaAccepts(t, schema, editorSchemaInstance(t, content)) {
				t.Fatalf("schema rejects witness baseline %s", site)
			}
		}
	}

	for _, use := range uses {
		t.Run(editorUseSite(use.path)+"."+use.property+"@"+use.origin, func(t *testing.T) {
			candidates := witnesses[editorUseSite(use.path)]
			matched := false
			for _, witness := range candidates {
				content := []byte(witness.content)
				raw, err := editorTOMLValues(content)
				if err != nil {
					t.Fatal(err)
				}
				if !editorWitnessActive(t, compiler, raw, use, witness) {
					continue
				}
				matched = true
				var baseline bytes.Buffer
				if err := toml.NewEncoder(&baseline).Encode(raw); err != nil {
					t.Fatal(err)
				}
				if _, err := manifest.Decode(baseline.Bytes()); err != nil {
					t.Fatalf("witness round trip changed admission: %v", err)
				}
				if !editorSchemaAccepts(t, schema, editorSchemaInstance(t, baseline.Bytes())) {
					t.Fatal("schema rejects unmodified witness round trip")
				}

				container, err := editorWitnessContainer(raw, use.path, witness)
				if err != nil {
					t.Fatal(err)
				}
				if _, exists := container[use.property]; !exists {
					t.Fatal("witness does not contain the required property")
				}
				delete(container, use.property)
				expected, err := editorJSONValue(raw)
				if err != nil {
					t.Fatal(err)
				}
				var mutated bytes.Buffer
				if err := toml.NewEncoder(&mutated).Encode(raw); err != nil {
					t.Fatal(err)
				}
				actual := editorSchemaInstance(t, mutated.Bytes())
				if !reflect.DeepEqual(expected, actual) {
					t.Fatal("serialization changed values beyond the selected property")
				}
				if editorSchemaAccepts(t, schema, actual) {
					t.Fatal("required omission was accepted by the schema")
				}
				if _, err := manifest.Decode(mutated.Bytes()); err == nil {
					t.Fatal("decoder accepted the omission: required is stricter than daem")
				}
			}
			if !matched {
				t.Fatal("no valid witness for this use site and object variant")
			}
		})
	}
}
