package declaration

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/isty2e/daem/internal/encoding/toml"

	"github.com/isty2e/daem/internal/target"
)

const editorSchemaDialect = "http://json-schema.org/draft-04/schema#"

type editorSchemaDocument struct {
	Schema      string                       `json:"$schema"`
	Ref         string                       `json:"$ref"`
	Definitions map[string]*editorSchemaNode `json:"definitions"`
}

type editorSchemaNode struct {
	Ref                  string                       `json:"$ref,omitempty"`
	Type                 string                       `json:"type,omitempty"`
	Properties           map[string]*editorSchemaNode `json:"properties,omitempty"`
	AdditionalProperties *editorSchemaAdditional      `json:"additionalProperties,omitempty"`
	Required             []string                     `json:"required,omitempty"`
	Enum                 []json.RawMessage            `json:"enum,omitempty"`
	Items                *editorSchemaNode            `json:"items,omitempty"`
	OneOf                []*editorSchemaNode          `json:"oneOf,omitempty"`
	AnyOf                []*editorSchemaNode          `json:"anyOf,omitempty"`
	Description          string                       `json:"description,omitempty"`
}

type editorSchemaAdditional struct {
	Value *editorSchemaNode
}

func (additional editorSchemaAdditional) MarshalJSON() ([]byte, error) {
	if additional.Value == nil {
		return []byte("false"), nil
	}
	return json.Marshal(additional.Value)
}

type editorSchemaProjector interface {
	editorSchema(*editorSchemaBuilder) (*editorSchemaNode, error)
}

type editorSchemaBuilder struct {
	definitions map[string]*editorSchemaNode
	names       map[string]reflect.Type
	active      map[reflect.Type]bool
}

func manifestEditorSchema() ([]byte, error) {
	builder := editorSchemaBuilder{
		definitions: make(map[string]*editorSchemaNode),
		names:       make(map[string]reflect.Type),
		active:      make(map[reflect.Type]bool),
	}
	root, err := builder.node(reflect.TypeFor[Manifest]())
	if err != nil {
		return nil, err
	}
	document := editorSchemaDocument{
		Schema: editorSchemaDialect, Ref: root.Ref, Definitions: builder.definitions,
	}
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func (builder *editorSchemaBuilder) node(typ reflect.Type) (*editorSchemaNode, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	projector, custom := reflect.New(typ).Interface().(editorSchemaProjector)
	if custom || typ.Kind() == reflect.Struct {
		if typ.Name() == "" {
			return builder.object(typ)
		}
		if previous, exists := builder.names[typ.Name()]; exists && previous != typ {
			return nil, fmt.Errorf("editor schema definition name collision: %s and %s", previous, typ)
		}
		if builder.active[typ] {
			return nil, fmt.Errorf("recursive editor schema type %s needs an explicit projection", typ)
		}
		if _, exists := builder.definitions[typ.Name()]; exists {
			return &editorSchemaNode{Ref: "#/definitions/" + typ.Name()}, nil
		}

		builder.names[typ.Name()] = typ
		builder.active[typ] = true
		var definition *editorSchemaNode
		var err error
		if custom {
			definition, err = projector.editorSchema(builder)
		} else if editorSchemaCustomDecoder(typ) {
			err = fmt.Errorf("custom decoder %s needs an editor schema projection", typ)
		} else {
			definition, err = builder.object(typ)
		}
		delete(builder.active, typ)
		if err != nil {
			return nil, err
		}
		builder.definitions[typ.Name()] = definition
		return &editorSchemaNode{Ref: "#/definitions/" + typ.Name()}, nil
	}
	if editorSchemaCustomDecoder(typ) {
		return nil, fmt.Errorf("custom decoder %s needs an editor schema projection", typ)
	}

	switch typ.Kind() {
	case reflect.String:
		return &editorSchemaNode{Type: "string"}, nil
	case reflect.Bool:
		return &editorSchemaNode{Type: "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &editorSchemaNode{Type: "integer"}, nil
	case reflect.Slice, reflect.Array:
		item, err := builder.node(typ.Elem())
		return &editorSchemaNode{Type: "array", Items: item}, err
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("editor schema map %s must have string keys", typ)
		}
		value, err := builder.node(typ.Elem())
		return &editorSchemaNode{Type: "object", AdditionalProperties: &editorSchemaAdditional{Value: value}}, err
	default:
		return nil, fmt.Errorf("unsupported editor schema type %s", typ)
	}
}

func editorSchemaCustomDecoder(typ reflect.Type) bool {
	for _, contract := range []reflect.Type{
		reflect.TypeFor[tomlsyntax.Unmarshaler](), reflect.TypeFor[encoding.TextUnmarshaler](),
	} {
		if typ.Implements(contract) || reflect.PointerTo(typ).Implements(contract) {
			return true
		}
	}
	return false
}

func (builder *editorSchemaBuilder) object(typ reflect.Type) (*editorSchemaNode, error) {
	node := &editorSchemaNode{
		Type: "object", Properties: make(map[string]*editorSchemaNode),
		AdditionalProperties: &editorSchemaAdditional{}, Required: editorSchemaRequired(typ),
	}
	for index := range typ.NumField() {
		field := typ.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
		if name == "-" {
			continue
		}
		if field.Anonymous {
			return nil, fmt.Errorf("editor schema type %s has anonymous field %s", typ, field.Name)
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			return nil, fmt.Errorf("editor schema field %s.%s requires an explicit TOML name", typ, field.Name)
		}
		if _, duplicate := node.Properties[name]; duplicate {
			return nil, fmt.Errorf("duplicate editor schema property %s.%s", typ, name)
		}

		property, err := builder.field(typ, field)
		if err != nil {
			return nil, err
		}
		node.Properties[name] = property
	}
	return node, nil
}

func (builder *editorSchemaBuilder) field(owner reflect.Type, field reflect.StructField) (*editorSchemaNode, error) {
	property, err := builder.node(field.Type)
	if err != nil {
		return nil, err
	}
	manifest := owner == reflect.TypeFor[Manifest]()
	resource := editorSchemaResource(owner)

	switch {
	case manifest && field.Name == "Version":
		property.Enum = []json.RawMessage{json.RawMessage(strconv.FormatInt(CurrentManifestVersion, 10))}
		property.Description = "Manifest format version. This does not select a daem release or editor schema."
	case (manifest || resource) && field.Name == "Targets":
		property.Items.Enum = editorSchemaTargetEnum()
		if !manifest {
			property.Description = "Omission or an empty array inherits the manifest target set."
		}
	case (resource || owner == reflect.TypeFor[Defaults]()) && field.Name == "Scope":
		values := []string{""}
		for _, scope := range target.SupportedScopes() {
			values = append(values, string(scope))
		}
		property.Enum = editorSchemaStringEnum(values...)
		property.Description = "Omission or an empty string uses the applicable default scope."
	case owner == reflect.TypeFor[HookTargetOverride]() && field.Name == "Target":
		property.Enum = editorSchemaTargetEnum()
	case field.Name == "Target" && (owner == reflect.TypeFor[Skill]() ||
		owner == reflect.TypeFor[SkillGroup]() || owner == reflect.TypeFor[Instructions]()):
		value, valueErr := builder.node(field.Type.Elem())
		if valueErr != nil {
			return nil, valueErr
		}
		property = &editorSchemaNode{
			Type: "object", Properties: make(map[string]*editorSchemaNode),
			AdditionalProperties: &editorSchemaAdditional{},
		}
		for _, selected := range target.SupportedTargets() {
			property.Properties[string(selected)] = value
		}
	}
	return property, nil
}

func editorSchemaResource(typ reflect.Type) bool {
	switch typ {
	case reflect.TypeFor[Skill](), reflect.TypeFor[SkillGroup](), reflect.TypeFor[Hook](),
		reflect.TypeFor[HookAsset](), reflect.TypeFor[MCPServer](), reflect.TypeFor[Instructions](),
		reflect.TypeFor[Extension]():
		return true
	default:
		return false
	}
}

func editorSchemaRequired(typ reflect.Type) []string {
	switch typ {
	case reflect.TypeFor[Manifest]():
		return []string{"version", "targets"}
	case reflect.TypeFor[Skill]():
		return []string{"name", "source"}
	case reflect.TypeFor[SkillGroup](), reflect.TypeFor[Instructions]():
		return []string{"source"}
	case reflect.TypeFor[Hook]():
		return []string{"name", "event", "command"}
	case reflect.TypeFor[HookAsset]():
		return []string{"source", "kind"}
	case reflect.TypeFor[MCPServer]():
		return []string{"name", "transport", "command"}
	case reflect.TypeFor[Extension]():
		return []string{"id", "carrier", "source"}
	case reflect.TypeFor[HookTargetOverride]():
		return []string{"target"}
	default:
		return nil
	}
}

func editorSchemaStringEnum(values ...string) []json.RawMessage {
	result := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		result = append(result, encoded)
	}
	return result
}

func editorSchemaTargetEnum() []json.RawMessage {
	values := make([]string, 0, len(target.SupportedTargets()))
	for _, selected := range target.SupportedTargets() {
		values = append(values, string(selected))
	}
	return editorSchemaStringEnum(values...)
}
