package host

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
)

func TestDecodeNormalDocumentUsesNullishAliasSemantics(t *testing.T) {
	fallback, err := decodeNormalDocument([]byte(
		`{"mcpServers":null,"mcp-servers":{"context7":{"command":"node"}}}`,
	))
	if err != nil {
		t.Fatalf("decode nullish fallback: %v", err)
	}
	if _, present := fallback.serverNames["context7"]; !present {
		t.Fatal("null primary alias did not select the fallback table")
	}

	if _, err := decodeNormalDocument([]byte(
		`{"mcpServers":"invalid","mcp-servers":{"context7":{"command":"node"}}}`,
	)); err == nil {
		t.Fatal("non-null invalid primary alias fell through to the fallback table")
	}
}

func TestDecodeImportsRejectsSchemaIncompatibleServerTables(t *testing.T) {
	tests := []struct {
		name   string
		decode func() error
	}{
		{
			name: "normal scalar root",
			decode: func() error {
				_, err := decodeNormalDocument([]byte(`"invalid"`))
				return err
			},
		},
		{
			name: "Codex scalar primary",
			decode: func() error {
				_, err := decodeCodexImportServerNames(
					[]byte(`{"mcp_servers":"invalid","mcpServers":{"context7":{}}}`),
					false,
				)
				return err
			},
		},
		{
			name: "OpenCode scalar table",
			decode: func() error {
				_, err := decodeOpenCodeConfig([]byte(`{"mcp":"invalid"}`))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode(); err == nil {
				t.Fatal("schema-incompatible config decoded as exact evidence")
			}
		})
	}
}

func TestDecodeCodexImportUsesFallbackOnlyForNullishPrimary(t *testing.T) {
	names, err := decodeCodexImportServerNames(
		[]byte(`{"mcp_servers":null,"mcpServers":{"context7":{}}}`),
		false,
	)
	if err != nil {
		t.Fatalf("decode Codex nullish fallback: %v", err)
	}
	if _, present := names["context7"]; !present {
		t.Fatal("Codex null primary alias did not select the fallback table")
	}
}

func TestDecodeCodexImportRejectsExcessiveTOMLStructure(t *testing.T) {
	content := []byte("root = " + strings.Repeat("{ k = ", tomlstrict.MaximumDepth) +
		"1" + strings.Repeat(" }", tomlstrict.MaximumDepth) + "\n")

	_, err := decodeCodexImportServerNames(content, true)
	if !errors.Is(err, tomlstrict.ErrMaximumDepthExceeded) {
		t.Fatalf("decode Codex TOML import = %v, want depth rejection", err)
	}
}
