package document_test

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
)

func sequencePermutation(t *testing.T, before, expected string, field string) configdocument.JSONArrayPermutation {
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
	sequence, err := configdocument.NewJSONArrayPermutation(t.Context(), left, right, path)
	if err != nil {
		t.Fatal(err)
	}
	return sequence
}

func TestNativeComparisonRetainsDisjointArraysInOnePhysicalDocument(t *testing.T) {
	before := "{\n // keep\n \"outside\":1,\"packages\":[\"a\",\"b\"],\"plugin\":[\"x\",\"y\"]\n}"
	expected := "{\n // keep\n \"outside\":1,\"packages\":[\"b\",\"a\"],\"plugin\":[\"y\",\"x\"]\n}"
	packages := sequencePermutation(t, before, expected, "packages")
	plugins := sequencePermutation(t, before, expected, "plugin")
	mode := recovery.PermissionMode(0o600)
	input := comparisonInput(before, expected, &mode, &mode)
	comparison, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayPermutation{plugins, packages})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayPermutation{packages, plugins})
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
	if _, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayPermutation{plugins, plugins}); err == nil {
		t.Fatal("duplicate array selections acquired independent recovery state")
	}
}

func TestCanonicalSequencePolicyIncludesTheSelectedNativeArray(t *testing.T) {
	before := `{"packages":["a","b"],"plugin":["a","b"]}`
	expected := `{"packages":["b","a"],"plugin":["b","a"]}`
	mode := recovery.PermissionMode(0o600)
	input := comparisonInput(before, expected, &mode, &mode)
	left, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayPermutation{sequencePermutation(t, before, expected, "packages")})
	if err != nil {
		t.Fatal(err)
	}
	right, err := document.NewSelectedComparison(t.Context(), input, nil, []configdocument.JSONArrayPermutation{sequencePermutation(t, before, expected, "plugin")})
	if err != nil {
		t.Fatal(err)
	}
	if err := left.RequireCanonicalRecoveryPolicy(t.Context(), right); err == nil {
		t.Fatal("equal row bytes erased the native array's identity")
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
	neutral := comparisonOrder(t, string(beforeBytes), string(expectedBytes))
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
