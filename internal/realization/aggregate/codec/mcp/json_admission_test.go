package mcpcodec

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPJSONHostDocumentsRejectInvalidUTF8AndExcessiveDepth(t *testing.T) {
	specs := []struct {
		name string
		spec mcpConfigSpec
	}{
		{name: "claude project", spec: claudeProjectMCPConfigSpec()},
		{name: "claude global", spec: claudeGlobalMCPConfigSpec()},
		{name: "antigravity global", spec: antigravityGlobalMCPConfigSpec()},
		{name: "opencode project", spec: openCodeProjectMCPConfigSpec()},
		{name: "opencode global", spec: openCodeGlobalMCPConfigSpec()},
		{name: "pi project", spec: newPiMCPPlacementCodec(aggregate.MCPPlacementPiProject).spec},
		{name: "pi global", spec: newPiMCPPlacementCodec(aggregate.MCPPlacementPiGlobal).spec},
	}

	invalidUTF8 := []byte(`{"unmanaged":"`)
	invalidUTF8 = append(invalidUTF8, 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	deep := []byte(`{"unmanaged":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`)

	for _, test := range specs {
		t.Run(test.name, func(t *testing.T) {
			for name, content := range map[string][]byte{
				"invalid UTF-8":   invalidUTF8,
				"excessive depth": deep,
			} {
				t.Run(name, func(t *testing.T) {
					_, err := decodeMCPConfig(content, test.spec)
					assertMCPProjectionReason(t, err, MCPProjectionReasonConfigMalformed)
				})
			}
		})
	}
}

func TestMCPJSONHostDocumentDepthBoundaryIsInclusive(t *testing.T) {
	// The root object contributes one edge before the nested array value.
	exact := []byte(`{"unmanaged":` + strings.Repeat("[", 63) + `0` + strings.Repeat("]", 63) + `}`)
	if _, err := decodeMCPConfig(exact, claudeProjectMCPConfigSpec()); err != nil {
		t.Fatalf("exact-depth host JSON rejected: %v", err)
	}
}

func TestPiMCPJSONCDepthIsBoundedBeforeProviderParsing(t *testing.T) {
	spec := newPiMCPPlacementCodec(aggregate.MCPPlacementPiProject).spec
	exact := []byte(`{"mcpServers":` + strings.Repeat("[", 63) + `0` + strings.Repeat("]", 63) + ", // comment\n}")
	_, err := decodeMCPConfig(exact, spec)
	assertMCPProjectionReason(t, err, MCPProjectionReasonProviderDocumentLossy)

	over := []byte(`{"mcpServers":` + strings.Repeat("[", 64) + `0` + strings.Repeat("]", 64) + ", // comment\n}")
	_, err = decodeMCPConfig(over, spec)
	assertMCPProjectionReason(t, err, MCPProjectionReasonConfigMalformed)
}

func TestPiMCPJSONCNestingGuardIgnoresStringsAndComments(t *testing.T) {
	spec := newPiMCPPlacementCodec(aggregate.MCPPlacementPiProject).spec
	content := []byte("{\n" +
		"  // [[[[{{{{\n" +
		"  \"text\": \"[[[[{{{{\\\"still-string\",\n" +
		"  /* ]]]]}}}} */\n" +
		"  \"mcpServers\": {},\n" +
		"}\n")
	_, err := decodeMCPConfig(content, spec)
	assertMCPProjectionReason(t, err, MCPProjectionReasonProviderDocumentLossy)
}

func TestMCPJSONCanonicalProducerRejectsEncodingExpansionBeyondDocumentLimit(t *testing.T) {
	_, err := CanonicalClaudeProjectMCPServerEntry(ClaudeProjectMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{strings.Repeat(`"`, 2_200_000)},
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err == nil {
		t.Fatal("canonical JSON producer accepted output beyond the document limit")
	}
	var projectionErr *MCPProjectionError
	if !errors.As(err, &projectionErr) || projectionErr.Code() != MCPProjectionReasonCanonicalInvalid {
		t.Fatalf("producer error = %v, want canonical invalid", err)
	}
}

func TestMCPJSONCanonicalEntryAdmissionIsOriginAware(t *testing.T) {
	operations, ok := ImplementedMCPPlacementOperationsForPlacement(aggregate.MCPPlacementClaudeProject)
	if !ok {
		t.Fatal("Claude project placement operations missing")
	}
	invalidUTF8 := []byte(`{"type":"stdio","command":"npx","args":["`)
	invalidUTF8 = append(invalidUTF8, 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"],"env":{}}`)...)
	deep := []byte(`{"type":"stdio","command":"npx","args":[],"env":{},"extra":` +
		strings.Repeat("[", 64) + `0` + strings.Repeat("]", 64) + `}`)
	duplicate := []byte(`{"type":"stdio","command":"npx","command":"node","args":[],"env":{}}`)

	for name, content := range map[string][]byte{
		"invalid UTF-8":   invalidUTF8,
		"excessive depth": deep,
		"duplicate key":   duplicate,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := operations.mergeCanonicalEntry(nil, "context7", content)
			assertMCPProjectionReason(t, err, MCPProjectionReasonCanonicalInvalid)
		})
	}
}
