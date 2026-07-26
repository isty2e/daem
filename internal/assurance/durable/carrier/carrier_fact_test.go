package carrier

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/topology"
)

func TestCarrierFactKeyRejectsZeroAndForgedValues(t *testing.T) {
	if err := (CarrierFactKey{}).Validate(); err == nil {
		t.Fatal("zero carrier fact key is valid")
	}

	relation, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"claude-code.plugin-carrier",
		"context7",
	)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"mcp-server.project.claude-code",
		"context7",
	)
	if err != nil {
		t.Fatal(err)
	}
	absoluteStatefile := filepath.Join(t.TempDir(), "state.json")

	for name, key := range map[string]CarrierFactKey{
		"relative statefile": {
			statefileKey:    ".daem/state.json",
			relationSubject: relation,
		},
		"non-relation subject": {
			statefileKey:    absoluteStatefile,
			relationSubject: projection,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := key.Validate(); err == nil {
				t.Fatalf("forged carrier fact key is valid: %#v", key)
			}
		})
	}

	canonical := CarrierFactKey{
		statefileKey:    absoluteStatefile,
		relationSubject: relation,
	}
	if err := canonical.Validate(); err != nil {
		t.Fatalf("canonical carrier fact key is invalid: %v", err)
	}
}
