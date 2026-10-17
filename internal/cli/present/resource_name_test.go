package clipresent

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/findings"
)

func TestPublicResourceNamesPreserveLiteralKeysAndCanonicalIDs(t *testing.T) {
	for _, test := range []struct {
		kind entity.Kind
		name string
	}{
		{entity.KindInstructions, "instruction"},
		{entity.KindSkill, "skill"},
		{entity.KindHook, "hook"},
		{entity.KindHookAsset, "hook_asset"},
		{entity.KindMCPServer, "mcp-server"},
		{entity.KindExtension, "extension"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			const key = "instructions/mcp_server:%25"
			id, err := entity.New(test.kind, key)
			if err != nil {
				t.Fatal(err)
			}
			canonical := id.String()
			if ResourceKindName(test.kind) != test.name || ResourceReference(id.Kind(), id.Name()) != test.name+"/"+key {
				t.Fatalf("public resource changed its name or key: %q", ResourceReference(id.Kind(), id.Name()))
			}
			roundTrip, err := entity.Parse(canonical)
			if err != nil || roundTrip != id || id.String() != canonical {
				t.Fatalf("canonical identity changed: id=%v parsed=%v err=%v", id, roundTrip, err)
			}
		})
	}
}

func TestResourceLessDiagnosticKeepsItsExistingAbsenceEncoding(t *testing.T) {
	rows := planJSONDiagnostics([]findings.Diagnostic{{Code: "no-entity"}})
	if len(rows) != 1 || rows[0].ResourceID != "/" || rows[0].Resource.Kind != "" || rows[0].Resource.Name != "" {
		t.Fatalf("resource-less diagnostic changed representation: %#v", rows)
	}
}
