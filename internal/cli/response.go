package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
)

type commandFailureClass uint8

const (
	operationFailure commandFailureClass = iota
	usageFailure
)

func (class commandFailureClass) exitCode() int {
	if class == usageFailure {
		return 2
	}
	return 1
}

func (class commandFailureClass) jsonKind() clipresent.CLIErrorKind {
	if class == usageFailure {
		return clipresent.CLIErrorUsage
	}
	return clipresent.CLIErrorOperation
}

type terminalResponse uint8

const (
	responseAbsent terminalResponse = iota
	responseOrdinary
	responseCommonFailure
)

var errTerminalResponseSelected = errors.New("a terminal response is already selected")

// commandResponse reserves result identity before projection or writing. Empty
// or failed delivery cannot make an ordinary result eligible for replacement.
type commandResponse struct {
	command string
	output  requestedOutput
	stdout  io.Writer
	stderr  io.Writer
	state   terminalResponse
}

func newCommandResponse(command string, output requestedOutput, stdout io.Writer, stderr io.Writer) *commandResponse {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &commandResponse{command: command, output: output, stdout: stdout, stderr: stderr}
}

func (response *commandResponse) ordinaryResult(render func() error) error {
	if err := response.selectOrdinaryResult(); err != nil {
		return err
	}
	return render()
}

func (response *commandResponse) selectOrdinaryResult() error {
	if response.state != responseAbsent {
		return errTerminalResponseSelected
	}
	response.state = responseOrdinary
	return nil
}

func (response *commandResponse) failure(class commandFailureClass, diagnostic clipresent.FailureDiagnostic) int {
	if response.output == requestedJSON && response.state == responseAbsent {
		response.state = responseCommonFailure
		if err := clipresent.PrintCLIErrorJSON(response.stdout, response.command, class.jsonKind(), diagnostic); err != nil {
			fmt.Fprintf(response.stderr, "%s failed: write json: %s\n", response.command, humanDiagnosticError(err))
		}
	} else {
		fmt.Fprintln(response.stderr, diagnostic.HumanText())
	}
	return class.exitCode()
}
