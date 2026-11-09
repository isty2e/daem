package document_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestSelectedCandidateCompletesEveryArrayBeforeCompilingComparison(t *testing.T) {
	beforeText := `{"model":"before","left":[0,1,2,3],"right":["y","x"],"outside":true}`
	before := aggregate.ExistingDocument([]byte(beforeText))
	mode := recovery.PermissionMode(0o640)
	input := document.SelectedPreparationInput{
		Before: before, BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}
	left := candidateArrayEdit(t, before, "left", []int{1}, []int{2, 1, 0})
	right := candidateArrayEdit(t, before, "right", nil, []int{1, 0})
	edits := []compose.ProjectionEdit{candidateNativeEdit(t, before, "model", `"after"`)}
	sequences := []configdocument.JSONArrayEdit{right, left}
	prepared, err := document.PrepareSelected(t.Context(), input, edits, sequences)
	want := `{"model":"after","left":[3,2,0],"right":["x","y"],"outside":true}`
	if err != nil {
		t.Fatal("completion lost a selected array, positional removal or native edit", err)
	}
	assertRestorationValue(t, prepared.Document(), "model", `"after"`)
	assertRestorationValue(t, prepared.Document(), "outside", "true")
	loaded, err := configdocument.Parse(t.Context(), prepared.Document().Content(), configdocument.JSON, input.Limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, sequence := range sequences {
		if matches, err := sequence.MatchesExpected(t.Context(), loaded); err != nil || !matches {
			t.Fatal("completion did not retain the exact expected selected array", err)
		}
	}
	images := document.ComparisonInput{
		Before: before, Expected: prepared.Document(), BeforeMode: &mode, ExpectedMode: &mode, Limits: input.Limits,
	}
	replayed, err := document.ReconstructSelected(t.Context(), images, []compose.Participant{edits[0].Participant()}, []configdocument.JSONArrayEdit{left, right})
	if err != nil || prepared.Comparison().RequireCanonicalRecoveryPolicy(t.Context(), replayed.Comparison()) != nil {
		t.Fatal("reconstruction changed the complete selected policy", err)
	}
	for _, current := range []string{
		want,
		`{"model":"before","left":[3,2,0],"right":["y","x"],"outside":true}`,
		`{"model":"before","left":[0,1,2,3],"right":["x","y"],"outside":true}`,
		`{"model":"after","left":[0,1,2,3],"right":["y","x"],"outside":true}`,
	} {
		current = string(bytes.Replace([]byte(current), []byte(`"outside":true`), []byte(`"outside":false`), 1))
		restored, err := replayed.Comparison().RestoreBefore(t.Context(), aggregate.ExistingDocument([]byte(current)), &mode)
		if err != nil {
			t.Fatal("different-length array restoration lost a mixed phase or outside edit", err)
		}
		assertRestorationValue(t, restored.Document(), "model", `"before"`)
		assertRestorationValue(t, restored.Document(), "outside", "false")
		loaded, err := configdocument.Parse(t.Context(), restored.Document().Content(), configdocument.JSON, input.Limits)
		if err != nil {
			t.Fatal(err)
		}
		for _, sequence := range sequences {
			if matches, err := sequence.MatchesBefore(t.Context(), loaded); err != nil || !matches {
				t.Fatal("restoration did not recover the exact original selected array", err)
			}
		}
		again, err := replayed.Comparison().RestoreBefore(t.Context(), restored.Document(), &mode)
		if err != nil || again.Changed() {
			t.Fatal("completed restoration did not converge", err)
		}
	}
	intermediate := aggregate.ExistingDocument([]byte(`{"model":"after","left":[0,2,3],"right":["x","y"],"outside":true}`))
	if match, err := replayed.Comparison().Match(t.Context(), intermediate, &mode); err != nil || match.Recoverable() {
		t.Fatal("surviving-row baseline became a third recovery phase", err)
	}
	if rejected, err := replayed.Comparison().RestoreBefore(t.Context(), intermediate, &mode); err == nil || rejected.Validate() == nil {
		t.Fatal("recovery overwrote an intermediate array", err)
	}
	for _, incomplete := range [][]configdocument.JSONArrayEdit{nil, {left}, {right}, {left, left, right}} {
		if rejected, err := document.ReconstructSelected(t.Context(), images, []compose.Participant{edits[0].Participant()}, incomplete); err == nil || rejected.Comparison().Validate() == nil {
			t.Fatal("incomplete or overlapping array evidence returned a comparison", err)
		}
	}
	images.Expected = aggregate.ExistingDocument([]byte(`{"model":"after","left":[3,2,0],"right":["x","y"],"outside":false}`))
	if rejected, err := document.ReconstructSelected(t.Context(), images, []compose.Participant{edits[0].Participant()}, sequences); err == nil || rejected.Comparison().Validate() == nil {
		t.Fatal("reconstruction accepted unselected expected content", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if rejected, err := document.PrepareSelected(cancelled, input, edits, sequences); !errors.Is(err, context.Canceled) || rejected.Comparison().Validate() == nil {
		t.Fatal("canceled completion returned a partial candidate", err)
	}
}

func TestSelectedPreparationRejectsOverlapsAndAlreadyEditedBaselines(t *testing.T) {
	before := aggregate.ExistingDocument([]byte(`{"left":[0,1,2],"model":"before"}`))
	sequence := candidateArrayEdit(t, before, "left", []int{1}, []int{1, 0})
	mode := recovery.PermissionMode(0o600)
	input := document.SelectedPreparationInput{
		Before: before, BeforeMode: &mode, ExpectedMode: &mode,
		Limits: configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32},
	}
	for _, sequences := range [][]configdocument.JSONArrayEdit{{sequence, sequence}, {{}}} {
		if rejected, err := document.PrepareSelected(t.Context(), input, nil, sequences); err == nil || rejected.Comparison().Validate() == nil {
			t.Fatal("duplicate or invalid selected array returned a candidate", err)
		}
	}
	overlap := candidateNativeEdit(t, before, "left", `[2,0]`)
	if rejected, _, err := document.PrepareSelectedImage(t.Context(), before, input.Limits, []compose.ProjectionEdit{overlap}, []configdocument.JSONArrayEdit{sequence}); err == nil || rejected.Exists() || rejected.ContentSize() != 0 {
		t.Fatal("compose and sequence channels shared a rewrite footprint", err)
	}
	alreadyExpected := aggregate.ExistingDocument([]byte(`{"left":[2,0],"model":"before"}`))
	if rejected, _, err := document.PrepareSelectedImage(t.Context(), alreadyExpected, input.Limits, nil, []configdocument.JSONArrayEdit{sequence}); err == nil || rejected.Exists() || rejected.ContentSize() != 0 {
		t.Fatal("new preparation accepted an already-edited array as its baseline", err)
	}
}

func TestSelectedCandidateReconstructsNativeMCPWithoutAuthorizingForeignRows(t *testing.T) {
	mode := recovery.PermissionMode(0o600)
	before := "# keep\nmodel='before'\n[mcp_servers.foreign]\ncommand='keep'\n"
	expected := "# keep\nmodel='after'\n[mcp_servers.foreign]\ncommand='keep'\n[mcp_servers.search]\ncommand='node'\n"
	participants := restorationMCPParticipants(t)
	prepared, err := document.ReconstructSelected(t.Context(), comparisonInput(before, expected, &mode, &mode), participants, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{
		"# keep\nmodel='before'\n[mcp_servers.foreign]\ncommand='keep'\n[mcp_servers.search]\ncommand='node'\n",
		"# keep\nmodel='after'\n[mcp_servers.foreign]\ncommand='keep'\n",
	} {
		restored, err := prepared.Comparison().RestoreBefore(t.Context(), aggregate.ExistingDocument([]byte(current)), &mode)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := configdocument.Parse(t.Context(), restored.Document().Content(), configdocument.TOML, comparisonInput(before, expected, &mode, &mode).Limits)
		if err != nil {
			t.Fatal(err)
		}
		key, err := output.NewKeyPath("model")
		if err != nil {
			t.Fatal(err)
		}
		value, present, err := parsed.Lookup(key)
		if err != nil || !present || !value.Equal(expectedRestorationValue(t, `"before"`)) {
			t.Fatalf("native/MCP mixture failed to restore the actual native value: %v", err)
		}
	}
	bad, err := document.ReconstructSelected(t.Context(), comparisonInput(before, expected+"outside=true\n", &mode, &mode), participants, nil)
	if err == nil || bad.Comparison().Validate() == nil {
		t.Fatal("unexpected MCP content returned a usable comparison")
	}
}

func candidateArrayEdit(t *testing.T, before aggregate.Document, field string, removed, positions []int) configdocument.JSONArrayEdit {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	loaded, err := configdocument.Parse(t.Context(), before.Content(), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	path, err := output.NewKeyPath(field)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := configdocument.EditJSONArray(t.Context(), loaded, path, removed, positions)
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := configdocument.NewJSONArrayEdit(t.Context(), loaded, expected, path, removed, positions)
	if err != nil {
		t.Fatal(err)
	}
	return sequence
}

func candidateNativeEdit(t *testing.T, before aggregate.Document, name, desired string) compose.ProjectionEdit {
	t.Helper()
	participant := comparisonParticipant(t, name)
	codec := comparisonNativeCodec(t, configdocument.JSON)
	contract := participant.Selection().Contracts()[0]
	canonical, err := expectedRestorationValue(t, desired).CanonicalContext(t.Context(), configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	body, err := aggregate.NewManagedContribution(contract, canonical)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "preferences")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeProject, contract.Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	baseline, failure := codec.Read(before, participant.Selection())
	if failure != nil {
		t.Fatal(failure)
	}
	intent, err := aggregate.NewProjectionIntent(baseline.States()[0], &set)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(baseline, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, configdocument.JSON, []output.KeyPath{contract.Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	return edit
}
