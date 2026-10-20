package declaration

func (MCPCommand) editorSchema(_ *editorSchemaBuilder) (*editorSchemaNode, error) {
	return &editorSchemaNode{AnyOf: []*editorSchemaNode{
		{Type: "string"},
		{
			Type: "object", Properties: map[string]*editorSchemaNode{"path": {Type: "string"}},
			AdditionalProperties: &editorSchemaAdditional{}, Required: []string{"path"},
		},
	}}, nil
}
