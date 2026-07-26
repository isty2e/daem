package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func readConfirmationLineFromReader(ctx context.Context, input io.Reader, maximumBytes int) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("confirmation context is required")
	}
	if input == nil {
		return "", fmt.Errorf("confirmation input is required")
	}
	if maximumBytes <= 0 {
		return "", fmt.Errorf("confirmation response limit must be positive")
	}

	reader := bufio.NewReader(input)
	answer := make([]byte, 0, 16)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		value, err := reader.ReadByte()
		if err != nil {
			return string(answer), err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if value == '\n' {
			return string(answer), nil
		}
		answer = append(answer, value)
		if len(answer) > maximumBytes {
			return "", fmt.Errorf("confirmation response exceeds %d bytes", maximumBytes)
		}
	}
}

func TestConfirmationBoundaryRequiresEveryTerminalRole(t *testing.T) {
	for mask := range 8 {
		var output bytes.Buffer
		boundary := confirmationBoundary{
			context:              context.Background(),
			input:                strings.NewReader("yes\n"),
			promptOutput:         &output,
			readLine:             readConfirmationLineFromReader,
			inputIsTerminal:      mask&1 != 0,
			disclosureIsTerminal: mask&2 != 0,
			promptIsTerminal:     mask&4 != 0,
		}

		want := mask == 7
		if got := boundary.allowsInteractiveAuthorization(); got != want {
			t.Fatalf("mask %03b allowsInteractiveAuthorization() = %t, want %t", mask, got, want)
		}
	}
}

func TestConfirmationBoundaryRefusesBeforePromptWhenRoleIsUnavailable(t *testing.T) {
	var output bytes.Buffer
	boundary := confirmationBoundary{
		context:              context.Background(),
		input:                strings.NewReader("yes\n"),
		promptOutput:         &output,
		readLine:             readConfirmationLineFromReader,
		inputIsTerminal:      true,
		disclosureIsTerminal: false,
		promptIsTerminal:     true,
	}
	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, errInteractiveConfirmationUnavailable) {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want no prompt", output.String())
	}
}

func TestConfirmationBoundaryRefusesFailedDisclosureBeforePrompt(t *testing.T) {
	disclosureErr := errors.New("stdout closed")
	var output bytes.Buffer
	boundary := readyConfirmationBoundary(strings.NewReader("yes\n"), &output)
	boundary.disclosureError = func() error { return disclosureErr }

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, disclosureErr) || !strings.Contains(err.Error(), "disclose plan") {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want no prompt", output.String())
	}
}

func TestConfirmationBoundaryPromptWriteFailureDoesNotReadInput(t *testing.T) {
	input := &countingReader{reader: strings.NewReader("yes\n")}
	promptErr := errors.New("stderr closed")
	boundary := readyConfirmationBoundary(input, errorWriter{err: promptErr})

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, promptErr) || !strings.Contains(err.Error(), "write prompt") {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
	if input.reads != 0 {
		t.Fatalf("input reads = %d, want zero", input.reads)
	}
}

func TestConfirmationBoundaryPromptShortWriteDoesNotReadInput(t *testing.T) {
	input := &countingReader{reader: strings.NewReader("yes\n")}
	output := &shortWriteRecorder{}
	boundary := readyConfirmationBoundary(input, output)

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, io.ErrShortWrite) || !strings.Contains(err.Error(), "write prompt") {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
	if input.reads != 0 {
		t.Fatalf("input reads = %d, want zero", input.reads)
	}
}

func TestConfirmationBoundaryPromptTerminatorFailureRejectsAffirmativeInput(t *testing.T) {
	input := &countingReader{reader: strings.NewReader("yes\n")}
	promptErr := errors.New("stderr closed after prompt")
	output := &failAfterSuccessfulWrites{remaining: 1, err: promptErr}
	boundary := readyConfirmationBoundary(input, output)

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, promptErr) || !strings.Contains(err.Error(), "write prompt") {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
	if input.reads == 0 || !strings.Contains(output.buffer.String(), "Proceed with apply?") {
		t.Fatalf("input reads = %d output = %q", input.reads, output.buffer.String())
	}
}

func TestConfirmationBoundaryPromptTerminatorShortWriteRejectsAffirmativeInput(t *testing.T) {
	input := &countingReader{reader: strings.NewReader("yes\n")}
	output := &shortWriteAfterSuccessfulWrites{remaining: 1}
	boundary := readyConfirmationBoundary(input, output)

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, io.ErrShortWrite) || !strings.Contains(err.Error(), "write prompt") {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
	if input.reads == 0 || !strings.Contains(output.buffer.String(), "Proceed with apply?") {
		t.Fatalf("input reads = %d output = %q", input.reads, output.buffer.String())
	}
}

func TestConfirmationBoundaryReaderFailureCannotAuthorizeBufferedYes(t *testing.T) {
	readErr := errors.New("stdin failed")
	var output bytes.Buffer
	boundary := readyConfirmationBoundary(&dataAndErrorReader{data: []byte("yes"), err: readErr}, &output)

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, readErr) || !strings.Contains(err.Error(), "read confirmation") {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
}

func TestConfirmationBoundaryCanceledContextWinsBeforePrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	boundary := readyConfirmationBoundary(strings.NewReader("yes\n"), &output)
	boundary.context = ctx

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, context.Canceled) {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want no prompt", output.String())
	}
}

func TestConfirmationBoundaryCancellationWinsOverAffirmativeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	boundary := readyConfirmationBoundary(&cancelAfterRead{
		reader: strings.NewReader("yes\n"),
		cancel: cancel,
	}, &output)
	boundary.context = ctx

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, context.Canceled) {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
	if output.String() != "Proceed with apply? [y/N]: \n" {
		t.Fatalf("output = %q", output.String())
	}
}

func TestConfirmationBoundaryParsesBoundedAnswers(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		confirmed bool
		wantError string
	}{
		{name: "short y", input: "y\n", confirmed: true},
		{name: "case and spaces", input: "  YeS  \r\n", confirmed: true},
		{name: "decline", input: "no\n"},
		{name: "unknown", input: "sure\n"},
		{name: "empty line", input: "\n"},
		{name: "eof", input: ""},
		{name: "yes at eof is not authority", input: "yes"},
		{name: "nul is not yes", input: "yes\x00\n"},
		{name: "invalid utf8 is not yes", input: string([]byte{'y', 'e', 's', 0xff, '\n'})},
		{name: "unicode confusable is not yes", input: "ｙｅｓ\n"},
		{name: "maximum", input: strings.Repeat("x", maximumConfirmationAnswerBytes)},
		{name: "oversized", input: strings.Repeat("x", maximumConfirmationAnswerBytes+1), wantError: "exceeds"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			boundary := readyConfirmationBoundary(strings.NewReader(test.input), &output)
			confirmed, err := boundary.prompt("probe")
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || confirmed {
					t.Fatalf("confirmed = %t, error = %v", confirmed, err)
				}
				return
			}
			if err != nil || confirmed != test.confirmed {
				t.Fatalf("confirmed = %t, error = %v, want confirmed = %t", confirmed, err, test.confirmed)
			}
			if output.String() != "Proceed with probe? [y/N]: \n" {
				t.Fatalf("output = %q", output.String())
			}
		})
	}
}

func TestConfirmationBoundaryCancellationDuringPromptTerminatorWins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	output := &cancelOnWrite{cancelAt: 2, cancel: cancel}
	boundary := readyConfirmationBoundary(strings.NewReader("yes\n"), output)
	boundary.context = ctx

	confirmed, err := boundary.prompt("apply")
	if confirmed || !errors.Is(err, context.Canceled) {
		t.Fatalf("confirmed = %t, error = %v", confirmed, err)
	}
}

func TestConfirmationBoundaryZeroProgressReaderFailsClosed(t *testing.T) {
	var output bytes.Buffer
	boundary := readyConfirmationBoundary(zeroProgressReader{}, &output)
	result := make(chan error, 1)
	go func() {
		_, err := boundary.prompt("apply")
		result <- err
	}()

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "no data") {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("zero-progress reader left confirmation blocked")
	}
}

func readyConfirmationBoundary(input io.Reader, output io.Writer) confirmationBoundary {
	return confirmationBoundary{
		context:              context.Background(),
		input:                input,
		promptOutput:         output,
		readLine:             readConfirmationLineFromReader,
		inputIsTerminal:      true,
		disclosureIsTerminal: true,
		promptIsTerminal:     true,
	}
}

type countingReader struct {
	reader io.Reader
	reads  int
}

func (reader *countingReader) Read(payload []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(payload)
}

type cancelAfterRead struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (reader *cancelAfterRead) Read(payload []byte) (int, error) {
	count, err := reader.reader.Read(payload)
	reader.cancel()
	return count, err
}

type errorWriter struct {
	err error
}

func (writer errorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

type shortWriteRecorder struct {
	buffer bytes.Buffer
}

func (writer *shortWriteRecorder) Write(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	return writer.buffer.Write(payload[:len(payload)-1])
}

type failAfterSuccessfulWrites struct {
	buffer    bytes.Buffer
	remaining int
	err       error
}

type shortWriteAfterSuccessfulWrites struct {
	buffer    bytes.Buffer
	remaining int
}

func (writer *shortWriteAfterSuccessfulWrites) Write(payload []byte) (int, error) {
	if writer.remaining > 0 {
		writer.remaining--
		return writer.buffer.Write(payload)
	}
	if len(payload) == 0 {
		return 0, nil
	}
	return writer.buffer.Write(payload[:len(payload)-1])
}

func (writer *failAfterSuccessfulWrites) Write(payload []byte) (int, error) {
	if writer.remaining == 0 {
		return 0, writer.err
	}
	writer.remaining--
	return writer.buffer.Write(payload)
}

type dataAndErrorReader struct {
	data []byte
	err  error
	done bool
}

type cancelOnWrite struct {
	buffer   bytes.Buffer
	writes   int
	cancelAt int
	cancel   context.CancelFunc
}

func (writer *cancelOnWrite) Write(payload []byte) (int, error) {
	writer.writes++
	count, err := writer.buffer.Write(payload)
	if writer.writes == writer.cancelAt {
		writer.cancel()
	}
	return count, err
}

type zeroProgressReader struct{}

func (zeroProgressReader) Read([]byte) (int, error) {
	return 0, nil
}

func (reader *dataAndErrorReader) Read(payload []byte) (int, error) {
	if reader.done {
		return 0, reader.err
	}
	reader.done = true
	return copy(payload, reader.data), reader.err
}
