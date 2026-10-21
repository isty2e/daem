package declaration

import "reflect"

func (Source) editorSchema(builder *editorSchemaBuilder) (*editorSchemaNode, error) {
	return builder.object(reflect.TypeFor[Source]())
}

func sourceValueEditorSchema(builder *editorSchemaBuilder) (*editorSchemaNode, error) {
	table, err := builder.node(reflect.TypeFor[Source]())
	if err != nil {
		return nil, err
	}
	return &editorSchemaNode{AnyOf: []*editorSchemaNode{{Type: "string"}, table}}, nil
}

func (InstructionSource) editorSchema(builder *editorSchemaBuilder) (*editorSchemaNode, error) {
	return sourceValueEditorSchema(builder)
}

func (HookAssetSource) editorSchema(builder *editorSchemaBuilder) (*editorSchemaNode, error) {
	return sourceValueEditorSchema(builder)
}

func (MCPEnvReference) editorSchema(_ *editorSchemaBuilder) (*editorSchemaNode, error) {
	return &editorSchemaNode{
		Type: "object", Properties: map[string]*editorSchemaNode{"from_env": {Type: "string"}},
		AdditionalProperties: &editorSchemaAdditional{}, Required: []string{"from_env"},
		Description: "Symbolic environment reference. Values stay in the host environment, not this manifest.",
	}, nil
}
