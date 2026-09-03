package declaration

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
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
			content: manifestWithNestedInlineTables(tomlstrict.MaximumDepth),
			want:    tomlstrict.ErrMaximumDepthExceeded,
		},
		{
			name:    "containers",
			content: manifestWithTables(tomlstrict.MaximumContainers + 1),
			want:    tomlstrict.ErrMaximumContainersExceeded,
		},
		{
			name:    "work",
			content: manifestWithArrayValues(tomlstrict.MaximumWork),
			want:    tomlstrict.ErrMaximumWorkExceeded,
		},
		{
			name:    "path work",
			content: manifestWithLongAncestorSiblings(tomlstrict.MaximumPathWork/tomlstrict.MaximumKeyBytes + 1),
			want:    tomlstrict.ErrMaximumPathWorkExceeded,
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
	_, err := DecodeManifestHeader(manifestWithNestedInlineTables(tomlstrict.MaximumDepth))
	if !errors.Is(err, tomlstrict.ErrMaximumDepthExceeded) {
		t.Fatalf("DecodeManifestHeader error = %v, want depth exceeded", err)
	}
	if err == nil || !strings.HasPrefix(err.Error(), "parse manifest header:") {
		t.Fatalf("DecodeManifestHeader error = %v, want parse manifest header prefix", err)
	}
}

func TestManifestDecodersAcceptExactTOMLDepthBeforeSemanticValidation(t *testing.T) {
	content := manifestWithNestedInlineTables(tomlstrict.MaximumDepth - 1)
	if _, err := DecodeManifest(content); errors.Is(err, tomlstrict.ErrMaximumDepthExceeded) {
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
	builder.WriteString(strings.Repeat("a", tomlstrict.MaximumKeyBytes))
	builder.WriteString("]\n")
	for range siblings {
		builder.WriteString("k = 1\n")
	}
	return []byte(builder.String())
}
