package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func requireDocumentImageReference(t *testing.T, record recoveryDocumentImage) recordedDocumentImage {
	t.Helper()
	image, err := documentImageReference(record)
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func TestDocumentImageReadPlanFreezesWitnessAndReturnsModeCopies(t *testing.T) {
	content := []byte("payload\n")
	mode := recovery.NewPermissionMode(0o640)
	record, err := persistedDocumentImage(aggregate.ExistingDocument(content), mode, "images/before")
	if err != nil {
		t.Fatal(err)
	}
	image := requireDocumentImageReference(t, record)
	*record.Mode, *record.Artifact.Size, record.Artifact.Path = 0o777, 999, "images/other"
	plan, err := image.readPlan(4096)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Exists() || plan.Reference() != "images/before" || plan.Size() != int64(len(content)) {
		t.Fatal("read plan aliases mutable wire evidence")
	}
	loaded, err := plan.loadedImage(content, 0o600)
	if err != nil || loaded.validate() != nil || !loaded.Document().Equal(aggregate.ExistingDocument(content)) || *loaded.Mode() != 0o640 {
		t.Fatalf("frozen witness did not admit its exact private payload: %v", err)
	}
	copy := loaded.Mode()
	*copy = 0
	if *loaded.Mode() != 0o640 {
		t.Fatal("loaded mode query exposes writable witness state")
	}
	if pair, err := loadedDocumentImagePair(loaded, loaded); err != nil || !pair.Original().Document().Equal(pair.Expected().Document()) {
		t.Fatal("actual complete images could not form a pair", err)
	}
	if pair, err := loadedDocumentImagePair(loaded, loadedDocumentImage{}); err == nil || pair.original.validate() == nil || pair.expected.validate() == nil {
		t.Fatal("incomplete image pair retained a usable prefix")
	}

	if value, err := plan.loadedImage([]byte("changed\n"), 0o600); err == nil || value.validate() == nil {
		t.Fatal("read plan accepted bytes outside its witness")
	}
	if value, err := plan.loadedImage(content, 0o644); err == nil || value.validate() == nil {
		t.Fatal("read plan accepted nonprivate evidence mode")
	}
	if value, err := plan.absentImage(); err == nil || value.validate() == nil {
		t.Fatal("file read plan produced absence evidence")
	}
}

func TestDocumentImageReadPlanRejectsZeroAndKeepsExplicitAbsence(t *testing.T) {
	if _, err := (recordedDocumentImage{}).readPlan(4096); err == nil {
		t.Fatal("zero reference supplied a read plan")
	}
	zero := recordedDocumentImage{}
	if value, err := zero.absentImage(); err == nil || value.validate() == nil {
		t.Fatal("zero read plan supplied absence")
	}
	if value, err := zero.loadedImage(nil, 0o600); err == nil || value.validate() == nil {
		t.Fatal("zero read plan supplied file evidence")
	}
	absent := requireDocumentImageReference(t, recoveryDocumentImage{Kind: "absent"})
	if _, err := absent.readPlan(0); err == nil {
		t.Fatal("absence bypassed the byte-envelope check")
	}
	plan, err := absent.readPlan(4096)
	if err != nil || plan.Exists() {
		t.Fatalf("explicit absence became file evidence: %v", err)
	}
	loaded, err := plan.absentImage()
	if err != nil || loaded.validate() != nil || loaded.Document().Exists() || loaded.Mode() != nil {
		t.Fatalf("explicit absence lost its initialized state: %v", err)
	}
	if value, err := plan.loadedImage(nil, 0o600); err == nil || value.validate() == nil {
		t.Fatal("absence read plan supplied a file")
	}
}
