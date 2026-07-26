package resource

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
)

func TestSubjectUsesCanonicalDesiredEntityIdentity(t *testing.T) {
	tests := []struct {
		kind entity.Kind
		want string
	}{
		{kind: entity.KindSkill, want: "resource/skill/review"},
		{kind: entity.KindHook, want: "resource/hook/review"},
		{kind: entity.KindHookAsset, want: "resource/hook_asset/review"},
		{kind: entity.KindInstructions, want: "resource/instructions/review"},
		{kind: entity.KindMCPServer, want: "resource/mcp_server/review"},
		{kind: entity.KindExtension, want: "resource/extension/review"},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			id, err := entity.New(test.kind, "review")
			if err != nil {
				t.Fatal(err)
			}
			subject, err := Subject(id)
			if err != nil {
				t.Fatal(err)
			}
			if got := subject.String(); got != test.want {
				t.Fatalf("subject = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSubjectPreservesEntityCollisionDomainsAndCanonicalEscaping(t *testing.T) {
	skillID, err := entity.New(entity.KindSkill, "review/tools 한글")
	if err != nil {
		t.Fatal(err)
	}
	hookID, err := entity.New(entity.KindHook, "review/tools 한글")
	if err != nil {
		t.Fatal(err)
	}
	skillSubject, err := Subject(skillID)
	if err != nil {
		t.Fatal(err)
	}
	hookSubject, err := Subject(hookID)
	if err != nil {
		t.Fatal(err)
	}
	if skillSubject == hookSubject {
		t.Fatal("different Desired families collided in one resource namespace")
	}
	if got, want := skillSubject.String(), "resource/skill/review%2Ftools%20%ED%95%9C%EA%B8%80"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

func TestSubjectRejectsInvalidStructuralEntityKeys(t *testing.T) {
	if _, err := Subject(entity.ID{}); err == nil {
		t.Fatal("expected zero entity rejection")
	}
	for _, name := range []string{" review", "review ", "review\u202Ehidden"} {
		id, err := entity.New(entity.KindSkill, name)
		if err != nil {
			t.Fatalf("entity.New(%q) unexpectedly failed before Topology ingress: %v", name, err)
		}
		if _, err := Subject(id); err == nil {
			t.Fatalf("Subject accepted structurally invalid entity name %q", name)
		}
	}
}
