package qualification_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestPublishedNativeJournalReadsChargeActualImagesAndPreserveRollback(t *testing.T) {
	fixture := nativeJournalFixture(t)
	remaining := fixture.read.Budget.RemainingBytes()
	loaded, err := journalqualification.LoadDocuments(fixture.journal, t.Context(), fixture.read)
	if err != nil || loaded.Len() != 1 {
		t.Fatal("published record did not reconstruct one complete group", err)
	}
	document, present := loaded.At(0)
	if !present || len(document.Claims().After()) != 1 {
		t.Fatal("complete document facts were lost")
	}
	wantBytes := fixture.comparison.BeforeDocument().ContentSize() + fixture.comparison.ExpectedDocument().ContentSize()
	if remaining-fixture.read.Budget.RemainingBytes() != wantBytes {
		t.Fatal("the whole image pair did not use the shared physical byte allocation")
	}
	mode := recovery.NewPermissionMode(0o600)
	current := aggregate.ExistingDocument([]byte(`{"model":"fast","outside":"fresh"}`))
	restored, err := document.Group().Comparison().RestoreBefore(t.Context(), current, mode)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || string(values["model"]) != "true" || string(values["outside"]) != `"fresh"` {
		t.Fatal("record-backed rollback changed foreign data or lost before values", err)
	}
	if retry, err := document.Group().Comparison().RestoreBefore(t.Context(), restored.Document(), mode); err != nil || !retry.Document().Equal(restored.Document()) {
		t.Fatal("record-backed restore is not retry-safe", err)
	}
}

func TestPublishedNativeJournalRefusesChangedPrivateImageWithoutPrefix(t *testing.T) {
	fixture := nativeJournalFixture(t)
	session, err := fixture.journal.BeginLoadedDocuments(t.Context(), fixture.read.Limits, fixture.read.Codecs)
	if err != nil {
		t.Fatal(err)
	}
	demand, present, err := session.Next(t.Context())
	if err != nil || !present {
		t.Fatal("fixture has no image demand", err)
	}
	reference, err := demand.Expected().FileReference()
	if err != nil {
		t.Fatal(err)
	}
	root, err := fixture.read.Artifacts.Authority()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.PhysicalRoot(), filepath.FromSlash(reference)), []byte(`{"model":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := journalqualification.LoadDocuments(fixture.journal, t.Context(), fixture.read)
	if err == nil || loaded.Len() != 0 {
		t.Fatal("changed private expected payload returned compiled reconstruction")
	}
}

func TestPublishedNativeJournalCancellationReturnsNoGroup(t *testing.T) {
	fixture := nativeJournalFixture(t)
	remaining := fixture.read.Budget.RemainingBytes()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	loaded, err := journalqualification.LoadDocuments(fixture.journal, ctx, fixture.read)
	if !errors.Is(err, context.Canceled) || loaded.Len() != 0 {
		t.Fatal("canceled reconstruction returned a group", err)
	}
	if fixture.read.Budget.RemainingBytes() != remaining {
		t.Fatal("canceled reconstruction consumed image bytes")
	}
}
