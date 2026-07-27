package mcp_test

import (
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestDependencyIdentityRoundTripsOnlyItsCanonicalNamespace(t *testing.T) {
	tests := []struct {
		name      string
		construct func(string) (topology.SubjectID, error)
		extract   func(topology.SubjectID) (string, bool)
		value     string
		kind      topology.SubjectKind
		namespace string
	}{
		{name: "executable", construct: topologymcp.ExecutableSubject, extract: executableValue, value: "npx", kind: topology.SubjectRuntimeDependency, namespace: "executable"},
		{name: "absolute executable", construct: topologymcp.ExecutableSubject, extract: executableValue, value: "/opt/example/bin/codegraph", kind: topology.SubjectRuntimeDependency, namespace: "executable.path"},
		{name: "environment", construct: topologymcp.EnvironmentReferenceSubject, extract: topologymcp.EnvironmentReferenceName, value: "HOST_TOKEN", kind: topology.SubjectCredentialReference, namespace: "env"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			subject, err := test.construct(test.value)
			if err != nil {
				t.Fatalf("construct returned error: %v", err)
			}
			if subject.Kind() != test.kind || subject.Namespace() != test.namespace || subject.Key() != test.value {
				t.Fatalf("subject = (%s, %q, %q)", subject.Kind(), subject.Namespace(), subject.Key())
			}
			if value, ok := test.extract(subject); !ok || value != test.value {
				t.Fatalf("extract(%s) = (%q, %t)", subject, value, ok)
			}
			wrongNamespace, err := topology.NewSubjectID(test.kind, "other", test.value)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := test.extract(wrongNamespace); ok {
				t.Fatalf("extract admitted wrong namespace %s", wrongNamespace)
			}
		})
	}
}

func executableValue(subject topology.SubjectID) (string, bool) {
	command, ok := topologymcp.ExecutableReference(subject)
	if !ok {
		return "", false
	}
	return command.Executable(), true
}

func TestAbsoluteExecutableIdentityPreservesResolution(t *testing.T) {
	const absolutePath = "/opt/example/bin/codegraph"
	subject, err := topologymcp.ExecutableSubject(absolutePath)
	if err != nil {
		t.Fatalf("ExecutableSubject returned error: %v", err)
	}
	command, ok := topologymcp.ExecutableReference(subject)
	if !ok ||
		command.Resolution() != desiredmcp.CommandResolutionAbsolutePath ||
		command.Executable() != absolutePath {
		t.Fatalf("ExecutableReference(%s) = (%#v, %t)", subject, command, ok)
	}
}

func TestDependencyIdentityRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name      string
		construct func(string) (topology.SubjectID, error)
		values    []string
	}{
		{name: "executable", construct: topologymcp.ExecutableSubject, values: []string{"", "./bin/node", "/bin/../bin/node", "node --flag", "node;rm"}},
		{name: "environment", construct: topologymcp.EnvironmentReferenceSubject, values: []string{"", "9TOKEN", "BAD-NAME", "TOKEN\n"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range test.values {
				if subject, err := test.construct(value); err == nil {
					t.Fatalf("construct(%q) = %s, want error", value, subject)
				}
			}
		})
	}
}
