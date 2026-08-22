package mcp

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestTransportValidationRejectsForgedUnknownKind(t *testing.T) {
	command, _ := NewAmbientCommand("node")
	validStdio, _ := NewStdioTransport(command, []string{"server.js"}, nil)
	forged := Transport{
		kind:  TransportKind("http"),
		stdio: validStdio.stdio,
	}
	if err := forged.validate(); err == nil || !strings.Contains(err.Error(), "unknown MCP transport kind") {
		t.Fatalf("validate error = %v, want unknown-kind rejection", err)
	}
}

func TestServerRejectsSameBindingKeyWithDifferentTransportState(t *testing.T) {
	node, _ := NewAmbientCommand("node")
	npx, _ := NewAmbientCommand("npx")
	leftTransport, _ := NewStdioTransport(node, []string{"server.js"}, nil)
	rightTransport, _ := NewStdioTransport(npx, []string{"server@1"}, nil)
	left, _ := NewBinding(target.TargetCodex, target.ScopeProject, leftTransport, OnAbsentRemoveBinding)
	right, _ := NewBinding(target.TargetCodex, target.ScopeProject, rightTransport, OnAbsentKeep)
	if _, err := New(Spec{Name: "server", Bindings: []Binding{left, right}}); err == nil || !strings.Contains(err.Error(), "duplicate binding") {
		t.Fatalf("New error = %v, want duplicate binding", err)
	}
}

func TestStdioAccessorsDefendEveryCollection(t *testing.T) {
	command, _ := NewAmbientCommand("node")
	reference, _ := NewEnvReference("HOST_TOKEN")
	transport, err := NewStdioTransport(
		command,
		[]string{"server.js"},
		map[string]EnvReference{"TOKEN": reference},
	)
	if err != nil {
		t.Fatalf("NewStdioTransport returned error: %v", err)
	}
	stdio, _ := transport.Stdio()
	args := stdio.Args()
	env := stdio.Env()
	args[0] = "changed"
	env["TOKEN"] = EnvReference{}
	stdio, _ = transport.Stdio()
	if stdio.Args()[0] != "server.js" || stdio.Env()["TOKEN"].FromEnv() != "HOST_TOKEN" {
		t.Fatal("stdio accessor exposed mutable canonical storage")
	}
}

func TestStdioEnvironmentSourceNamesAreDistinctStableAndDefensive(t *testing.T) {
	command, _ := NewAmbientCommand("node")
	first, _ := NewEnvReference("HOST_Z")
	second, _ := NewEnvReference("HOST_A")
	transport, err := NewStdioTransport(
		command,
		nil,
		map[string]EnvReference{
			"FIRST":  first,
			"ALIAS":  first,
			"SECOND": second,
		},
	)
	if err != nil {
		t.Fatalf("NewStdioTransport returned error: %v", err)
	}
	stdio, _ := transport.Stdio()
	names := stdio.EnvironmentSourceNames()
	if !slices.Equal(names, []string{"HOST_A", "HOST_Z"}) {
		t.Fatalf("EnvironmentSourceNames = %#v", names)
	}
	names[0] = "changed"
	if !slices.Equal(stdio.EnvironmentSourceNames(), []string{"HOST_A", "HOST_Z"}) {
		t.Fatal("EnvironmentSourceNames exposed mutable canonical storage")
	}
}

func TestServerAllowsSameTargetAtDistinctScopes(t *testing.T) {
	command, _ := NewAmbientCommand("node")
	transport, _ := NewStdioTransport(command, nil, nil)
	project, _ := NewBinding(target.TargetCodex, target.ScopeProject, transport, OnAbsentRemoveBinding)
	global, _ := NewBinding(target.TargetCodex, target.ScopeGlobal, transport, OnAbsentRemoveBinding)
	if _, err := New(Spec{Name: "server", Bindings: []Binding{project, global}}); err != nil {
		t.Fatalf("New conflated distinct scope bindings: %v", err)
	}
}

func TestValidateStdioArgumentsMatchesTransportAdmission(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "valid Unicode and empty", args: []string{"안녕👩‍💻", ""}},
		{name: "invalid UTF-8", args: []string{string([]byte{'b', 'a', 'd', 0xff})}, want: "valid UTF-8"},
		{name: "control", args: []string{"safe\x00text"}, want: "control"},
		{name: "bidirectional control", args: []string{"safe\u202etext"}, want: "control"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateStdioArguments(test.args)
			if test.want == "" {
				if err != nil {
					t.Fatalf("ValidateStdioArguments error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateStdioArguments error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStdioTransportRejectsInvalidUTF8Argument(t *testing.T) {
	command, _ := NewAmbientCommand("node")
	if _, err := NewStdioTransport(command, []string{string([]byte{'b', 'a', 'd', 0xff})}, nil); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("NewStdioTransport error = %v, want invalid UTF-8 rejection", err)
	}
}

func TestStdioTransportRejectsBidirectionalControlArgument(t *testing.T) {
	command, _ := NewAmbientCommand("node")
	if _, err := NewStdioTransport(command, []string{"safe\u202etxt"}, nil); err == nil || !strings.Contains(err.Error(), "control") {
		t.Fatalf("NewStdioTransport error = %v, want bidirectional control rejection", err)
	}
}
