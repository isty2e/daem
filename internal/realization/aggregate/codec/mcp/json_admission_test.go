package mcpcodec

import (
	"bytes"
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

func TestMCPJSONHostDocumentsRejectUnpairedSurrogateEscapes(t *testing.T) {
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
	content := []byte(`{"unmanaged":"\ud800"}`)
	for _, test := range specs {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeMCPConfig(content, test.spec)
			assertMCPProjectionReason(t, err, MCPProjectionReasonConfigMalformed)
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

func TestPiMCPJSONCRejectsUnpairedSurrogateAfterStandardization(t *testing.T) {
	spec := newPiMCPPlacementCodec(aggregate.MCPPlacementPiProject).spec
	invalid := []byte("{\n" +
		`  "mcpServers": {"context7": {"command": "node", "args": ["\ud800"]}},` + "\n" +
		"}\n")
	_, err := decodeMCPConfig(invalid, spec)
	assertMCPProjectionReason(t, err, MCPProjectionReasonConfigMalformed)

	validPair := []byte("{\n" +
		`  "mcpServers": {"context7": {"command": "node", "args": ["\ud83d\ude00"]}},` + "\n" +
		"}\n")
	_, err = decodeMCPConfig(validPair, spec)
	assertMCPProjectionReason(t, err, MCPProjectionReasonProviderDocumentLossy)
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

func TestMCPJSONCanonicalEntriesRejectUnpairedSurrogatesForEveryPlacement(t *testing.T) {
	type canonicalCase struct {
		name       string
		operations MCPPlacementOperations
		canonical  []byte
	}
	cases := make([]canonicalCase, 0, 7)
	for _, test := range mcpProjectionMutationCases() {
		if test.placement == aggregate.MCPPlacementCodexProject ||
			test.placement == aggregate.MCPPlacementCodexGlobal {
			continue
		}
		cases = append(cases, canonicalCase{
			name:       test.name,
			operations: mustMCPCodecOperations(t, test.placement),
			canonical:  mustMutationCanonical(t, test, "context7", "npx"),
		})
	}
	for _, placementID := range []aggregate.MCPPlacementID{
		aggregate.MCPPlacementPiProject,
		aggregate.MCPPlacementPiGlobal,
	} {
		cases = append(cases, canonicalCase{
			name:       string(placementID),
			operations: mustPiMCPPlacementOperations(t, placementID),
			canonical:  mustPiMCPAdapterCanonical(t, "context7"),
		})
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			invalid := bytes.Replace(test.canonical, []byte(`"npx"`), []byte(`"\ud800"`), 1)
			if bytes.Equal(invalid, test.canonical) {
				t.Fatal("canonical fixture does not contain the command string")
			}
			_, err := test.operations.mergeCanonicalEntry(nil, "context7", invalid)
			assertMCPProjectionReason(t, err, MCPProjectionReasonCanonicalInvalid)

			placement := test.operations.Placement()
			codec, ok := For(placement.CodecContractID())
			if !ok {
				t.Fatal("MCP codec missing")
			}
			contribution := mcpCodecContribution(t, placement, "context7", invalid)
			err = codec.ValidateContributions(mcpCodecExclusiveSet(t, contribution))
			var failure *aggregate.CodecFailure
			if !errors.As(err, &failure) || failure.Reason() != aggregate.CodecFailureCanonicalInvalid {
				t.Fatalf("ValidateContributions(lone surrogate) = %v, want canonical invalid", err)
			}
		})
	}
}

func TestMCPJSONLoneSurrogateCannotReachLifecycleMaterialization(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject)
	host := []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"node","args":["\ud800"],"env":{}}}}`)
	if _, _, err := collectClaudeProjectMCPServerProjections(t.Context(), host); err == nil {
		t.Fatal("host extraction accepted a lone surrogate")
	} else {
		assertMCPProjectionReason(t, err, MCPProjectionReasonConfigMalformed)
	}
	if _, err := operations.ObserveCanonicalEntries(host, []string{"context7"}); err == nil {
		t.Fatal("canonical observation accepted a lone surrogate")
	} else {
		assertMCPProjectionReason(t, err, MCPProjectionReasonConfigMalformed)
	}

	canonical := []byte(`{"type":"stdio","command":"node","args":["\ud800"],"env":{}}`)
	mutation, err := NewMCPProjectionUpsert("context7", canonical)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := operations.RestoreMutations(nil, []MCPProjectionMutation{mutation}, false); err == nil {
		t.Fatal("restore accepted a lone-surrogate canonical entry")
	} else {
		assertMCPProjectionReason(t, err, MCPProjectionReasonCanonicalInvalid)
	}
	if _, _, _, err := DecodeRuntimeProbeLaunch(
		aggregate.MCPPlacementClaudeProject,
		string(canonical),
	); err == nil {
		t.Fatal("runtime probe accepted a lone-surrogate canonical entry")
	} else {
		assertMCPProjectionReason(t, err, MCPProjectionReasonCanonicalInvalid)
	}
}

func TestMCPJSONValidSurrogatePairPreservesTheDecodedScalar(t *testing.T) {
	host := []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"node","args":["\ud83d\ude00"],"env":{}}}}`)
	projections, rejections, err := collectClaudeProjectMCPServerProjections(t.Context(), host)
	if err != nil || len(rejections) != 0 || len(projections) != 1 {
		t.Fatalf("extraction = %#v, %#v, %v", projections, rejections, err)
	}
	if len(projections[0].Args) != 1 || projections[0].Args[0] != "😀" {
		t.Fatalf("decoded args = %#v, want one exact scalar", projections[0].Args)
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
