package durable

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

const testRouteRequestHash = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func testLockedHostRelation(t *testing.T) lock.LockedSubjectContract {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"codex.plugin-carrier",
		"documents",
	)
	if err != nil {
		t.Fatal(err)
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"documents@openai-primary-runtime",
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey("documents")
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := entity.New(entity.KindExtension, "documents")
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lock.NewDelegatedRelationCarrierContract(
		entityID,
		carrier,
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	return locked
}
