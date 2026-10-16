package clipresent

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// GuidanceKind distinguishes context, manual work and a concrete command.
type GuidanceKind string

const (
	GuidanceNote        GuidanceKind = "note"
	GuidanceInstruction GuidanceKind = "instruction"
	GuidanceCommand     GuidanceKind = "command"
)

// Guidance retains safe text or exact command arguments without shell parsing.
type Guidance struct {
	kind        GuidanceKind
	message     string
	argv        []string
	humanPrefix string
}

func NoteGuidance(message string) Guidance {
	return textGuidance(GuidanceNote, "note: ", message)
}

func InstructionGuidance(message string) Guidance {
	return textGuidance(GuidanceInstruction, "next: ", message)
}

// BoundedEvidenceGuidance retains the already sanitized causal view without
// escaping or truncating its display projection a second time.
func BoundedEvidenceGuidance(err error, maximumRunes int, prefix string) Guidance {
	evidence := BoundedErrorEvidence(err, maximumRunes)
	if evidence == "" {
		return Guidance{}
	}
	return Guidance{kind: GuidanceNote, message: Quote(evidence), humanPrefix: humanErrorDetail(prefix)}
}

func textGuidance(kind GuidanceKind, prefix string, message string) Guidance {
	if message == "" {
		return Guidance{}
	}
	return Guidance{kind: kind, message: humanErrorDetail(message), humanPrefix: prefix}
}

// CommandGuidance admits a complete producer-selected invocation. It does not
// validate workflow prerequisites or grant permission to run the command.
func CommandGuidance(argv []string, purpose string) (Guidance, error) {
	if len(argv) == 0 || argv[0] != "daem" {
		return Guidance{}, fmt.Errorf("command guidance requires a daem invocation")
	}
	for _, argument := range argv {
		if !utf8.ValidString(argument) || strings.ContainsRune(argument, '\x00') {
			return Guidance{}, fmt.Errorf("command guidance arguments cannot round-trip through JSON")
		}
	}

	return Guidance{
		kind:        GuidanceCommand,
		message:     humanErrorDetail(purpose),
		argv:        slices.Clone(argv),
		humanPrefix: "next: run ",
	}, nil
}

func (guidance Guidance) Kind() GuidanceKind { return guidance.kind }

func (guidance Guidance) Message() string { return guidance.message }

func (guidance Guidance) Argv() []string { return slices.Clone(guidance.argv) }

// WithHumanPrefix changes only the human annotation, not its wire meaning.
func (guidance Guidance) WithHumanPrefix(prefix string) Guidance {
	guidance.humanPrefix = humanErrorDetail(prefix)
	return guidance
}

func (guidance Guidance) HumanText() string {
	if guidance.kind != GuidanceCommand {
		return guidance.humanPrefix + guidance.message
	}
	command, _ := ShellCommand(guidance.argv...)
	if guidance.message != "" {
		command += " " + guidance.message
	}
	return guidance.humanPrefix + command
}

// FailureDiagnostic separates the primary cause from presenter-owned annotations.
// Construction projects text once; neither renderer interprets error prose.
type FailureDiagnostic struct {
	message  string
	guidance []Guidance
}

func NewFailureDiagnostic(message string) FailureDiagnostic {
	return FailureDiagnostic{message: humanErrorDetail(message)}
}

func (diagnostic FailureDiagnostic) Message() string { return diagnostic.message }

// WithPrefix projects the caller's context without escaping the retained cause
// or its guidance a second time.
func (diagnostic FailureDiagnostic) WithPrefix(prefix string) FailureDiagnostic {
	diagnostic.message = humanErrorDetail(prefix) + diagnostic.message
	return diagnostic
}

func (diagnostic FailureDiagnostic) Guidance() []Guidance {
	result := slices.Clone(diagnostic.guidance)
	for index := range result {
		result[index].argv = slices.Clone(result[index].argv)
	}
	return result
}

func (diagnostic FailureDiagnostic) WithGuidance(values ...Guidance) FailureDiagnostic {
	diagnostic.guidance = slices.Clone(diagnostic.guidance)
	for _, guidance := range values {
		if guidance.kind != "" {
			diagnostic.guidance = append(diagnostic.guidance, guidance)
		}
	}
	return diagnostic
}

func (diagnostic FailureDiagnostic) HumanText() string {
	var output strings.Builder
	output.WriteString(diagnostic.message)
	for _, guidance := range diagnostic.guidance {
		output.WriteByte('\n')
		output.WriteString(guidance.HumanText())
	}
	return output.String()
}
