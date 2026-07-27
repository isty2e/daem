package live

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/outputtest"
)

func TestManagedPathEvidenceObservesCanonicalFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "AGENTS.md", "instructions\n")
	writeTestFile(t, root, "skills/oracle/SKILL.md", "skill\n")

	requests := []ManagedPathRequest{
		managedPathTestRequest(t, "file", outputtest.Parse(t, "AGENTS.md"), realization.PathProjectionFile),
		managedPathTestRequest(t, "directory", outputtest.Parse(t, "skills/oracle"), realization.PathProjectionDirectory),
	}
	evidence, err := ManagedPathEvidence(context.Background(), managedPathTestResolver(root), requests)
	if err != nil {
		t.Fatalf("ManagedPathEvidence returned error: %v", err)
	}
	if len(evidence) != 2 {
		t.Fatalf("evidence = %#v, want two observations", evidence)
	}
	for _, observed := range evidence {
		if !observed.Exists() || observed.ContentHash() == "" {
			t.Fatalf("evidence = %#v, want present exact content", observed)
		}
	}
}

func TestManagedPathEvidenceRejectsCrossKindAndDuplicateRequests(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "AGENTS.md", "instructions\n")
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		requests []ManagedPathRequest
		want     string
	}{
		{
			name: "directory cannot satisfy file request",
			requests: []ManagedPathRequest{
				managedPathTestRequest(t, "file", outputtest.Parse(t, "directory"), realization.PathProjectionFile),
			},
			want: "expected regular file",
		},
		{
			name: "file cannot satisfy directory request",
			requests: []ManagedPathRequest{
				managedPathTestRequest(t, "directory", outputtest.Parse(t, "AGENTS.md"), realization.PathProjectionDirectory),
			},
			want: "expected directory",
		},
		{
			name: "same subject and address cannot change kind",
			requests: []ManagedPathRequest{
				managedPathTestRequest(t, "duplicate", outputtest.Parse(t, "AGENTS.md"), realization.PathProjectionFile),
				managedPathTestRequest(t, "duplicate", outputtest.Parse(t, "AGENTS.md"), realization.PathProjectionDirectory),
			},
			want: "conflicts with duplicate subject/address",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ManagedPathEvidence(context.Background(), managedPathTestResolver(root), test.requests)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ManagedPathEvidence error = %v, want %q", err, test.want)
			}
		})
	}
}

func managedPathTestRequest(
	t *testing.T,
	name string,
	destination output.Destination,
	contentKind realization.PathProjectionContentKind,
) ManagedPathRequest {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "test.managed-path", name)
	if err != nil {
		t.Fatal(err)
	}
	return ManagedPathRequest{Subject: subject, Destination: destination, ContentKind: contentKind}
}

func managedPathTestResolver(root string) DestinationResolver {
	return func(destination output.Destination) (string, error) {
		return filepath.Join(root, filepath.FromSlash(destination.RelativePath())), nil
	}
}
