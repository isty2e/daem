// Package mcp owns MCP structural subject identities and the pure lowering
// from canonical Desired MCP bindings into topology.
package mcp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/topology"
)

const (
	executableNamespace  = "executable"
	environmentNamespace = "env"
)

// ExecutableSubject returns the structural identity of an ambient command lookup.
func ExecutableSubject(command string) (topology.SubjectID, error) {
	if err := validateAmbientExecutableCommand(command); err != nil {
		return topology.SubjectID{}, err
	}
	return topology.NewSubjectID(topology.SubjectRuntimeDependency, executableNamespace, command)
}

// ExecutableCommand returns the ambient command represented by subject.
func ExecutableCommand(subject topology.SubjectID) (string, bool) {
	if subject.Kind() != topology.SubjectRuntimeDependency || subject.Namespace() != executableNamespace {
		return "", false
	}
	if err := validateAmbientExecutableCommand(subject.Key()); err != nil {
		return "", false
	}
	return subject.Key(), true
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

func validateAmbientExecutableCommand(command string) error {
	if command == "" {
		return fmt.Errorf("ambient executable command is required")
	}
	if filepath.IsAbs(command) || strings.ContainsAny(command, "/\\ \t\n\r;&|$`") {
		return fmt.Errorf("ambient executable command %q must be a portable command token", command)
	}
	if !isStableToken(command) {
		return fmt.Errorf("ambient executable command %q must be a stable command token", command)
	}
	return nil
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
