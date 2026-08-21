package mcpcodec

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

const maximumRejectedJSONProducerAllocationBytes = 128 << 10

func TestMCPJSONCanonicalProducerRejectsOversizedInputWithoutExpandedAllocation(t *testing.T) {
	argument := strings.Repeat("a", 8<<20)
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			_, err := CanonicalClaudeProjectMCPServerEntry(ClaudeProjectMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				Args:            []string{argument},
				Env:             map[string]string{},
				AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
			})
			if err == nil {
				b.Fatal("oversized producer call succeeded")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedJSONProducerAllocationBytes {
		t.Fatalf(
			"oversized producer allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedJSONProducerAllocationBytes,
		)
	}
}

func TestCanonicalJSONRejectsHostReindentExpansionWithoutExpandedAllocation(t *testing.T) {
	const depth = 32
	const items = 100_000
	raw := json.RawMessage(
		strings.Repeat("[", depth) + strings.Repeat("0,", items-1) + "0" + strings.Repeat("]", depth),
	)
	value := map[string]json.RawMessage{"payload": raw}
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			_, err := canonicalJSON(value)
			if err == nil {
				b.Fatal("expanded host JSON succeeded")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedJSONProducerAllocationBytes {
		t.Fatalf(
			"host reindent rejection allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedJSONProducerAllocationBytes,
		)
	}
}

func TestMCPJSONCanonicalProducerRejectsMinimalHostOverflowWithoutIntermediateAllocation(t *testing.T) {
	expansion := claudeProjectMinimalHostArgumentExpansion(t)
	projection := validMCPProjection("context7")
	projection.Args = []string{strings.Repeat("a", expansion+1)}

	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if _, err := CanonicalClaudeProjectMCPServerEntry(projection); err == nil {
				b.Fatal("minimal-host limit+1 producer succeeded")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedJSONProducerAllocationBytes {
		t.Fatalf(
			"minimal-host limit+1 rejection allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedJSONProducerAllocationBytes,
		)
	}

	_, err := CanonicalClaudeProjectMCPServerEntry(projection)
	var projectionErr *MCPProjectionError
	if !errors.As(err, &projectionErr) || projectionErr.Code() != MCPProjectionReasonCanonicalInvalid {
		t.Fatalf("minimal-host limit+1 error = %v, want canonical invalid", err)
	}
}

func TestMCPJSONConfigRejectsPreservedSiblingOverflowWithoutServerObjectAllocation(t *testing.T) {
	expansion := claudeProjectMinimalHostArgumentExpansion(t)
	projection := validMCPProjection("context7")
	projection.Args = []string{strings.Repeat("a", expansion)}
	canonical, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatal(err)
	}

	encode := func() error {
		config := mcpConfig{
			spec: claudeProjectMCPConfigSpec(),
			top: map[string]json.RawMessage{
				"preserved": json.RawMessage(`{"value":"keep"}`),
			},
			servers: map[string]json.RawMessage{"context7": canonical},
		}
		_, err := config.encode()
		return err
	}
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if err := encode(); err == nil {
				b.Fatal("preserved-sibling overflow succeeded")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedJSONProducerAllocationBytes {
		t.Fatalf(
			"preserved-sibling overflow allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedJSONProducerAllocationBytes,
		)
	}

	err = encode()
	var projectionErr *MCPProjectionError
	if !errors.As(err, &projectionErr) || projectionErr.Code() != MCPProjectionReasonCanonicalInvalid {
		t.Fatalf("preserved-sibling overflow error = %v, want canonical invalid", err)
	}
}

func TestMCPJSONMutationPathsRejectCompleteHostOverflowBeforeEntryMaterialization(t *testing.T) {
	expansion := claudeProjectMinimalHostArgumentExpansion(t)
	projection := validMCPProjection("context7")
	projection.Args = []string{strings.Repeat("a", expansion)}
	canonical, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatal(err)
	}
	mutation := mustMCPProjectionUpsert(t, "context7", canonical)
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject)
	existing := []byte(`{"preserved":{"value":"keep"}}`)

	for _, test := range []struct {
		name      string
		operation func() error
	}{
		{
			name: "fold",
			operation: func() error {
				_, err := operations.FoldMutations(existing, []MCPProjectionMutation{mutation})
				return err
			},
		},
		{
			name: "restore",
			operation: func() error {
				_, _, err := operations.RestoreMutations(
					existing,
					[]MCPProjectionMutation{mutation},
					false,
				)
				return err
			},
		},
		{
			name: "direct merge",
			operation: func() error {
				_, err := operations.mergeCanonicalEntry(existing, "context7", canonical)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertMCPJSONRejectedAllocationBound(t, test.operation)
		})
	}
}

func TestMCPJSONFoldRejectsMultiMutationOverflowBeforeEntryMaterialization(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject)
	spec := claudeProjectMCPConfigSpec()
	first := validMCPProjection("alpha")
	first.Args = []string{""}
	firstBase, err := CanonicalClaudeProjectMCPServerEntry(first)
	if err != nil {
		t.Fatal(err)
	}
	second := validMCPProjection("beta")
	second.Args = []string{""}
	secondBase, err := CanonicalClaudeProjectMCPServerEntry(second)
	if err != nil {
		t.Fatal(err)
	}
	baseBytes, err := canonicalMCPJSONConfigEncodedSize(
		nil,
		spec.serversKey,
		map[string]json.RawMessage{
			"alpha": firstBase,
			"beta":  secondBase,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	expansion := int(maximumDocumentBytes - baseBytes)
	first.Args = []string{strings.Repeat("a", expansion/2)}
	second.Args = []string{strings.Repeat("b", expansion-expansion/2+1)}
	firstCanonical, err := CanonicalClaudeProjectMCPServerEntry(first)
	if err != nil {
		t.Fatal(err)
	}
	secondCanonical, err := CanonicalClaudeProjectMCPServerEntry(second)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []MCPProjectionMutation{
		mustMCPProjectionInsert(t, "alpha", firstCanonical),
		mustMCPProjectionInsert(t, "beta", secondCanonical),
	}

	assertMCPJSONRejectedAllocationBound(t, func() error {
		_, err := operations.FoldMutations(nil, mutations)
		return err
	})
}

func assertMCPJSONRejectedAllocationBound(t *testing.T, operation func() error) {
	t.Helper()
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if err := operation(); err == nil {
				b.Fatal("complete-host overflow succeeded")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedJSONProducerAllocationBytes {
		t.Fatalf(
			"complete-host overflow allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedJSONProducerAllocationBytes,
		)
	}

	err := operation()
	var projectionErr *MCPProjectionError
	if !errors.As(err, &projectionErr) || projectionErr.Code() != MCPProjectionReasonCanonicalInvalid {
		t.Fatalf("complete-host overflow error = %v, want canonical invalid", err)
	}
}

func claudeProjectMinimalHostArgumentExpansion(t *testing.T) int {
	t.Helper()
	projection := validMCPProjection("context7")
	projection.Args = []string{""}
	canonical, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatal(err)
	}
	host, err := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject).mergeCanonicalEntry(
		nil,
		"context7",
		canonical,
	)
	if err != nil {
		t.Fatal(err)
	}
	return int(maximumDocumentBytes) - len(host)
}

func TestMCPJSONCanonicalProducersHonorMinimalHostByteBoundary(t *testing.T) {
	type producerCase struct {
		name       string
		operations MCPPlacementOperations
		canonical  func([]string) ([]byte, error)
	}
	cases := []producerCase{
		{
			name:       "claude project",
			operations: mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject),
			canonical: func(args []string) ([]byte, error) {
				projection := validMCPProjection("context7")
				projection.Args = args
				return CanonicalClaudeProjectMCPServerEntry(projection)
			},
		},
		{
			name:       "claude global",
			operations: mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeGlobal),
			canonical: func(args []string) ([]byte, error) {
				projection := validClaudeGlobalMCPProjection("context7")
				projection.Args = args
				return CanonicalClaudeGlobalMCPServerEntry(projection)
			},
		},
		{
			name:       "antigravity global",
			operations: mustMCPCodecOperations(t, aggregate.MCPPlacementAntigravityGlobal),
			canonical: func(args []string) ([]byte, error) {
				projection := validAntigravityMCPProjection("context7")
				projection.Args = args
				return CanonicalAntigravityGlobalMCPServerEntry(projection)
			},
		},
		{
			name:       "opencode project",
			operations: mustMCPCodecOperations(t, aggregate.MCPPlacementOpenCodeProject),
			canonical: func(args []string) ([]byte, error) {
				projection := validOpenCodeMCPProjection("context7")
				projection.Args = args
				return CanonicalOpenCodeProjectMCPServerEntry(projection)
			},
		},
		{
			name:       "opencode global",
			operations: mustMCPCodecOperations(t, aggregate.MCPPlacementOpenCodeGlobal),
			canonical: func(args []string) ([]byte, error) {
				projection := validOpenCodeGlobalMCPProjection("context7")
				projection.Args = args
				return CanonicalOpenCodeGlobalMCPServerEntry(projection)
			},
		},
		{
			name:       "pi project",
			operations: mustPiMCPPlacementOperations(t, aggregate.MCPPlacementPiProject),
			canonical: func(args []string) ([]byte, error) {
				projection := PiMCPAdapterServerProjection{
					ServerID:        "context7",
					Command:         "npx",
					Args:            args,
					Env:             map[string]string{},
					AdapterContract: aggregate.PiMCPAdapterStdioV1,
				}
				return CanonicalPiMCPAdapterServerEntry(projection)
			},
		},
		{
			name:       "pi global",
			operations: mustPiMCPPlacementOperations(t, aggregate.MCPPlacementPiGlobal),
			canonical: func(args []string) ([]byte, error) {
				projection := PiMCPAdapterServerProjection{
					ServerID:        "context7",
					Command:         "npx",
					Args:            args,
					Env:             map[string]string{},
					AdapterContract: aggregate.PiMCPAdapterStdioV1,
				}
				return CanonicalPiMCPAdapterServerEntry(projection)
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			base, err := test.canonical([]string{""})
			if err != nil {
				t.Fatal(err)
			}
			baseHost, err := test.operations.mergeCanonicalEntry(nil, "context7", base)
			if err != nil {
				t.Fatal(err)
			}
			expansion := int(maximumDocumentBytes) - len(baseHost)
			if expansion < 1 {
				t.Fatalf("base host size = %d, want room for boundary fixture", len(baseHost))
			}

			for _, delta := range []int{-1, 0} {
				canonical, err := test.canonical([]string{strings.Repeat("a", expansion+delta)})
				if err != nil {
					t.Fatalf("boundary delta %d: %v", delta, err)
				}
				host, err := test.operations.mergeCanonicalEntry(nil, "context7", canonical)
				if err != nil {
					t.Fatalf("merge boundary delta %d: %v", delta, err)
				}
				if got, want := len(host), int(maximumDocumentBytes)+delta; got != want {
					t.Fatalf("boundary delta %d host bytes = %d, want %d", delta, got, want)
				}
			}

			_, err = test.canonical([]string{strings.Repeat("a", expansion+1)})
			var projectionErr *MCPProjectionError
			if !errors.As(err, &projectionErr) || projectionErr.Code() != MCPProjectionReasonCanonicalInvalid {
				t.Fatalf("limit+1 producer error = %v, want canonical invalid", err)
			}
		})
	}
}

func TestCanonicalJSONPreservesStdlibBytesAcrossSupportedShapes(t *testing.T) {
	values := []struct {
		name  string
		value any
	}{
		{
			name: "claude entry",
			value: ClaudeProjectMCPServerEntry{
				Type:    "stdio",
				Command: `npx<>&\\"`,
				Args:    []string{"", "é", "\u2028", `quote"slash\\`},
				Env:     map[string]string{"B": "${B}", "A": "${A}"},
			},
		},
		{
			name: "pi entry",
			value: PiMCPAdapterServerEntry{
				Command:   "node",
				Args:      []string{},
				Env:       map[string]string{},
				Lifecycle: "lazy",
				Disabled:  false,
			},
		},
		{
			name: "preserved raw messages",
			value: map[string]json.RawMessage{
				"raw":   json.RawMessage(` { "escaped": "\u0061", "html": "<>&", "number": 1e+02 } `),
				"array": json.RawMessage("[{}, [], true, null, \"\u2028\"]\n"),
			},
		},
	}
	for _, test := range values {
		t.Run(test.name, func(t *testing.T) {
			expected, err := json.MarshalIndent(test.value, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			expected = append(expected, '\n')
			actual, err := canonicalJSON(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, expected) {
				t.Fatalf("canonical JSON differs:\nactual:\n%s\nexpected:\n%s", actual, expected)
			}
		})
	}
}

func TestMCPJSONHostEncodedSizeMatchesStdlibWithPreservedAndReplacedFields(t *testing.T) {
	servers := map[string]json.RawMessage{
		"alpha": json.RawMessage(` { "command": "node", "args": ["a"] } `),
		"nil":   nil,
	}
	top := map[string]json.RawMessage{
		"mcpServers": json.RawMessage(`{"stale":{"command":"old"}}`),
		"model":      json.RawMessage(` { "name": "keep", "html": "<>&" } `),
		"nil":        nil,
	}
	measured, err := canonicalMCPJSONConfigEncodedSize(
		top,
		"mcpServers",
		servers,
	)
	if err != nil {
		t.Fatal(err)
	}

	serversRaw, err := encodeSortedRawObject(servers)
	if err != nil {
		t.Fatal(err)
	}
	actualTop := map[string]json.RawMessage{
		"mcpServers": serversRaw,
		"model":      top["model"],
		"nil":        nil,
	}
	actual, err := json.MarshalIndent(actualTop, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	if measured != int64(len(actual)) {
		t.Fatalf("host preflight bytes = %d, stdlib produced %d", measured, len(actual))
	}
}
