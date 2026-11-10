package mcpcodec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPJSONRenderAndRestorePreserveUnselectedConcreteValues(t *testing.T) {
	cases := mcpProjectionMutationCases()
	for _, placement := range []aggregate.MCPPlacementID{aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiGlobal} {
		cases = append(cases, mcpProjectionMutationCase{
			name: string(placement), placement: placement,
			canonical: func(serverID, command string) ([]byte, error) {
				return CanonicalPiMCPAdapterServerEntry(PiMCPAdapterServerProjection{
					ServerID: serverID, Command: command, AdapterContract: aggregate.PiMCPAdapterStdioV1,
				})
			},
		})
	}
	for _, test := range cases {
		operations := mustMCPCodecOperations(t, test.placement)
		placement := operations.Placement()
		if placement.Target() == target.TargetCodex {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			prefix := strings.Trim(string(placement.ContentPathPrefix()), "/")
			outside := `"outside": 1e+02`
			manual := `"manual": {"future": [1e+03, "literal"]}`
			original := []byte(fmt.Sprintf(`{%s, %q: {%s}}`, outside, prefix, manual))
			canonical := mustMutationCanonical(t, test, "selected", "node")
			contribution := mcpCodecContribution(t, placement, "selected", canonical)
			selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
			if err != nil {
				t.Fatal(err)
			}
			codec, found := For(placement.CodecContractID())
			if !found {
				t.Fatal("fixture codec is unavailable")
			}
			baseline, failure := codec.Read(aggregate.ExistingDocument(original), selection)
			if failure != nil {
				t.Fatal(failure)
			}
			desired := mcpCodecExclusiveSet(t, contribution)
			intent, err := aggregate.NewProjectionIntent(baseline.States()[0], &desired)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := aggregate.NewPlan(baseline, []aggregate.ProjectionIntent{intent})
			if err != nil {
				t.Fatal(err)
			}
			rendered, failure := codec.Render(aggregate.ExistingDocument(original), plan)
			if failure != nil {
				t.Fatal(failure)
			}
			restored, failure := codec.Restore(rendered.Document(), baseline)
			if failure != nil {
				t.Fatal(failure)
			}
			for phase, document := range map[string]aggregate.Document{
				"render": rendered.Document(), "restore": restored.Document(),
			} {
				content := string(document.Content())
				if !strings.Contains(content, outside) || !strings.Contains(content, manual) {
					t.Fatalf("%s rewrote unselected concrete values: %s", phase, content)
				}
			}
			actual, failure := codec.Read(restored.Document(), selection)
			if failure != nil || !actual.Equal(baseline) {
				t.Fatal("inverse did not restore its selected semantic baseline", failure)
			}
			unchanged, err := operations.removeProjection(original, "absent")
			if err != nil || !bytes.Equal(unchanged, original) {
				t.Fatal("absent removal rewrote the document", err)
			}
		})
	}
}

func TestOpenCodeMCPJSONCForwardAndInverseKeepCommentsAndLiteralRows(t *testing.T) {
	for _, test := range mcpProjectionMutationCases() {
		operations := mustMCPCodecOperations(t, test.placement)
		if operations.Placement().Target() != target.TargetOpenCode {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			before := mustMutationCanonical(t, test, "selected", "old")
			beforeWithComment := strings.Replace(string(before), "{", "{/* selected entry */", 1)
			manual := `"manual": {"future": [1e+03, "literal"]}`
			tuple := `"plugin": ["foreign", ["literal", {"marker":true}]]`
			original := []byte(fmt.Sprintf("// header\n{\"mcp\":{%s, \"selected\":%s,}, %s, /* trailing */}", manual, beforeWithComment, tuple))
			witness := bytes.Clone(original)
			if _, err := operations.ObserveCanonicalEntries(original, []string{"selected"}); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(original, witness) {
				t.Fatal("JSONC observation mutated the caller's disk witness")
			}
			after := mustMutationCanonical(t, test, "selected", "new")
			forward := []MCPProjectionMutation{mustMCPProjectionUpsert(t, "selected", after)}
			changed, err := operations.FoldMutations(original, forward)
			if err != nil {
				t.Fatal(err)
			}
			if err := operations.VerifyMutations(changed, forward); err != nil {
				t.Fatal(err)
			}
			inverse := []MCPProjectionMutation{mustMCPProjectionUpsert(t, "selected", before)}
			restored, keep, err := operations.RestoreMutations(changed, inverse, true)
			if err != nil || !keep {
				t.Fatal("JSONC inverse lost its document", err)
			}
			if err := operations.VerifyMutations(restored, inverse); err != nil {
				t.Fatal(err)
			}
			for phase, content := range map[string][]byte{"forward": changed, "inverse": restored} {
				for _, literal := range []string{manual, tuple} {
					if !bytes.Contains(content, []byte(literal)) {
						t.Fatalf("%s rewrote unselected JSONC literal %s", phase, literal)
					}
				}
				for _, comment := range []string{"// header", "/* selected entry */", "/* trailing */"} {
					if strings.Count(string(content), comment) != 1 {
						t.Fatalf("%s lost or duplicated %s", phase, comment)
					}
				}
			}
		})
	}
}

func TestOpenCodeMCPJSONCInverseKeepsCommentsWithoutKeepingNewParent(t *testing.T) {
	for _, test := range mcpProjectionMutationCases() {
		operations := mustMCPCodecOperations(t, test.placement)
		if operations.Placement().Target() != target.TargetOpenCode {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			canonical := mustMutationCanonical(t, test, "selected", "node")
			contribution := mcpCodecContribution(t, operations.Placement(), "selected", canonical)
			selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
			if err != nil {
				t.Fatal(err)
			}
			codec, found := For(contribution.CodecContractID())
			if !found {
				t.Fatal("fixture codec is unavailable")
			}
			baseline, failure := codec.Read(aggregate.AbsentDocument(), selection)
			if failure != nil {
				t.Fatal(failure)
			}
			entry := strings.Replace(string(canonical), "{", "{/* entry comment */", 1)
			current := []byte(fmt.Sprintf("// user comment\n{\"mcp\":{/* parent comment */\"selected\":%s,},}", entry))
			restored, failure := codec.Restore(aggregate.ExistingDocument(current), baseline)
			if failure != nil || !restored.Document().Exists() {
				t.Fatal("inverse dropped its comments-only document", failure)
			}
			for _, comment := range []string{"// user comment", "/* parent comment */", "/* entry comment */"} {
				if strings.Count(string(restored.Document().Content()), comment) != 1 {
					t.Fatalf("inverse lost or duplicated %s", comment)
				}
			}
			present, err := operations.parentPresent(restored.Document().Content())
			if err != nil || present {
				t.Fatal("inverse kept a parent absent from its baseline", err)
			}
		})
	}
}

func TestMCPJSONEditedImageStillObeysPhysicalByteLimit(t *testing.T) {
	test := mcpProjectionMutationCases()[0]
	operations := mustMCPCodecOperations(t, test.placement)
	canonical := mustMutationCanonical(t, test, "selected", "node")
	content := []byte(`{"outside":true` + strings.Repeat(" ", int(maximumDocumentBytes)-len(`{"outside":true}`)) + `}`)
	_, err := operations.FoldMutations(content, []MCPProjectionMutation{mustMCPProjectionInsert(t, "selected", canonical)})
	assertMCPProjectionReason(t, err, MCPProjectionReasonCanonicalInvalid)
}

func TestPiMCPJSONCRefusalDoesNotMutateCallerWitness(t *testing.T) {
	for _, placement := range []aggregate.MCPPlacementID{aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiGlobal} {
		operations := mustMCPCodecOperations(t, placement)
		content := []byte("// header\n{\"mcpServers\":{},}")
		witness := bytes.Clone(content)
		_, err := operations.ObserveCanonicalEntries(content, []string{"selected"})
		assertMCPProjectionReason(t, err, MCPProjectionReasonProviderDocumentLossy)
		if !bytes.Equal(content, witness) {
			t.Fatal("refused provider observation mutated its caller's disk witness")
		}
		_, err = operations.ObserveCanonicalEntries([]byte(`{/* duplicate */"mcpServers":{},"mcpServers":{}}`), []string{"selected"})
		assertMCPProjectionReason(t, err, MCPProjectionReasonDuplicateKey)
	}
}

func TestOpenCodeMCPJSONCReaderAndEditorAgreeOnLineComments(t *testing.T) {
	for _, test := range mcpProjectionMutationCases() {
		operations := mustMCPCodecOperations(t, test.placement)
		if operations.Placement().Target() != target.TargetOpenCode {
			continue
		}
		canonical := mustMutationCanonical(t, test, "manual", "node")
		var compact bytes.Buffer
		if err := json.Compact(&compact, canonical); err != nil {
			t.Fatal(err)
		}
		replacement := mustMutationCanonical(t, test, "manual", "replacement")
		for name, content := range map[string]string{
			"LF":      fmt.Sprintf("{\"mcp\":{ // note\n\"manual\":%s,\n}}", compact.String()),
			"CRLF":    fmt.Sprintf("{\"mcp\":{ // note\r\n\"manual\":%s,\r\n}}", compact.String()),
			"bare CR": fmt.Sprintf("{\"mcp\":{ // note\r\"manual\":%s,\n}}", compact.String()),
			"EOF":     fmt.Sprintf("{\"mcp\":{\"manual\":%s}} // EOF", compact.String()),
		} {
			t.Run(test.name+"/"+name, func(t *testing.T) {
				limits := configdocument.Limits{MaximumBytes: maximumDocumentBytes, MaximumDepth: maximumMCPJSONDepth}
				document, err := configdocument.Parse(t.Context(), []byte(content), configdocument.JSONC, limits)
				if err != nil {
					t.Fatal(err)
				}
				path, err := output.NewKeyPath("mcp", "manual")
				if err != nil {
					t.Fatal(err)
				}
				_, present, err := document.Lookup(path)
				if err != nil || !present {
					t.Fatal("editor did not retain the live entry", err)
				}
				present, err = operations.entryPresent([]byte(content), "manual")
				if err != nil || !present {
					t.Error("semantic reader disagrees with the editor", err)
				}
				_, err = operations.FoldMutations([]byte(content), []MCPProjectionMutation{mustMCPProjectionInsert(t, "manual", replacement)})
				if err == nil {
					t.Fatal("insert replaced an entry without replacement authority")
				}
			})
		}
	}
}

func TestOpenCodeMCPJSONCRejectsInvalidSemanticInputs(t *testing.T) {
	for _, placement := range []aggregate.MCPPlacementID{aggregate.MCPPlacementOpenCodeProject, aggregate.MCPPlacementOpenCodeGlobal} {
		operations := mustMCPCodecOperations(t, placement)
		for _, test := range []struct {
			name   string
			source string
			reason MCPProjectionReasonCode
		}{
			{"duplicate", `{"mcp":{},/* duplicate */"mcp":{}}`, MCPProjectionReasonDuplicateKey},
			{"surrogate", "// comment\n" + `{"mcp":{},"outside":"\ud800"}`, MCPProjectionReasonConfigMalformed},
			{"UTF-8 value", "// comment\n{\"mcp\":{},\"outside\":\"\xff\"}", MCPProjectionReasonConfigMalformed},
			{"UTF-8 comment", "// invalid \xff\n{\"mcp\":{}}", MCPProjectionReasonConfigMalformed},
			{"depth", "// comment\n{\"mcp\":{},\"outside\":" + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}", MCPProjectionReasonConfigMalformed},
		} {
			t.Run(string(placement)+"/"+test.name, func(t *testing.T) {
				_, err := operations.ObserveCanonicalEntries([]byte(test.source), []string{"selected"})
				assertMCPProjectionReason(t, err, test.reason)
			})
		}
	}
}
