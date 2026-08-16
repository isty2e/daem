// Package clipresent renders CLI human, JSON, diff, progress, and exit projections.
package clipresent

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

var commandHookAdmissionMessages = map[profile.UnsupportedReason]string{
	profile.UnsupportedReasonDirectCLIRouteNotAdmitted: "hook %q target %q: add hook cannot author Antigravity CLI command hooks because direct CLI hooks are not TP04-07 admitted; %s",
}

var staticErrorEvidenceType = reflect.TypeOf(errors.New(""))

const maximumErrorEvidenceNodes = 64

const errorEvidenceTruncationMarker = "\n[truncated]"

type mcpEnvUnsupportedMessage struct {
	label        string
	adapterShape string
}

type boundedErrorEvidence interface {
	BoundedErrorEvidence(maximumRunes int) (string, bool)
}

var mcpEnvUnsupportedMessages = map[aggregate.MCPPlacementID]mcpEnvUnsupportedMessage{
	aggregate.MCPPlacementClaudeProject:     {label: "Claude project MCP projection", adapterShape: "stdio"},
	aggregate.MCPPlacementClaudeGlobal:      {label: "Claude Code user/global MCP projection", adapterShape: "stdio"},
	aggregate.MCPPlacementAntigravityGlobal: {label: "Antigravity CLI MCP projection", adapterShape: "command/args"},
	aggregate.MCPPlacementOpenCodeProject:   {label: "OpenCode MCP projection", adapterShape: "local-command"},
	aggregate.MCPPlacementOpenCodeGlobal:    {label: "OpenCode global MCP projection", adapterShape: "local-command"},
	aggregate.MCPPlacementCodexProject:      {label: "Codex MCP projection", adapterShape: "command/args"},
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

// Quote returns one display-safe quoted field without admitting terminal
// controls, format characters, or invalid UTF-8 bytes.
func Quote(value string) string {
	return strconv.QuoteToGraphic(value)
}

// BoundedErrorEvidence projects a bounded error cause tree into sanitized,
// display-safe evidence. Wrapping and joined nodes are traversed without
// materializing their aggregate Error strings.
func BoundedErrorEvidence(err error, maximumRunes int) string {
	if err == nil || maximumRunes <= 0 {
		return ""
	}
	maximumBytes := maximumRunes
	if maximumRunes <= int(^uint(0)>>1)/utf8.UTFMax {
		maximumBytes *= utf8.UTFMax
	}
	buffer := subprocess.NewBoundedBuffer(maximumBytes)
	stack := []error{err}
	leaves := 0
	nodes := 0
	omitted := false
	for len(stack) != 0 {
		if nodes == maximumErrorEvidenceNodes {
			omitted = true
			break
		}
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == nil {
			continue
		}
		nodes++
		if _, ok := current.(boundedErrorEvidence); ok {
			wrote, truncated := writeErrorEvidenceLeaf(
				buffer,
				current,
				maximumRunes,
				&leaves,
			)
			if !wrote || truncated {
				omitted = true
			}
			if buffer.Truncated() {
				omitted = true
				break
			}
			continue
		}

		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			children := joined.Unwrap()
			if len(children) == 0 {
				wrote, truncated := writeErrorEvidenceLeaf(
					buffer,
					current,
					maximumRunes,
					&leaves,
				)
				if !wrote {
					omitted = true
				}
				if truncated {
					omitted = true
				}
				if buffer.Truncated() {
					omitted = true
					break
				}
				continue
			}
			remainingNodes := maximumErrorEvidenceNodes - nodes
			if len(children) > remainingNodes {
				children = children[:remainingNodes]
				omitted = true
			}
			for index := len(children) - 1; index >= 0; index-- {
				stack = append(stack, children[index])
			}
			continue
		}
		if wrapped, ok := current.(interface{ Unwrap() error }); ok {
			if child := wrapped.Unwrap(); child != nil {
				stack = append(stack, child)
				continue
			}
		}

		wrote, truncated := writeErrorEvidenceLeaf(
			buffer,
			current,
			maximumRunes,
			&leaves,
		)
		if !wrote {
			omitted = true
		}
		if truncated {
			omitted = true
		}
		if buffer.Truncated() {
			omitted = true
			break
		}
	}
	if len(stack) != 0 {
		omitted = true
	}
	if leaves == 0 && omitted {
		_, _ = io.WriteString(buffer, "error evidence omitted")
	}
	upstreamTruncated := buffer.Truncated() || omitted
	result := subprocess.NewCapturePolicy(nil, maximumRunes).
		SanitizeUsing(buffer.String(), upstreamTruncated, nil, func(text string) string {
			if upstreamTruncated && !strings.HasSuffix(text, errorEvidenceTruncationMarker) {
				return text + errorEvidenceTruncationMarker
			}
			return text
		})
	return result.Text()
}

func writeErrorEvidenceLeaf(
	buffer *subprocess.BoundedBuffer,
	err error,
	maximumRunes int,
	leaves *int,
) (bool, bool) {
	projector, ok := err.(boundedErrorEvidence)
	if ok {
		evidence, truncated := projector.BoundedErrorEvidence(maximumRunes)
		if evidence == "" {
			return false, truncated
		}
		if *leaves != 0 {
			_, _ = io.WriteString(buffer, "\n")
		}
		_, _ = io.WriteString(buffer, evidence)
		(*leaves)++
		return true, truncated
	}

	staticEvidence := reflect.TypeOf(err) == staticErrorEvidenceType
	if _, ok := err.(syscall.Errno); ok {
		staticEvidence = true
	}
	if !staticEvidence {
		return false, false
	}
	if *leaves != 0 {
		_, _ = io.WriteString(buffer, "\n")
	}
	_, _ = io.WriteString(buffer, err.Error())
	(*leaves)++
	return true, false
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
	var envFailure aggregate.MCPEnvReferenceAdmissionError
	if errors.As(err, &envFailure) {
		if envFailure.Mapping() != aggregate.MCPEnvMappingUnsupported {
			return Escape(err.Error())
		}
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
