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
