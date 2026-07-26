// Package clipresent renders CLI human, JSON, diff, progress, and exit projections.
package clipresent

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

var commandHookAdmissionMessages = map[profile.UnsupportedReason]string{
	profile.UnsupportedReasonDirectCLIRouteNotAdmitted: "hook %q target %q: add hook cannot author Antigravity CLI command hooks because direct CLI hooks are not TP04-07 admitted; %s",
}

type mcpEnvUnsupportedMessage struct {
	label        string
	adapterShape string
}

var mcpEnvUnsupportedMessages = map[aggregate.MCPPlacementID]mcpEnvUnsupportedMessage{
	aggregate.MCPPlacementClaudeProject:     {label: "Claude project MCP projection", adapterShape: "stdio"},
	aggregate.MCPPlacementClaudeGlobal:      {label: "Claude Code user/global MCP projection", adapterShape: "stdio"},
	aggregate.MCPPlacementAntigravityGlobal: {label: "Antigravity CLI MCP projection", adapterShape: "command/args"},
	aggregate.MCPPlacementOpenCodeProject:   {label: "OpenCode MCP projection", adapterShape: "local-command"},
	aggregate.MCPPlacementOpenCodeGlobal:    {label: "OpenCode global MCP projection", adapterShape: "local-command"},
	aggregate.MCPPlacementCodexProject:      {label: "Codex MCP projection", adapterShape: "command/args"},
	aggregate.MCPPlacementCodexGlobal:       {label: "Codex global MCP projection", adapterShape: "command/args"},
}

// Escape preserves printable text while making control, format, and invalid
// UTF-8 bytes visible instead of allowing them to affect terminal presentation.
func Escape(value string) string {
	var escaped strings.Builder
	for offset := 0; offset < len(value); {
		if value[offset] == '\\' {
			escaped.WriteString(`\\`)
			offset++
			continue
		}
		character, size := utf8.DecodeRuneInString(value[offset:])
		if character == utf8.RuneError && size == 1 {
			fmt.Fprintf(&escaped, `\x%02x`, value[offset])
			offset++
			continue
		}
		if unicode.IsControl(character) || !unicode.IsGraphic(character) {
			quoted := strconv.QuoteRuneToGraphic(character)
			escaped.WriteString(quoted[1 : len(quoted)-1])
		} else {
			escaped.WriteString(value[offset : offset+size])
		}
		offset += size
	}
	return escaped.String()
}

// Error renders err for human output without allowing its dynamic text to
// introduce terminal controls. A nil error renders as an empty string.
func Error(err error) string {
	if err == nil {
		return ""
	}
	var admissionFailure authoring.CommandHookAdmissionError
	if errors.As(err, &admissionFailure) {
		message, specialized := commandHookAdmissionMessages[admissionFailure.Reason()]
		if !specialized {
			return Escape(err.Error())
		}
		return Escape(fmt.Sprintf(
			message,
			admissionFailure.HookName(),
			admissionFailure.Target(),
			admissionFailure.Detail(),
		))
	}
	var envFailure aggregate.MCPEnvUnsupportedError
	if errors.As(err, &envFailure) {
		message, specialized := mcpEnvUnsupportedMessages[envFailure.PlacementID()]
		if !specialized {
			return Escape(err.Error())
		}
		return Escape(fmt.Sprintf(
			"%s does not support env in the admitted %s adapter",
			message.label,
			message.adapterShape,
		))
	}
	return Escape(err.Error())
}
