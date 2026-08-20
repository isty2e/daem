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
	}{
		{
			name: "host document",
			decode: func() error {
				_, err := decodeCodexProjectMCPConfig(content)
				return err
			},
		},
		{
			name: "project canonical entry",
			decode: func() error {
				_, err := decodeCodexProjectMCPServerEntry(content, "context7")
				return err
			},
		},
		{
			name: "global canonical entry",
			decode: func() error {
				_, err := decodeCodexGlobalMCPServerEntry(content, "context7")
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.decode()
			reason, ok := MCPProjectionReasonCodeOf(err)
			if !ok || reason != MCPProjectionReasonConfigMalformed ||
				!strings.Contains(err.Error(), tomlstrict.ErrMaximumDepthExceeded.Error()) {
				t.Fatalf("decode error = %v, reason = %q, want config-malformed depth rejection", err, reason)
			}
		})
	}
}

func TestCodexTOMLDecoderRejectsContentBeyondDocumentLimit(t *testing.T) {
	content := []byte("command = \"")
	content = append(content, make([]byte, maximumDocumentBytes)...)
	content = append(content, '"', '\n')

	_, err := decodeCodexProjectMCPServerEntry(content, "context7")
	if err == nil || !strings.Contains(err.Error(), "exceeds 4194304 bytes") {
		t.Fatalf("decode oversized canonical entry = %v, want document byte-limit rejection", err)
	}
}

func deeplyNestedCodexTOML(depth int) []byte {
	return []byte("command = " + strings.Repeat("{ k = ", depth) +
		`"npx"` + strings.Repeat(" }", depth) + "\n")
}
