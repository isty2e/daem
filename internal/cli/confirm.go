package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maximumConfirmationAnswerBytes = 4096

var errInteractiveConfirmationUnavailable = errors.New("interactive confirmation requires terminal stdin, terminal stdout disclosure, and terminal stderr prompt")

type confirmationBoundary struct {
	context              context.Context
	input                io.Reader
	promptOutput         io.Writer
	readLine             func(context.Context, io.Reader, int) (string, error)
	inputIsTerminal      bool
	disclosureIsTerminal bool
	promptIsTerminal     bool
	disclosureError      func() error
}

func (boundary confirmationBoundary) allowsInteractiveAuthorization() bool {
	return boundary.input != nil && boundary.promptOutput != nil && boundary.readLine != nil &&
		boundary.inputIsTerminal &&
		boundary.disclosureIsTerminal &&
		boundary.promptIsTerminal
}

func (boundary confirmationBoundary) prompt(action string) (bool, error) {
	if !boundary.allowsInteractiveAuthorization() {
		return false, errInteractiveConfirmationUnavailable
	}
	ctx := boundary.context
	if ctx == nil {
		return false, fmt.Errorf("confirmation context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if boundary.disclosureError != nil {
		if err := boundary.disclosureError(); err != nil {
			return false, fmt.Errorf("disclose plan: %w", err)
		}
	}
	if err := writeConfirmationText(boundary.promptOutput, fmt.Sprintf("Proceed with %s? [y/N]: ", action)); err != nil {
		return false, fmt.Errorf("write prompt: %w", err)
	}

	answer, err := boundary.readLine(ctx, boundary.input, maximumConfirmationAnswerBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		_, _ = fmt.Fprintln(boundary.promptOutput)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_, _ = fmt.Fprintln(boundary.promptOutput)
		return false, err
	}
	if err := writeConfirmationText(boundary.promptOutput, "\n"); err != nil {
		return false, fmt.Errorf("write prompt: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if errors.Is(err, io.EOF) {
		return false, nil
	}

	normalized := strings.ToLower(strings.TrimSpace(answer))
	return normalized == "y" || normalized == "yes", nil
}

func writeConfirmationText(output io.Writer, text string) error {
	written, err := io.WriteString(output, text)
	if err == nil && written != len(text) {
		return io.ErrShortWrite
	}
	return err
}

func printInteractiveConfirmationRequired(output io.Writer, command string, noun string) {
	fmt.Fprintf(output, "%s failed: non-interactive %s requires --yes\n", command, noun)
	fmt.Fprintf(output, "detail: %s\n", errInteractiveConfirmationUnavailable)
}

func printConfirmationFailure(output io.Writer, command string, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(output, "%s canceled: %s\n", command, humanDiagnosticError(err))
		return
	}
	fmt.Fprintf(output, "%s failed: %s\n", command, humanDiagnosticError(err))
}
