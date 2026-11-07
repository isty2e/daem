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
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

var commandHookAdmissionMessages = map[profile.UnsupportedReason]string{
	profile.UnsupportedReasonDirectCLIRouteNotAdmitted: "hook %q target %q: add hook cannot author Antigravity CLI command hooks because direct CLI hooks are not TP04-07 admitted; %s",
}

var staticErrorEvidenceType = reflect.TypeOf(errors.New(""))

const maximumErrorEvidenceNodes = 64

const maximumHumanErrorDetailRunes = 4096

const errorEvidenceRedactionMarker = "[REDACTED]"

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

func humanErrorDetail(value string) string {
	maximumRawBytes := maximumHumanErrorDetailRunes * utf8.UTFMax
	if len(value) > maximumRawBytes {
		return errorEvidenceRedactionMarker + errorEvidenceTruncationMarker
	}

	return subprocess.NewCapturePolicy(nil, maximumHumanErrorDetailRunes).
		Sanitize(Escape(value), false).Text()
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
	incompleteText := false
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
			incompleteText = incompleteText || wrote && truncated
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
				incompleteText = incompleteText || wrote && truncated
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
		incompleteText = incompleteText || wrote && truncated
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
	// A cut string can hide a credential delimiter beyond the buffer. Omitting
	// whole nodes does not make the retained observations incomplete.
	if buffer.Truncated() || incompleteText {
		buffer = subprocess.NewBoundedBuffer(maximumBytes)
		_, _ = io.WriteString(buffer, errorEvidenceRedactionMarker)
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

// Error renders bounded, credential-redacted human error detail. Dynamic controls
// are escaped; only presenter-owned annotations add structural lines.
// A nil error renders as an empty string.
func Error(err error) string {
	return FailureDiagnosticFromError(err).HumanText()
}

// FailureDiagnosticFromError preserves the general human error projection.
// Callers with stricter failure contracts must supply their own owned summary.
func FailureDiagnosticFromError(err error) FailureDiagnostic {
	if err == nil {
		return FailureDiagnostic{}
	}
	if operation, ok := err.(authoring.OperationError); ok && operation.Phase == authoring.OperationPhaseBuildLockfile && operation.Err != nil {
		return FailureDiagnosticFromError(operation.Err)
	}
	var admissionFailure authoring.CommandHookAdmissionError
	if errors.As(err, &admissionFailure) {
		message, specialized := commandHookAdmissionMessages[admissionFailure.Reason()]
		if !specialized {
			return NewFailureDiagnostic(err.Error())
		}
		return NewFailureDiagnostic(fmt.Sprintf(
			message,
			admissionFailure.HookName(),
			admissionFailure.Target(),
			admissionFailure.Detail(),
		))
	}
	var envFailure aggregate.MCPEnvReferenceAdmissionError
	if errors.As(err, &envFailure) {
		if envFailure.Mapping() != aggregate.MCPEnvMappingUnsupported {
			return NewFailureDiagnostic(err.Error())
		}
		message, specialized := mcpEnvUnsupportedMessages[envFailure.PlacementID()]
		if !specialized {
			return NewFailureDiagnostic(err.Error())
		}
		return NewFailureDiagnostic(fmt.Sprintf(
			"%s does not support env in the admitted %s adapter",
			message.label,
			message.adapterShape,
		))
	}
	if diagnostic, recognized := RecognizedInputDiagnostic(err); recognized {
		return diagnostic
	}
	if guidance, present := DeclarationReplacementGuidance(err); present {
		return NewFailureDiagnostic(err.Error()).WithGuidance(guidance)
	}
	return NewFailureDiagnostic(err.Error())
}

// NativePlanningFailureDiagnostic is called only by an effect-free planning
// boundary. Generic error rendering must not promote this cause through a
// recovery or execution wrapper that owns a stronger lifecycle summary.
func NativePlanningFailureDiagnostic(err error) (FailureDiagnostic, bool) {
	var protection configuration.NativeProtectionFailure
	if errors.As(err, &protection) {
		return NativeProtectionPlanningDiagnostic(protection), true
	}
	var claim configuration.NativeClaimFailure
	if errors.As(err, &claim) {
		return NativeClaimPlanningDiagnostic(claim), true
	}
	return FailureDiagnostic{}, false
}

func NativeProtectionPlanningDiagnostic(failure configuration.NativeProtectionFailure) FailureDiagnostic {
	if failure == nil {
		return FailureDiagnostic{}
	}
	diagnostic := NewFailureDiagnostic(failure.Error())
	if failure.Kind() == configuration.ProtectionReserved {
		return diagnostic.WithGuidance(InstructionGuidance(fmt.Sprintf("check this document's links and the reserved %s endpoint; point the document at an ordinary configuration file outside reserved data, or remove the declaration", failure.Host())))
	}
	return diagnostic.WithGuidance(InstructionGuidance(nativeProtectionVerificationRepair(failure) + ", then retry"))
}

func NativeClaimPlanningDiagnostic(failure configuration.NativeClaimFailure) FailureDiagnostic {
	diagnostic := NewFailureDiagnostic(failure.Error())
	if failure.Kind() == configuration.NativeClaimScopeTransfer {
		return diagnostic.WithGuidance(InstructionGuidance("first remove the old declaration and apply to retire its management (this removes its managed values), then add the declaration at the new scope and apply again. The --manage-existing option does not transfer an existing claim between scopes"))
	}
	if failure.Kind() == configuration.NativeClaimPhysicalRole {
		diagnostic = diagnostic.WithGuidance(InstructionGuidance("manage only ordinary configuration keys with key ownership; use the dedicated resource for reserved entries"))
	}
	if _, global := failure.GlobalLocation(); global && failure.Address().Scope() == target.ScopeProject {
		return diagnostic.WithGuidance(InstructionGuidance("use a project file outside global host configuration. To move existing project management to global scope, first remove the project declaration and apply to retire it (this removes its managed values), then add the global declaration with its host-native path and apply again; changing scope in one apply does not transfer the existing claim"))
	}
	return diagnostic
}

func NativeProtectionRecoveryGuidance(failure configuration.NativeProtectionFailure) Guidance {
	if failure.Kind() == configuration.ProtectionReserved {
		return InstructionGuidance("preserve the journal and recovery artifacts; use daem recover --dry-run --verbose to inspect the recorded path and repair any unintended link mapping. Changing the manifest declaration does not change recorded aliases; an intentional reserved-data overlap cannot be recovered automatically")
	}
	return InstructionGuidance(nativeProtectionVerificationRepair(failure) + ", then rerun daem recover --dry-run; preserve the journal and recovery artifacts")
}

func nativeProtectionVerificationRepair(failure configuration.NativeProtectionFailure) string {
	switch failure.Uncertainty() {
	case configuration.ProtectionDocumentIndeterminate, configuration.ProtectionRegionIndeterminate:
		return "use a location that already exists, or choose an ordinary configuration path whose filesystem spelling can be verified"
	case configuration.ProtectionDocumentUnavailable:
		return "check this document's parent directories and links"
	case configuration.ProtectionEndpointUnavailable:
		return fmt.Sprintf("check access to the %s %q endpoint and repair its links", failure.Host(), failure.Endpoint())
	case configuration.ProtectionSelectorMissing:
		return "restore the required native protection-location information; if it remains missing, report this error"
	case configuration.ProtectionAllocationUnavailable:
		return "retry the command with its normal operation context; if the shared filesystem-work allocation remains unavailable, report this error"
	case configuration.ProtectionWorkLimit:
		return "check the host locations for unexpectedly large directories or excessive link chains; retry after reducing the required filesystem work"
	}
	location := configuration.NativeRootDescription(failure.Reference())
	if source := failure.Source(); source != "" && source != "default location" && source != "selected location" {
		return fmt.Sprintf("check that %s selects an accessible %s and that its links resolve", source, location)
	}
	return "check access to the " + location + " and its links"
}
