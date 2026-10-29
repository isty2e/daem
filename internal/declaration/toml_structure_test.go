package declaration

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/toml"
	testscale "github.com/isty2e/daem/test/scale"
)

const maximumRejectedManifestStructureAllocationBytes = 256 << 10

func TestManifestDecodersRejectOverLimitTOMLStructure(t *testing.T) {
	testscale.Require(t)

	tests := []struct {
		name    string
		content []byte
		want    error
	}{
		{
			name:    "depth",
			content: manifestWithNestedInlineTables(tomlsyntax.MaximumDepth),
			want:    tomlsyntax.ErrMaximumDepthExceeded,
		},
		{
			name:    "containers",
			content: manifestWithTables(tomlsyntax.MaximumContainers + 1),
			want:    tomlsyntax.ErrMaximumContainersExceeded,
		},
		{
			name:    "work",
			content: manifestWithArrayValues(tomlsyntax.MaximumWork),
			want:    tomlsyntax.ErrMaximumWorkExceeded,
		},
		{
			name:    "path work",
			content: manifestWithLongAncestorSiblings(tomlsyntax.MaximumPathWork/tomlsyntax.MaximumKeyBytes + 1),
			want:    tomlsyntax.ErrMaximumPathWorkExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeManifest(test.content); !errors.Is(err, test.want) {
				t.Fatalf("DecodeManifest error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodeManifestHeaderRejectsOverLimitTOMLStructure(t *testing.T) {
	_, err := DecodeManifestHeader(manifestWithNestedInlineTables(tomlsyntax.MaximumDepth))
	if !errors.Is(err, tomlsyntax.ErrMaximumDepthExceeded) {
		t.Fatalf("DecodeManifestHeader error = %v, want depth exceeded", err)
	}
	if err == nil || !strings.HasPrefix(err.Error(), "parse manifest header:") {
		t.Fatalf("DecodeManifestHeader error = %v, want parse manifest header prefix", err)
	}
}

func TestManifestDecodersAcceptExactTOMLDepthBeforeSemanticValidation(t *testing.T) {
	content := manifestWithNestedInlineTables(tomlsyntax.MaximumDepth - 1)
	if _, err := DecodeManifest(content); errors.Is(err, tomlsyntax.ErrMaximumDepthExceeded) {
		t.Fatalf("DecodeManifest exact-depth error = %v", err)
	}
	if _, err := DecodeManifestHeader(content); err != nil {
		t.Fatalf("DecodeManifestHeader exact-depth error = %v", err)
	}
}

func TestDecodeManifestRejectsDeepTOMLBeforeDecoderAllocation(t *testing.T) {
	testscale.Require(t)

	content := manifestWithNestedInlineTables(256)
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if _, err := DecodeManifest(content); err == nil {
				b.Fatal("DecodeManifest accepted over-depth document")
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > maximumRejectedManifestStructureAllocationBytes {
		t.Fatalf(
			"over-depth manifest allocated %d bytes per call, want at most %d",
			got,
			maximumRejectedManifestStructureAllocationBytes,
		)
	}
}

func manifestWithNestedInlineTables(depth int) []byte {
	var builder strings.Builder
	builder.WriteString("version = 1\nextra = ")
	for range depth {
		builder.WriteString("{ k = ")
	}
	builder.WriteByte('1')
	for range depth {
		builder.WriteString(" }")
	}
	builder.WriteByte('\n')
	return []byte(builder.String())
}

func manifestWithTables(count int) []byte {
	var builder strings.Builder
	builder.WriteString("version = 1\n")
	for index := range count {
		fmt.Fprintf(&builder, "[extra%d]\n", index)
	}
	return []byte(builder.String())
}

func manifestWithArrayValues(count int) []byte {
	var builder strings.Builder
	builder.Grow(count*3 + 32)
	builder.WriteString("version = 1\nextra = [")
	for index := range count {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteByte('0')
	}
	builder.WriteString("]\n")
	return []byte(builder.String())
}

func manifestWithLongAncestorSiblings(siblings int) []byte {
	var builder strings.Builder
	builder.WriteString("version = 1\n[")
	builder.WriteString(strings.Repeat("a", tomlsyntax.MaximumKeyBytes))
	builder.WriteString("]\n")
	for range siblings {
		builder.WriteString("k = 1\n")
	}
	return []byte(builder.String())
}
