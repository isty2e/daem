package mcp

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

// Command is one immutable, secret-free stdio command reference.
type Command struct {
	name string
}

// NewAmbientCommand constructs a portable executable command reference.
func NewAmbientCommand(name string) (Command, error) {
	if err := validatePortableCommand(name); err != nil {
		return Command{}, err
	}
	return Command{name: name}, nil
}

// Name returns the ambient executable name.
func (command Command) Name() string { return command.name }

func (command Command) validate() error {
	return validatePortableCommand(command.name)
}

// EnvReference identifies an environment variable without carrying its value.
type EnvReference struct {
	fromEnv string
}

// NewEnvReference constructs a secret-free environment reference.
func NewEnvReference(fromEnv string) (EnvReference, error) {
	if strings.TrimSpace(fromEnv) != fromEnv {
		return EnvReference{}, fmt.Errorf("env reference must not contain leading or trailing whitespace")
	}
	if err := validateEnvName(fromEnv, "env reference"); err != nil {
		return EnvReference{}, err
	}
	return EnvReference{fromEnv: fromEnv}, nil
}

// FromEnv returns the host environment variable name.
func (reference EnvReference) FromEnv() string { return reference.fromEnv }

// TransportKind identifies a canonical MCP transport variant.
type TransportKind string

const (
	TransportKindStdio TransportKind = "stdio"
)

// Stdio is immutable stdio transport desired state.
type Stdio struct {
	command Command
	args    []string
	env     map[string]EnvReference
}

// Transport is the canonical admitted MCP transport.
type Transport struct {
	kind  TransportKind
	stdio Stdio
}

// NewStdioTransport constructs a stdio transport without shell interpretation.
func NewStdioTransport(
	command Command,
	args []string,
	env map[string]EnvReference,
) (Transport, error) {
	if err := command.validate(); err != nil {
		return Transport{}, fmt.Errorf("stdio command: %w", err)
	}
	canonicalArgs := append([]string(nil), args...)
	for index, argument := range canonicalArgs {
		if err := validateArgument(argument); err != nil {
			return Transport{}, fmt.Errorf("stdio args[%d]: %w", index, err)
		}
	}
	canonicalEnv := make(map[string]EnvReference, len(env))
	envNames := make([]string, 0, len(env))
	for name := range env {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)
	for _, name := range envNames {
		reference := env[name]
		if err := validateEnvName(name, "stdio env name"); err != nil {
			return Transport{}, err
		}
		canonical, err := NewEnvReference(reference.fromEnv)
		if err != nil {
			return Transport{}, fmt.Errorf("stdio env %q: %w", name, err)
		}
		canonicalEnv[name] = canonical
	}
	return Transport{
		kind: TransportKindStdio,
		stdio: Stdio{
			command: command,
			args:    canonicalArgs,
			env:     canonicalEnv,
		},
	}, nil
}

// Kind returns the closed transport variant.
func (transport Transport) Kind() TransportKind { return transport.kind }

// Stdio returns stdio state when this is a stdio transport.
func (transport Transport) Stdio() (Stdio, bool) {
	return transport.stdio, transport.kind == TransportKindStdio
}

func (transport Transport) validate() error {
	switch transport.kind {
	case TransportKindStdio:
		_, err := NewStdioTransport(
			transport.stdio.command,
			transport.stdio.args,
			transport.stdio.env,
		)
		return err
	default:
		return fmt.Errorf("unknown MCP transport kind %q", transport.kind)
	}
}

// Command returns the stdio command reference.
func (stdio Stdio) Command() Command { return stdio.command }

// Args returns a defensive argv copy.
func (stdio Stdio) Args() []string { return append([]string(nil), stdio.args...) }

// Env returns a defensive environment-reference copy.
func (stdio Stdio) Env() map[string]EnvReference {
	result := make(map[string]EnvReference, len(stdio.env))
	maps.Copy(result, stdio.env)
	return result
}
