// Package mcp owns MCP structural subject identities and the pure lowering
// from canonical Desired MCP bindings into topology.
package mcp

import (
	"fmt"
	"path/filepath"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/topology"
)

const (
	executableNamespace         = "executable"
	absoluteExecutableNamespace = "executable.path"
	environmentNamespace        = "env"
)

// ExecutableSubject returns the structural identity of an ambient command lookup
// or an exact absolute executable path. The namespaces keep the two resolution
// contracts distinct while preserving existing ambient identities.
func ExecutableSubject(command string) (topology.SubjectID, error) {
	if filepath.IsAbs(command) {
		if _, err := desiredmcp.NewAbsolutePathCommand(command); err != nil {
			return topology.SubjectID{}, err
		}
		return topology.NewSubjectID(topology.SubjectRuntimeDependency, absoluteExecutableNamespace, command)
	}
	if _, err := desiredmcp.NewAmbientCommand(command); err != nil {
		return topology.SubjectID{}, err
	}
	return topology.NewSubjectID(topology.SubjectRuntimeDependency, executableNamespace, command)
}

// ExecutableReference returns the canonical command reference represented by subject.
func ExecutableReference(subject topology.SubjectID) (desiredmcp.Command, bool) {
	if subject.Kind() != topology.SubjectRuntimeDependency {
		return desiredmcp.Command{}, false
	}
	switch subject.Namespace() {
	case executableNamespace:
		command, err := desiredmcp.NewAmbientCommand(subject.Key())
		if err != nil {
			return desiredmcp.Command{}, false
		}
		return command, true
	case absoluteExecutableNamespace:
		command, err := desiredmcp.NewAbsolutePathCommand(subject.Key())
		if err != nil {
			return desiredmcp.Command{}, false
		}
		return command, true
	default:
		return desiredmcp.Command{}, false
	}
}

// EnvironmentReferenceSubject returns the structural identity of one source
// host environment reference. Destination env slots are not topology identity.
func EnvironmentReferenceSubject(fromEnv string) (topology.SubjectID, error) {
	if err := validateEnvironmentName(fromEnv); err != nil {
		return topology.SubjectID{}, err
	}
	return topology.NewSubjectID(topology.SubjectCredentialReference, environmentNamespace, fromEnv)
}

// EnvironmentReferenceName returns the source host env name represented by subject.
func EnvironmentReferenceName(subject topology.SubjectID) (string, bool) {
	if subject.Kind() != topology.SubjectCredentialReference || subject.Namespace() != environmentNamespace {
		return "", false
	}
	if err := validateEnvironmentName(subject.Key()); err != nil {
		return "", false
	}
	return subject.Key(), true
}

func validateEnvironmentName(value string) error {
	if value == "" {
		return fmt.Errorf("env name is required")
	}
	if value[0] >= '0' && value[0] <= '9' {
		return fmt.Errorf("env name %q must not start with a digit", value)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if isASCIIAlnum(character) || character == '_' {
			continue
		}
		return fmt.Errorf("env name %q must contain only ASCII letters, digits, or underscore", value)
	}
	return nil
}

func isStableToken(value string) bool {
	if value == "" || !isASCIIAlnum(value[0]) {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if isASCIIAlnum(character) || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func isASCIIAlnum(character byte) bool {
	return (character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9')
}
