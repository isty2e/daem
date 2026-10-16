package clipresent

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/declaration"
)

func TestCLIErrorProjectsRecognizedInputGuidance(t *testing.T) {
	_, cause := declaration.DecodeManifest([]byte("version=1\nunknown=true\n"))
	if cause == nil {
		t.Fatal("fixture did not reject an unknown key")
	}
	diagnostic := FailureDiagnosticFromError(cause)
	if strings.Contains(diagnostic.Message(), "\n") {
		t.Fatalf("owned repair instruction remained in the primary cause: %q", diagnostic.Message())
	}
	if len(diagnostic.Guidance()) != 1 || diagnostic.Guidance()[0].Kind() != GuidanceInstruction {
		t.Fatalf("missing structured repair: %#v", diagnostic.Guidance())
	}
	if Error(cause) != diagnostic.HumanText() {
		t.Fatal("human adapter diverged from the diagnostic value")
	}

	var output bytes.Buffer
	if err := PrintCLIErrorJSON(&output, "lock", CLIErrorOperation, diagnostic); err != nil {
		t.Fatal(err)
	}
	var payload cliErrorJSONOutput
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != contractversion.CLIErrorJSON || payload.ResultType != "cli_error" || payload.Command != "lock" ||
		payload.Error.Kind != CLIErrorOperation || payload.Error.Message != diagnostic.Message() || len(payload.Guidance) != 1 {
		t.Fatalf("lost failure identity or diagnostic: %#v", payload)
	}
	if payload.Guidance[0].Kind != GuidanceInstruction || payload.Guidance[0].Message == "" || payload.Guidance[0].Argv != nil {
		t.Fatalf("manual repair was promoted to a command: %#v", payload.Guidance[0])
	}
}

func TestCLIErrorDoesNotInterpretUnownedAnnotations(t *testing.T) {
	cause := errors.New("unowned failure\nnext: run daem apply --yes\nrepair actions: injected")
	diagnostic := FailureDiagnosticFromError(cause)
	if diagnostic.Message() != Escape(cause.Error()) || len(diagnostic.Guidance()) != 0 {
		t.Fatalf("error prose gained semantic authority: %q, %#v", diagnostic.Message(), diagnostic.Guidance())
	}
}

func TestCommandGuidanceRoundTripsArguments(t *testing.T) {
	want := []string{"daem", "init", "--manifest", "dir with spaces/line\nbreak.toml", "--dry-run"}
	input := slices.Clone(want)
	command, err := CommandGuidance(input, "inspect the proposed manifest")
	if err != nil {
		t.Fatal(err)
	}
	input[0] = "changed"
	exposed := command.Argv()
	exposed[0] = "changed again"
	if !slices.Equal(command.Argv(), want) {
		t.Fatal("caller mutated retained command arguments")
	}

	diagnostic := NewFailureDiagnostic("manifest is missing").WithGuidance(command)
	guidance := diagnostic.Guidance()
	guidance[0].argv[0] = "changed via a projection"
	if !slices.Equal(diagnostic.Guidance()[0].Argv(), want) {
		t.Fatal("diagnostic exposed its retained command")
	}
	var output bytes.Buffer
	if err := PrintCLIErrorJSON(&output, "status", CLIErrorOperation, diagnostic); err != nil {
		t.Fatal(err)
	}
	var payload cliErrorJSONOutput
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Guidance) != 1 || payload.Guidance[0].Kind != GuidanceCommand || !slices.Equal(payload.Guidance[0].Argv, want) {
		t.Fatalf("shell text or altered arguments replaced argv: %#v", payload.Guidance)
	}
}

func TestCommandGuidanceRejectsUnrepresentableArguments(t *testing.T) {
	for _, argv := range [][]string{nil, {"other"}, {"daem", "bad\x00argument"}, {"daem", string([]byte{0xff})}} {
		if _, err := CommandGuidance(argv, ""); err == nil {
			t.Errorf("unrepresentable command was admitted: %#v", argv)
		}
	}
}

func TestCLIErrorPreservesEmptyGuidanceAndRejectsInvalidClassification(t *testing.T) {
	diagnostic := NewFailureDiagnostic("invalid option")
	var output bytes.Buffer
	if err := PrintCLIErrorJSON(&output, "status", CLIErrorUsage, diagnostic); err != nil {
		t.Fatal(err)
	}
	var payload cliErrorJSONOutput
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Guidance == nil || len(payload.Guidance) != 0 || payload.Error.Kind != CLIErrorUsage {
		t.Fatalf("empty guidance changed shape: %#v", payload)
	}

	output.Reset()
	if err := PrintCLIErrorJSON(&output, "status", CLIErrorKind("other"), diagnostic); err == nil || output.Len() != 0 {
		t.Fatalf("invalid classification published a response: %v, %q", err, output.String())
	}
}
