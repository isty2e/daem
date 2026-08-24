package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/buildidentity"
	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/platformsupport"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	platformworkflow "github.com/isty2e/daem/internal/workflow/platform"
	probeworkflow "github.com/isty2e/daem/internal/workflow/probe"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

// RunOptions describes one CLI invocation, including process-boundary capabilities and effect dependencies.
type RunOptions struct {
	Context               context.Context
	Stdin                 io.Reader
	Stdout                io.Writer
	Stderr                io.Writer
	StdinIsTerminal       bool
	StdoutIsTerminal      bool
	StderrIsTerminal      bool
	ReadConfirmationLine  func(context.Context, io.Reader, int) (string, error)
	ApplyExecuteOptions   applyworkflow.ExecuteOptions
	ProbeExecutor         probeworkflow.RuntimeProbeExecutor
	RecoverExecuteOptions recoverworkflow.ExecuteOptions
	RefreshPlanOptions    refreshworkflow.PlanOptions
	RefreshExecuteOptions refreshworkflow.ExecuteOptions
	PlatformObserver      platformworkflow.RuntimeObserver
	HelpWidth             int
}

type commandOptions struct {
	context               context.Context
	stderrIsTerminal      bool
	confirmation          confirmationBoundary
	applyExecuteOptions   applyworkflow.ExecuteOptions
	probeExecutor         probeworkflow.RuntimeProbeExecutor
	recoverExecuteOptions recoverworkflow.ExecuteOptions
	refreshPlanOptions    refreshworkflow.PlanOptions
	refreshExecuteOptions refreshworkflow.ExecuteOptions
	platformAdmission     platformsupport.Admission
	platformObserver      platformworkflow.RuntimeObserver
	buildIdentity         buildidentity.Identity
}

type platformAdmissionScope uint8

const (
	platformAdmissionBypass platformAdmissionScope = iota
	platformAdmissionWholeCommand
	platformAdmissionSelectedSubject
)

type commandPlatformAdmission struct {
	scope    platformAdmissionScope
	subjects []string
}

// commandAdmissionCatalog is the ingress authority for public root-command
// recognition and pre-dispatch platform admission.
var commandAdmissionCatalog = map[string]commandPlatformAdmission{
	"--help":    {},
	"--version": {},
	"-h":        {},
	"add": {
		scope: platformAdmissionSelectedSubject,
		subjects: []string{
			"extension", "instruction", "hook", "mcp-server", "skill", "skill-group",
		},
	},
	"apply":    {scope: platformAdmissionWholeCommand},
	"doctor":   {},
	"help":     {},
	"import":   {scope: platformAdmissionWholeCommand},
	"init":     {scope: platformAdmissionWholeCommand},
	"list":     {},
	"lock":     {scope: platformAdmissionWholeCommand},
	"outdated": {scope: platformAdmissionWholeCommand},
	"probe":    {},
	"recover":  {scope: platformAdmissionWholeCommand},
	"refresh":  {scope: platformAdmissionWholeCommand},
	"remove": {
		scope:    platformAdmissionSelectedSubject,
		subjects: []string{"extension", "instruction", "hook", "mcp-server", "skill"},
	},
	"status": {},
	"unmanage": {
		scope:    platformAdmissionSelectedSubject,
		subjects: []string{"extension"},
	},
	"version": {},
}

type stableOutputWriter struct {
	output          io.Writer
	err             error
	failureReported bool
}

type stableOutputWriteError struct {
	writer *stableOutputWriter
}

func (*stableOutputWriteError) Error() string {
	return "command output could not be written"
}

func (writer *stableOutputWriter) failure() error {
	if writer == nil || writer.err == nil {
		return nil
	}
	return &stableOutputWriteError{writer: writer}
}

func (writer *stableOutputWriter) Write(content []byte) (int, error) {
	if writer.err != nil {
		return 0, writer.failure()
	}
	count, err := writer.output.Write(content)
	if err == nil && count != len(content) {
		err = io.ErrShortWrite
	}
	if err != nil {
		writer.err = err
		return count, writer.failure()
	}
	return count, nil
}

// RunWithOptions executes the CLI with explicit process streams, terminal facts, and confirmation-read capability.
func RunWithOptions(args []string, options RunOptions) int {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	providedStdout := options.Stdout
	stdout := providedStdout
	var stableOutput *stableOutputWriter
	if stdout != nil {
		stableOutput = &stableOutputWriter{output: stdout}
		stdout = stableOutput
	} else {
		stdout = io.Discard
	}
	providedStderr := options.Stderr
	stderr := providedStderr
	if stderr == nil {
		stderr = io.Discard
	}
	commandInvocation := commandOptions{
		context:          ctx,
		stderrIsTerminal: providedStderr != nil && options.StderrIsTerminal,
		confirmation: confirmationBoundary{
			context:              ctx,
			input:                options.Stdin,
			promptOutput:         stderr,
			readLine:             options.ReadConfirmationLine,
			inputIsTerminal:      options.Stdin != nil && options.StdinIsTerminal,
			disclosureIsTerminal: providedStdout != nil && options.StdoutIsTerminal,
			promptIsTerminal:     providedStderr != nil && options.StderrIsTerminal,
			disclosureError: func() error {
				return stableOutput.failure()
			},
		},
		applyExecuteOptions:   options.ApplyExecuteOptions,
		probeExecutor:         options.ProbeExecutor,
		recoverExecuteOptions: options.RecoverExecuteOptions,
		refreshPlanOptions:    options.RefreshPlanOptions,
		refreshExecuteOptions: options.RefreshExecuteOptions,
		platformAdmission:     platformsupport.Current(),
		platformObserver:      options.PlatformObserver,
		buildIdentity:         buildidentity.Current(),
	}

	if len(args) == 0 {
		printUsage(stderr, options.HelpWidth)
		return 2
	}

	exitCode := runCommand(args, stdout, stderr, options, commandInvocation)
	if stableOutput != nil && stableOutput.err != nil && !stableOutput.failureReported {
		fmt.Fprintln(stderr, "output failed: command output could not be written")
	}
	if stableOutput != nil && stableOutput.err != nil && exitCode == 0 {
		return 1
	}
	return exitCode
}

func markOutputFailureReported(output io.Writer) {
	if stable, ok := output.(*stableOutputWriter); ok && stable.err != nil {
		stable.failureReported = true
	}
}

func runCommand(args []string, stdout io.Writer, stderr io.Writer, options RunOptions, commandInvocation commandOptions) int {
	if _, recognized := commandAdmissionCatalog[args[0]]; !recognized {
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		fmt.Fprintln(stderr, "next: run daem help")
		return 2
	}
	if exitCode, rejected := rejectUnsupportedPlatform(args, commandInvocation, stderr); rejected {
		return exitCode
	}

	switch args[0] {
	case "add":
		return runAdd(commandInvocation.context, args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr, commandInvocation)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr, commandInvocation)
	case "import":
		return runImport(args[1:], stdout, stderr, commandInvocation)
	case "init":
		return runInit(commandInvocation.context, args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout, stderr, commandInvocation)
	case "lock":
		return runLock(args[1:], stdout, stderr, commandInvocation)
	case "outdated":
		return runOutdated(args[1:], stdout, stderr, commandInvocation)
	case "probe":
		return runProbe(args[1:], stdout, stderr, commandInvocation)
	case "recover":
		return runRecover(args[1:], stdout, stderr, commandInvocation)
	case "refresh":
		return runRefresh(args[1:], stdout, stderr, commandInvocation)
	case "remove":
		return runRemove(commandInvocation.context, args[1:], stdout, stderr)
	case "status":
		return runStatus(args[1:], stdout, stderr, commandInvocation)
	case "unmanage":
		return runUnmanage(commandInvocation.context, args[1:], stdout, stderr)
	case "version":
		return runVersion(args[1:], stdout, stderr, options, commandInvocation)
	case "--version":
		return runVersionAlias(args[1:], stdout, stderr, commandInvocation)
	case "help", "-h", "--help":
		if args[0] == "help" && len(args) > 1 {
			if printCommandUsage(args[1:], stdout, options.HelpWidth) {
				return 0
			}
			fmt.Fprintf(stderr, "unknown help topic %q\n", strings.Join(args[1:], " "))
			printNearestHelpHint(stderr, args[1:])
			return 2
		}
		if len(args) > 1 {
			fmt.Fprintf(stderr, "unexpected argument %q\n", args[1])
			return 2
		}
		printUsage(stdout, options.HelpWidth)
		return 0
	default:
		fmt.Fprintf(stderr, "command %q is registered without an implementation\n", args[0])
		return 1
	}
}

func runVersion(args []string, stdout io.Writer, stderr io.Writer, options RunOptions, commandInvocation commandOptions) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		printCommandUsage([]string{"version"}, stdout, options.HelpWidth)
		return 0
	}

	jsonOutput := false
	if len(args) > 0 {
		if args[0] != "--json" {
			fmt.Fprintf(stderr, "unexpected argument %q\n", args[0])
			return 2
		}
		jsonOutput = true
	}
	if len(args) > 1 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", args[1])
		return 2
	}

	var err error
	if jsonOutput {
		err = clipresent.PrintVersionJSON(stdout, commandInvocation.buildIdentity)
	} else {
		err = clipresent.PrintVersion(stdout, commandInvocation.buildIdentity)
	}
	if err != nil {
		fmt.Fprintf(stderr, "version failed: %s\n", humanDiagnosticError(err))
		return 1
	}
	return 0
}

func runVersionAlias(args []string, stdout io.Writer, stderr io.Writer, commandInvocation commandOptions) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", args[0])
		return 2
	}
	if err := clipresent.PrintVersion(stdout, commandInvocation.buildIdentity); err != nil {
		fmt.Fprintf(stderr, "version failed: %s\n", humanDiagnosticError(err))
		return 1
	}
	return 0
}

func rejectUnsupportedPlatform(args []string, options commandOptions, stderr io.Writer) (int, bool) {
	if !requiresPlatformAdmission(args) || commandInvocationRequestsHelp(args[1:]) {
		return 0, false
	}
	assessment, err := options.assessPlatform()
	if err != nil {
		fmt.Fprintf(stderr, "%s failed: observe platform runtime: %s\n", humanDiagnosticText(args[0]), humanDiagnosticError(err))
		return 1, true
	}
	if err := assessment.RequireSupported(); err != nil {
		fmt.Fprintf(stderr, "%s failed: %s\n", humanDiagnosticText(args[0]), humanDiagnosticError(err))
		fmt.Fprintln(stderr, "next: run daem doctor")
		return 1, true
	}
	return 0, false
}

func (options commandOptions) assessPlatform() (platformsupport.PlatformAssessment, error) {
	return platformworkflow.Assess(options.context, options.platformAdmission, options.platformObserver)
}

func humanDiagnosticText(value string) string {
	return clipresent.Escape(value)
}

func humanDiagnosticError(err error) string {
	var outputFailure *stableOutputWriteError
	if errors.As(err, &outputFailure) {
		if outputFailure.writer != nil && outputFailure.writer.err != nil {
			outputFailure.writer.failureReported = true
		}
		return outputFailure.Error()
	}
	return clipresent.Error(err)
}

func requiresPlatformAdmission(args []string) bool {
	if len(args) == 0 {
		return false
	}
	policy, recognized := commandAdmissionCatalog[args[0]]
	if !recognized {
		return false
	}
	return policy.requires(args[1:])
}

func (policy commandPlatformAdmission) requires(args []string) bool {
	switch policy.scope {
	case platformAdmissionWholeCommand:
		return true
	case platformAdmissionSelectedSubject:
		return len(args) > 0 && slices.Contains(policy.subjects, args[0])
	default:
		return false
	}
}

func commandInvocationRequestsHelp(args []string) bool {
	return commandHelpRequested(args) || (len(args) > 0 && args[0] == "help")
}

func printNearestHelpHint(output io.Writer, path []string) {
	for length := len(path) - 1; length > 0; length-- {
		if _, ok := helpTopic(path[:length]); ok {
			fmt.Fprintf(output, "next: run daem help %s\n", strings.Join(path[:length], " "))
			return
		}
	}
	fmt.Fprintln(output, "next: run daem help")
}

func helpTopic(path []string) ([]string, bool) {
	var sink strings.Builder
	if printCommandUsage(path, &sink, 80) {
		return path, true
	}
	return nil, false
}
