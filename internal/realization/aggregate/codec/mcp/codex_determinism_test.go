package mcpcodec

import (
	"errors"
	"strings"
	"testing"
)

func TestCodexGlobalValidationUsesDeterministicReasonPrecedence(t *testing.T) {
	value := map[string]any{
		"command": "npx",
		"env":     map[string]any{"TOKEN": "SECRET_CANARY"},
		"url":     "https://example.invalid",
	}
	for range 256 {
		_, err := decodeCodexGlobalMCPServerEntryValue(value, "context7")
		var projectionErr *MCPProjectionError
		if !errors.As(err, &projectionErr) ||
			projectionErr.Code() != MCPProjectionReasonSecretLiteralForbidden ||
			!strings.HasSuffix(projectionErr.Subject(), "/env") {
			t.Fatalf("global diagnostic = %v, want deterministic secret-literal /env", err)
		}
	}
}

func TestCodexValidationSelectsLexicographicallyFirstUnsupportedField(t *testing.T) {
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "project entry",
			call: func() error {
				_, err := decodeCodexProjectMCPServerEntryValue(map[string]any{
					"command": "npx",
					"alpha":   true,
					"zeta":    true,
				}, "context7")
				return err
			},
		},
		{
			name: "global entry",
			call: func() error {
				_, err := decodeCodexGlobalMCPServerEntryValue(map[string]any{
					"command": "npx",
					"alpha":   true,
					"zeta":    true,
				}, "context7")
				return err
			},
		},
		{
			name: "nested env object",
			call: func() error {
				_, err := codexGlobalMCPEnvVarName(map[string]any{
					"name":   "TOKEN",
					"source": "local",
					"alpha":  true,
					"zeta":   true,
				}, "/mcp_servers/context7/env_vars[0]")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for range 256 {
				err := test.call()
				var projectionErr *MCPProjectionError
				if !errors.As(err, &projectionErr) ||
					projectionErr.Code() != MCPProjectionReasonUnsupportedManagedField ||
					!strings.HasSuffix(projectionErr.Subject(), "/alpha") {
					t.Fatalf("diagnostic = %v, want lexicographically first /alpha", err)
				}
			}
		})
	}
}
