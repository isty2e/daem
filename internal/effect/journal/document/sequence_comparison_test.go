package document_test

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
)

func sequencePermutation(t *testing.T, before, expected string, field string, positions ...int) configdocument.JSONArrayEdit {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	left, err := configdocument.Parse(t.Context(), []byte(before), configdocument.JSONC, limits)
	if err != nil {
		t.Fatal(err)
	}
	right, err := configdocument.Parse(t.Context(), []byte(expected), configdocument.JSONC, limits)
	if err != nil {
		t.Fatal(err)
	}
	path, err := output.NewKeyPath(field)
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := configdocument.NewJSONArrayEdit(t.Context(), left, right, path, nil, positions)
	if err != nil {
		t.Fatal(err)
	}
	return sequence
}

func TestNativeComparisonRetainsDisjointArraysInOnePhysicalDocument(t *testing.T) {
	before := "{\n // keep\n \"outside\":1,\"packages\":[\"a\",\"b\"],\"plugin\":[\"x\",\"y\"]\n}"
	expected := "{\n // keep\n \"outside\":1,\"packages\":[\"b\",\"a\"],\"plugin\":[\"y\",\"x\"]\n}"
	packages := sequencePermutation(t, before, expected, "packages", 1, 0)
	plugins := sequencePermutation(t, before, expected, "plugin", 1, 0)
	mode := recovery.PermissionMode(0o600)
	input := comparisonInput(before, expected, &mode, &mode)
	comparison, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayEdit{plugins, packages})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayEdit{packages, plugins})
	if err != nil || comparison.RequireCanonicalRecoveryPolicy(t.Context(), canonical) != nil {
		t.Fatal("sequence input order changed canonical comparison policy", err)
	}
	for _, selected := range []string{expected, "{\n // keep\n \"outside\":1,\"packages\":[\"a\",\"b\"],\"plugin\":[\"y\",\"x\"]\n}"} {
		current := bytes.Replace([]byte(selected), []byte(`"outside":1`), []byte(`"outside":2`), 1)
		match, err := comparison.Match(t.Context(), aggregate.ExistingDocument(current), &mode)
		if err != nil || !match.Recoverable() {
			t.Fatal("independent before/expected sequence states became unrecoverable", err)
		}
		restored, err := comparison.RestoreBefore(t.Context(), aggregate.ExistingDocument(current), &mode)
		want := bytes.Replace([]byte(before), []byte(`"outside":1`), []byte(`"outside":2`), 1)
		if err != nil || !bytes.Equal(restored.Document().Content(), want) {
			t.Fatal("array restoration overwrote another selection or outside bytes", err)
		}
	}
	if _, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayEdit{plugins, plugins}); err == nil {
		t.Fatal("duplicate array selections acquired independent recovery state")
	}
	third := aggregate.ExistingDocument([]byte(`{"outside":1,"packages":["a","b"],"plugin":["third","y"]}`))
	if match, err := comparison.Match(t.Context(), third, &mode); err != nil || match.Recoverable() || match.MatchesBefore() || match.MatchesExpected() {
		t.Fatal("an earlier valid array hid a later foreign array state", err)
	}
	if restored, err := comparison.RestoreBefore(t.Context(), third, &mode); err == nil || restored.Validate() == nil {
		t.Fatal("partial array-set evidence allowed restoration over a foreign row")
	}
}

func TestCanonicalSequencePolicyIncludesTheSelectedNativeArray(t *testing.T) {
	before := `{"packages":["a","b"],"plugin":["a","b"]}`
	expected := `{"packages":["b","a"],"plugin":["b","a"]}`
	mode := recovery.PermissionMode(0o600)
	input := comparisonInput(before, expected, &mode, &mode)
	left, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayEdit{sequencePermutation(t, before, expected, "packages", 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	right, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayEdit{sequencePermutation(t, before, expected, "plugin", 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := left.RequireCanonicalRecoveryPolicy(t.Context(), right); err == nil {
		t.Fatal("equal row bytes erased the native array's identity")
	}
}

func TestNativeComparisonKeepsMembershipAndOrderInOneArrayPhase(t *testing.T) {
	before := `{"model":"balanced","outside":1,"plugin":["beta","foreign","retired","alpha"]}`
	expected := `{"model":"fast","outside":1,"plugin":["alpha","foreign","beta"]}`
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	left, err := configdocument.Parse(t.Context(), []byte(before), configdocument.JSONC, limits)
	if err != nil {
		t.Fatal(err)
	}
	right, err := configdocument.Parse(t.Context(), []byte(expected), configdocument.JSONC, limits)
	if err != nil {
		t.Fatal(err)
	}
	path, err := output.NewKeyPath("plugin")
	if err != nil {
		t.Fatal(err)
	}
	edit, err := configdocument.NewJSONArrayEdit(t.Context(), left, right, path, []int{2}, []int{2, 1, 0})
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.PermissionMode(0o600)
	comparison, err := document.NewSelectedComparison(t.Context(), comparisonInput(before, expected, &mode, &mode), []compose.Participant{comparisonParticipant(t, "model")}, []configdocument.JSONArrayEdit{edit})
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{
		`{"model":"balanced","outside":2,"plugin":["alpha","foreign","beta"]}`,
		`{"model":"fast","outside":2,"plugin":["beta","foreign","retired","alpha"]}`,
	} {
		match, err := comparison.Match(t.Context(), aggregate.ExistingDocument([]byte(current)), &mode)
		if err != nil || !match.Recoverable() {
			t.Fatal("disjoint native and sequence phases no longer recover", err)
		}
		restored, err := comparison.RestoreBefore(t.Context(), aggregate.ExistingDocument([]byte(current)), &mode)
		if err != nil {
			t.Fatal(err)
		}
		assertRestorationValue(t, restored.Document(), "model", `"balanced"`)
		assertRestorationValue(t, restored.Document(), "outside", "2")
		loaded, err := configdocument.Parse(t.Context(), restored.Document().Content(), configdocument.JSONC, limits)
		if err != nil {
			t.Fatal(err)
		}
		if original, _, err := edit.Match(t.Context(), loaded); err != nil || !original {
			t.Fatal("mixed-phase recovery lost the exact original array", err)
		}
	}
	planning := aggregate.ExistingDocument([]byte(`{"model":"fast","outside":2,"plugin":["beta","foreign","alpha"]}`))
	match, err := comparison.Match(t.Context(), planning, &mode)
	if err != nil || match.Recoverable() {
		t.Fatal("unpublished membership baseline became a third sequence phase", err)
	}
	if restored, err := comparison.RestoreBefore(t.Context(), planning, &mode); err == nil || restored.Validate() == nil {
		t.Fatal("intermediate membership state was overwritten by recovery")
	}
}

func TestGenericArrayInverseRetainsLegacyPiPermutationEffects(t *testing.T) {
	beforeBytes := []byte("{\n \"packages\":[\"npm:b\", {\"source\":\"./local\",\"extensions\":[\"*.ts\"]}, \"npm:a\"],\"outside\":1\n}")
	before, err := piconfig.Parse(beforeBytes)
	if err != nil {
		t.Fatal(err)
	}
	expectedBytes, _, err := before.PermutePackageRows([]int{2, 1, 0})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := piconfig.Parse(expectedBytes)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := piconfig.NewPackagePermutation(before, expected)
	if err != nil {
		t.Fatal(err)
	}
	neutral := comparisonOrder(t, string(beforeBytes), string(expectedBytes), 2, 1, 0)
	currentBytes := bytes.Replace(expectedBytes, []byte(`"outside":1`), []byte(`"outside":2`), 1)
	current, err := piconfig.Parse(currentBytes)
	if err != nil {
		t.Fatal(err)
	}
	oldRestored, oldChanged, oldErr := legacy.Restore(current)
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	loaded, err := configdocument.Parse(t.Context(), currentBytes, configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	newRestored, newChanged, newErr := neutral.Restore(t.Context(), loaded)
	if oldErr != nil || newErr != nil || oldChanged != newChanged || !bytes.Equal(oldRestored, newRestored.Bytes()) {
		t.Fatal("native16 Pi inverse and neutral array inverse differ", oldErr, newErr)
	}
	foreignBytes := bytes.Replace(currentBytes, []byte(`"*.ts"`), []byte(`"*.js"`), 1)
	foreign, err := piconfig.Parse(foreignBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := legacy.Restore(foreign); err == nil {
		t.Fatal("legacy parity fixture does not discriminate foreign-row changes")
	}
	foreignLoaded, err := configdocument.Parse(t.Context(), foreignBytes, configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	if restored, changed, err := neutral.Restore(t.Context(), foreignLoaded); err == nil || changed || restored.Bytes() != nil {
		t.Fatal("neutral inverse overwrote a foreign row refused by native16")
	}
}
