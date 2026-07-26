package observe

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
)

func TestManagedPathEvidenceExcludesInvalidAuthorityShapes(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "skill.project.agents", "skill:oracle")
	if err != nil {
		t.Fatal(err)
	}
	contentHash := artifact.HashFileContent([]byte("oracle"))
	evidence, err := NewManagedPathEvidence(subject, ".agents/skills/oracle", true, contentHash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Exists() || evidence.Subject() != subject || evidence.ContentHash() != contentHash {
		t.Fatalf("evidence = %#v", evidence)
	}
	if _, err := NewManagedPathEvidence(subject, ".agents/skills/oracle", false, artifact.HashFileContent([]byte("stale")), 0); err == nil {
		t.Fatal("absent evidence accepted a stale content hash")
	}
	for _, destination := range []string{"../escape", "/absolute/escape", "@cache/escape", ".agents//skills"} {
		if _, err := NewManagedPathEvidence(subject, output.Destination(destination), false, "", 0); err == nil {
			t.Fatalf("evidence accepted malformed destination %q", destination)
		}
	}
	for _, malformed := range []artifact.ContentHash{
		"",
		"sha256:short",
		artifact.ContentHash("SHA256:" + string(artifact.HashFileContent([]byte("uppercase")))[len("sha256:"):]),
	} {
		if _, err := NewManagedPathEvidence(subject, ".agents/skills/oracle", true, malformed, 0); err == nil {
			t.Fatalf("present evidence accepted malformed content hash %q", malformed)
		}
	}
}
