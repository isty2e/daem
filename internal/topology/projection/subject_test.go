package projection

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/topology"
)

func TestSubjectUsesPlacementCollisionDomainAndCanonicalEntityKey(t *testing.T) {
	id, err := entity.New(entity.KindSkill, "review/audit")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := Subject(id, "skill.project.agents")
	if err != nil {
		t.Fatalf("Subject returned error: %v", err)
	}
	if subject.Kind() != topology.SubjectProjection || subject.Namespace() != "skill.project.agents" ||
		subject.Key() != id.String() {
		t.Fatalf("subject = %q", subject)
	}
	parsedEntity, ok := EntityID(subject)
	if !ok || parsedEntity != id {
		t.Fatalf("EntityID = %q, %t", parsedEntity, ok)
	}
	parsed, err := topology.ParseSubjectID(subject.String())
	if err != nil || parsed != subject {
		t.Fatalf("round trip = %q, %v", parsed, err)
	}
}

func TestEntityIDRejectsNonEntityProjectionKeys(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "codex.project.mcp-server", "context7")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := EntityID(subject); ok {
		t.Fatalf("EntityID accepted non-entity projection %q", subject)
	}
}

func TestSubjectRejectsInvalidPlacementAndEntityFacts(t *testing.T) {
	id, err := entity.New(entity.KindSkill, "oracle")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Subject(id, "bad placement"); err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("placement error = %v", err)
	}
	if _, err := Subject(entity.ID{}, "skill.project.agents"); err == nil || !strings.Contains(err.Error(), "entity") {
		t.Fatalf("entity error = %v", err)
	}
}
