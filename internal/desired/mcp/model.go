package mcp

import (
	"fmt"
	"maps"
	"slices"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

// OnAbsent identifies requested binding-local absence behavior.
type OnAbsent string

const (
	OnAbsentKeep          OnAbsent = "keep"
	OnAbsentRemoveBinding OnAbsent = "remove_binding"
)

// ParseOnAbsent validates an MCP binding absence policy.
func ParseOnAbsent(value string) (OnAbsent, error) {
	switch OnAbsent(value) {
	case OnAbsentKeep, OnAbsentRemoveBinding:
		return OnAbsent(value), nil
	default:
		return "", fmt.Errorf("unsupported MCP on_absent %q", value)
	}
}

type bindingKey struct {
	target target.Target
	scope  target.Scope
}

// Binding is one immutable target/scope transport relation for a server.
type Binding struct {
	target    target.Target
	scope     target.Scope
	transport Transport
	onAbsent  OnAbsent
}

// NewBinding constructs one canonical server binding.
func NewBinding(selected target.Target, scope target.Scope, transport Transport, onAbsent OnAbsent) (Binding, error) {
	parsedTarget, err := target.ParseTarget(string(selected))
	if err != nil {
		return Binding{}, err
	}
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return Binding{}, err
	}
	if err := transport.validate(); err != nil {
		return Binding{}, err
	}
	parsedOnAbsent, err := ParseOnAbsent(string(onAbsent))
	if err != nil {
		return Binding{}, err
	}
	return Binding{target: parsedTarget, scope: parsedScope, transport: transport, onAbsent: parsedOnAbsent}, nil
}

// Target returns the binding target.
func (binding Binding) Target() target.Target { return binding.target }

// Scope returns the binding scope.
func (binding Binding) Scope() target.Scope { return binding.scope }

// Transport returns immutable transport desired state.
func (binding Binding) Transport() Transport { return binding.transport }

// OnAbsent returns the requested absence behavior.
func (binding Binding) OnAbsent() OnAbsent { return binding.onAbsent }

func (binding Binding) key() bindingKey {
	return bindingKey{target: binding.target, scope: binding.scope}
}

func (binding Binding) equal(other Binding) bool {
	return binding.target == other.target &&
		binding.scope == other.scope &&
		binding.onAbsent == other.onAbsent &&
		binding.transport.kind == other.transport.kind &&
		binding.transport.stdio.command == other.transport.stdio.command &&
		slices.Equal(binding.transport.stdio.args, other.transport.stdio.args) &&
		maps.Equal(binding.transport.stdio.env, other.transport.stdio.env)
}

// Validate rejects a zero or invalid Binding value.
func (binding Binding) Validate() error {
	_, err := NewBinding(binding.target, binding.scope, binding.transport, binding.onAbsent)
	return err
}

// Spec is constructor input for one canonical MCP Server.
type Spec struct {
	Name     string
	Bindings []Binding
}

// Server is one authored MCP server with binding-local projection requests.
type Server struct {
	id       entity.ID
	bindings []Binding
}

// New constructs and validates a canonical standalone MCP server.
func New(spec Spec) (Server, error) {
	if err := validateStableToken(spec.Name, "MCP server name"); err != nil {
		return Server{}, err
	}
	id, err := entity.New(entity.KindMCPServer, spec.Name)
	if err != nil {
		return Server{}, err
	}
	if len(spec.Bindings) == 0 {
		return Server{}, fmt.Errorf("MCP server %q requires at least one binding", spec.Name)
	}
	bindings := append([]Binding(nil), spec.Bindings...)
	seen := make(map[bindingKey]struct{}, len(bindings))
	for index, binding := range bindings {
		if err := binding.Validate(); err != nil {
			return Server{}, fmt.Errorf("MCP server %q binding[%d]: %w", spec.Name, index, err)
		}
		if _, exists := seen[binding.key()]; exists {
			return Server{}, fmt.Errorf("MCP server %q has duplicate binding for target=%s scope=%s", spec.Name, binding.target, binding.scope)
		}
		seen[binding.key()] = struct{}{}
	}
	return Server{id: id, bindings: bindings}, nil
}

// Validate rejects a zero or invalid Server value.
func (server Server) Validate() error {
	if server.id.Kind() != entity.KindMCPServer {
		return fmt.Errorf("MCP server has entity kind %q", server.id.Kind())
	}
	_, err := New(Spec{Name: server.id.Name(), Bindings: server.bindings})
	return err
}

// ID returns the authored desired identity.
func (server Server) ID() entity.ID { return server.id }

// Bindings returns a defensive copy in declaration order.
func (server Server) Bindings() []Binding { return append([]Binding(nil), server.bindings...) }

// OwnsBinding reports whether binding is one exact relation in this Server.
// Matching target and scope alone is insufficient because transport and
// absence policy are binding-local desired state.
func (server Server) OwnsBinding(binding Binding) bool {
	for _, candidate := range server.bindings {
		if candidate.equal(binding) {
			return true
		}
	}
	return false
}
