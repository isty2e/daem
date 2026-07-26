package mcp

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestServerOwnsBindingLocalTransportAndDefensiveState(t *testing.T) {
	command, err := NewAmbientCommand("npx")
	if err != nil {
		t.Fatalf("NewAmbientCommand returned error: %v", err)
	}
	reference, err := NewEnvReference("HOST_TOKEN")
	if err != nil {
		t.Fatalf("NewEnvReference returned error: %v", err)
	}
	args := []string{"-y", "@acme/server"}
	env := map[string]EnvReference{"API_TOKEN": reference}
	stdio, err := NewStdioTransport(command, args, env)
	if err != nil {
		t.Fatalf("NewStdioTransport returned error: %v", err)
	}
	first, err := NewBinding(target.TargetClaudeCode, target.ScopeProject, stdio, OnAbsentRemoveBinding)
	if err != nil {
		t.Fatalf("NewBinding returned error: %v", err)
	}
	node, err := NewAmbientCommand("node")
	if err != nil {
		t.Fatalf("NewAmbientCommand returned error: %v", err)
	}
	otherStdio, err := NewStdioTransport(node, []string{"server.js"}, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport returned error: %v", err)
	}
	second, err := NewBinding(target.TargetOpenCode, target.ScopeGlobal, otherStdio, OnAbsentKeep)
	if err != nil {
		t.Fatalf("NewBinding returned error: %v", err)
	}
	bindings := []Binding{first, second}
	server, err := New(Spec{Name: "context7", Bindings: bindings})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	args[0] = "changed"
	env["API_TOKEN"] = EnvReference{}
	bindings[0] = Binding{}
	if server.ID().Kind() != entity.KindMCPServer || server.ID().Name() != "context7" {
		t.Fatalf("ID = %q", server.ID())
	}
	got := server.Bindings()
	stdioState, ok := got[0].Transport().Stdio()
	if !ok || stdioState.Args()[0] != "-y" || stdioState.Env()["API_TOKEN"].FromEnv() != "HOST_TOKEN" {
		t.Fatalf("stdio state = %#v", stdioState)
	}
	otherStdioState, ok := got[1].Transport().Stdio()
	if !ok || otherStdioState.Command().Name() != "node" || !slices.Equal(otherStdioState.Args(), []string{"server.js"}) {
		t.Fatalf("second stdio state = %#v", otherStdioState)
	}
	got[0] = Binding{}
	if server.Bindings()[0].Target() != target.TargetClaudeCode || server.Validate() != nil {
		t.Fatal("Bindings returned aliased storage or server became invalid")
	}
}

func TestServerAllowsDifferentDefinitionsAcrossBindings(t *testing.T) {
	npx, _ := NewAmbientCommand("npx")
	node, _ := NewAmbientCommand("node")
	leftTransport, _ := NewStdioTransport(npx, []string{"package@latest"}, nil)
	rightTransport, _ := NewStdioTransport(node, []string{"server.js"}, nil)
	left, _ := NewBinding(target.TargetClaudeCode, target.ScopeProject, leftTransport, OnAbsentRemoveBinding)
	right, _ := NewBinding(target.TargetOpenCode, target.ScopeProject, rightTransport, OnAbsentRemoveBinding)
	if _, err := New(Spec{Name: "shared-name", Bindings: []Binding{left, right}}); err != nil {
		t.Fatalf("New rejected binding-local definitions: %v", err)
	}
}

func TestServerOwnsOnlyExactBindings(t *testing.T) {
	npx, _ := NewAmbientCommand("npx")
	ownedTransport, _ := NewStdioTransport(npx, []string{"server@1"}, nil)
	foreignTransport, _ := NewStdioTransport(npx, []string{"server@2"}, nil)
	owned, _ := NewBinding(target.TargetClaudeCode, target.ScopeProject, ownedTransport, OnAbsentRemoveBinding)
	foreignTransportBinding, _ := NewBinding(target.TargetClaudeCode, target.ScopeProject, foreignTransport, OnAbsentRemoveBinding)
	foreignAbsenceBinding, _ := NewBinding(target.TargetClaudeCode, target.ScopeProject, ownedTransport, OnAbsentKeep)
	server, _ := New(Spec{Name: "server", Bindings: []Binding{owned}})

	if !server.OwnsBinding(owned) {
		t.Fatal("OwnsBinding rejected the server's exact binding")
	}
	if server.OwnsBinding(foreignTransportBinding) {
		t.Fatal("OwnsBinding accepted a foreign transport with the same target and scope")
	}
	if server.OwnsBinding(foreignAbsenceBinding) {
		t.Fatal("OwnsBinding accepted a foreign absence policy with the same target and scope")
	}
}

func TestStdioTransportPreservesEmptyArgument(t *testing.T) {
	command, err := NewAmbientCommand("node")
	if err != nil {
		t.Fatalf("NewAmbientCommand returned error: %v", err)
	}
	transport, err := NewStdioTransport(command, []string{"--label", ""}, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport returned error: %v", err)
	}
	stdio, ok := transport.Stdio()
	if !ok {
		t.Fatal("transport did not retain stdio state")
	}
	if !slices.Equal(stdio.Args(), []string{"--label", ""}) {
		t.Fatalf("Args = %#v, want empty argument preserved", stdio.Args())
	}
}

func TestMCPConstructorsRejectInvalidStates(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "path command", run: func() error { _, err := NewAmbientCommand("./server"); return err }, want: "portable command"},
		{name: "invalid env", run: func() error { _, err := NewEnvReference("9TOKEN"); return err }, want: "must not start"},
		{name: "invalid UTF-8 arg", run: func() error {
			command, _ := NewAmbientCommand("node")
			_, err := NewStdioTransport(command, []string{string([]byte{0xff})}, nil)
			return err
		}, want: "valid UTF-8"},
		{name: "control arg", run: func() error {
			command, _ := NewAmbientCommand("node")
			_, err := NewStdioTransport(command, []string{"x\ny"}, nil)
			return err
		}, want: "control"},
		{name: "unicode control arg", run: func() error {
			command, _ := NewAmbientCommand("node")
			_, err := NewStdioTransport(command, []string{"x\u0085y"}, nil)
			return err
		}, want: "control"},
		{name: "zero transport", run: func() error {
			_, err := NewBinding(target.TargetCodex, target.ScopeProject, Transport{}, OnAbsentRemoveBinding)
			return err
		}, want: "unknown MCP transport"},
		{name: "unknown absence", run: func() error {
			command, _ := NewAmbientCommand("node")
			transport, _ := NewStdioTransport(command, nil, nil)
			_, err := NewBinding(target.TargetCodex, target.ScopeProject, transport, "prune")
			return err
		}, want: "unsupported MCP on_absent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestServerRejectsInvalidIdentityDuplicateBindingAndZero(t *testing.T) {
	command, _ := NewAmbientCommand("node")
	transport, _ := NewStdioTransport(command, nil, nil)
	binding, _ := NewBinding(target.TargetCodex, target.ScopeProject, transport, OnAbsentRemoveBinding)
	if _, err := New(Spec{Name: "bad name", Bindings: []Binding{binding}}); err == nil {
		t.Fatal("New accepted unstable server name")
	}
	if _, err := New(Spec{Name: "server", Bindings: nil}); err == nil {
		t.Fatal("New accepted no bindings")
	}
	if _, err := New(Spec{Name: "server", Bindings: []Binding{binding, binding}}); err == nil {
		t.Fatal("New accepted duplicate binding")
	}
	if err := (Server{}).Validate(); err == nil {
		t.Fatal("zero Server validated")
	}
}
