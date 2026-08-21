package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
)

func TestCodexTOMLDecodersRejectExcessiveStructure(t *testing.T) {
	content := deeplyNestedCodexTOML(tomlstrict.MaximumDepth)
	tests := []struct {
		name   string
		decode func() error
		want   MCPProjectionReasonCode
	}{
		{
			name: "host document",
			decode: func() error {
				_, err := decodeCodexProjectMCPConfig(content)
				return err
			},
			want: MCPProjectionReasonConfigMalformed,
		},
		{
			name: "project canonical entry",
			decode: func() error {
				_, err := decodeCodexProjectMCPServerEntry(content, "context7")
				return err
			},
			want: MCPProjectionReasonCode("CANONICAL_INVALID"),
		},
		{
			name: "global canonical entry",
			decode: func() error {
				_, err := decodeCodexGlobalMCPServerEntry(content, "context7")
				return err
			},
			want: MCPProjectionReasonCode("CANONICAL_INVALID"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.decode()
			reason, ok := MCPProjectionReasonCodeOf(err)
			if !ok || reason != test.want ||
				!strings.Contains(err.Error(), tomlstrict.ErrMaximumDepthExceeded.Error()) {
				t.Fatalf("decode error = %v, reason = %q, want %q depth rejection", err, reason, test.want)
			}
		})
	}
}

func TestCodexTOMLDecoderRejectsContentBeyondDocumentLimit(t *testing.T) {
	content := []byte("command = \"")
	content = append(content, make([]byte, maximumDocumentBytes)...)
	content = append(content, '"', '\n')

	_, err := decodeCodexProjectMCPServerEntry(content, "context7")
	reason, ok := MCPProjectionReasonCodeOf(err)
	if !ok || reason != MCPProjectionReasonCode("CANONICAL_INVALID") ||
		!strings.Contains(err.Error(), "exceeds 4194304 bytes") {
		t.Fatalf("decode oversized canonical entry = %v, reason = %q, want canonical byte-limit rejection", err, reason)
	}
}

func deeplyNestedCodexTOML(depth int) []byte {
	return []byte("command = " + strings.Repeat("{ k = ", depth) +
		`"npx"` + strings.Repeat(" }", depth) + "\n")
}
