package record

import (
	"encoding/json"
	"path/filepath"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestNativeRemovalWireRoundtripAndSingularShapeRefusal(t *testing.T) {
	fixture := nativeCreateFixture(t)
	group := fixture.record.DocumentGroups[0]
	content, err := json.Marshal(group)
	if err != nil {
		t.Fatal(err)
	}
	var decoded recoveryDocumentGroup
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	actual, err := decoded.Removal.canonical()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := group.Removal.canonical()
	if err != nil || !actual.Demands().Equal(expected.Demands()) {
		t.Fatalf("wire discarded logical relations: %v", err)
	}
	physical, err := expected.PhysicalIntent()
	if err != nil {
		t.Fatal(err)
	}
	legacyShape, err := persistedRecoveryRemovalIntents([]recovery.RemovalIntent{physical})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	fields["removal"] = nativeRecordJSON(t, legacyShape[0])
	if err := json.Unmarshal(nativeRecordJSON(t, fields), &decoded); err == nil {
		t.Fatal("native group silently reinterpreted the obsolete one-relation shape")
	}
}

func TestNativeRemovalChecksActualImageHashAndMode(t *testing.T) {
	fixture := nativeJournalFixture(t)
	fixture.withImages(t, fixture.original, aggregate.AbsentDocument())
	group := fixture.record.DocumentGroups[0]
	group.Extent = "whole"
	comparison, err := journaldocument.NewWholeComparison(t.Context(), journaldocument.ComparisonInput{Before: fixture.original, BeforeMode: group.Original.Mode, Expected: aggregate.AbsentDocument(), Limits: fixture.limits})
	if err != nil {
		t.Fatal(err)
	}
	images, err := preparedNativeDocumentImages(t.Context(), native16GroupBody(group), comparison, fixture.limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := group.validateRemovalImages(images); err != nil {
		t.Fatal("unchanged removal does not match its actual images", err)
	}
	for _, change := range []string{"hash", "mode", "phase", "missing", "namespace"} {
		t.Run(change, func(t *testing.T) {
			bad := group
			copy := *group.Removal
			copy.Demands = append([]recoveryDocumentRemovalDemand(nil), copy.Demands...)
			copy.States = append([]recoveryRemovalState(nil), copy.States...)
			before := *copy.States[0].Before
			copy.States[0].Before = &before
			bad.Removal = &copy
			switch change {
			case "hash":
				before.ContentHash = string(artifact.HashFileContent([]byte("different")))
			case "mode":
				before.PathMode = recovery.NewPermissionMode(0o700)
			case "phase":
				copy.States[0] = recoveryRemovalState{ExpectedAfter: &recoveryExpectedPathDTO{Existed: true, Kind: recovery.PathKindFile, ContentHash: before.ContentHash, PathMode: before.PathMode}}
			case "missing":
				bad.Removal = nil
			case "namespace":
				parent := *copy.Namespace.ParentProvenance
				parent.PhysicalRoot = filepath.Dir(parent.PhysicalRoot)
				copy.Namespace.ParentProvenance = &parent
			}
			if err := bad.validateRemovalImages(images); err == nil {
				t.Fatal("invalid physical removal basis admitted")
			}
		})
	}
}
