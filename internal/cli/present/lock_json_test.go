package clipresent

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/realization"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestJSONRealizationKeepsDelegatedAndAggregateFieldAxesSeparate(t *testing.T) {
	subjectKey, err := hostrelation.NewSubjectKey("review")
	if err != nil {
		t.Fatal(err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("codex-plugin:review")
	if err != nil {
		t.Fatal(err)
	}
	expectedRelation, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatal(err)
	}
	realization, err := realization.NewDelegatedRelation(realization.DelegatedRelationInput{
		PlacementID:            "codex-plugin-global",
		Target:                 target.TargetCodex,
		Scope:                  target.ScopeGlobal,
		SourceNamespace:        "marketplace:example",
		ExpectedRelation:       expectedRelation,
		RouteID:                "codex.plugin.install",
		RouteContractVersion:   "codex-plugin-v1",
		CanonicalRequestHash:   "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		VerifiedRelationFields: []string{"name", "marketplace"},
	})
	if err != nil {
		t.Fatal(err)
	}

	projected := jsonRealizationFor(realization)
	if projected.RouteContractVersion != "codex-plugin-v1" ||
		!reflect.DeepEqual(projected.VerifiedRelationFields, []string{"marketplace", "name"}) {
		t.Fatalf("delegated JSON realization = %#v", projected)
	}
	if !projected.SourceNamespaceRedacted ||
		!projected.RelationSubjectKeyRedacted ||
		!projected.ManagedInstanceKeyRedacted {
		t.Fatalf("unclassified delegated identity was not redacted: %#v", projected)
	}
	if projected.AdapterContractVersion != "" || len(projected.ComparedFields) != 0 {
		t.Fatalf("delegated JSON realization leaked aggregate fields: %#v", projected)
	}
}

func TestJSONRealizationPreservesExplicitExactPermissionModeZero(t *testing.T) {
	exactMode, err := realization.NewExactPathPermissionMode(0)
	if err != nil {
		t.Fatal(err)
	}
	realization, err := realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID: "future.project.exact", ConsumerTargets: []target.Target{target.TargetCodex},
		Scope: target.ScopeProject, Destination: outputtest.Parse(t, ".daem/future-exact"), ContentKind: realization.PathProjectionFile,
		PlacementMode: realization.PathProjectionCopy, PermissionPolicy: realization.PathPermissionsExact,
		ExactPermissionMode: exactMode, AdapterContractVersion: "future-exact-v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	projected := jsonRealizationFor(realization)
	if projected.ExactPermissionMode == nil || *projected.ExactPermissionMode != 0 {
		t.Fatalf("exact permission mode = %#v, want explicit zero", projected.ExactPermissionMode)
	}
}
