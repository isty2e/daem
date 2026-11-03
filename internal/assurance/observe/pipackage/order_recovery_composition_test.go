package pipackage_test

import (
	"bytes"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
	"github.com/isty2e/daem/internal/target"
)

func TestPiPermutationAndNativeRestorationKeepPostInstallState(t *testing.T) {
	for _, test := range []struct{ name, content string }{
		{"existing theme", `{"packages":["npm:b@1", {"source":"npm:a@1","opaque":{"n":1e999}}],"outside":"original","theme":"day"}`},
		{"root created by installation", `{"packages":["npm:b@1", {"source":"npm:a@1","opaque":{"n":1e999}}],"outside":"original"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			postInstall := aggregate.ExistingDocument([]byte(test.content))
			baselines := readThemeBaselines(t, postInstall)
			observation := readCompositionOrder(t, postInstall.Content(), true)
			ordered, exists := observation.Candidate()
			if !exists || !observation.Changed() {
				t.Fatal("fixture did not produce a real post-install permutation")
			}
			candidate := composeTheme(t, postInstall, aggregate.ExistingDocument(ordered))
			before := parsedOrderDocument(t, postInstall.Content())
			permutation, err := piconfig.NewPackagePermutation(before, parsedOrderDocument(t, candidate.Content()))
			if err != nil {
				t.Fatal(err)
			}

			preInstall := parsedOrderDocument(t, []byte(`{"packages":["npm:b@1"]}`))
			if _, err := piconfig.NewPackagePermutation(preInstall, parsedOrderDocument(t, candidate.Content())); err == nil {
				t.Fatal("pre-install membership became a direct-permutation baseline")
			}

			current := bytes.Replace(candidate.Content(), []byte(`"outside":"original"`), []byte(`"outside":"fresh"`), 1)
			current = bytes.Replace(current, []byte(`{`), []byte(`{"concurrent":{"keep":true},`), 1)
			current = bytes.Replace(current, []byte(`"packages"`), []byte(`"\u0070ackages"`), 1)
			unordered, changed, err := permutation.Restore(parsedOrderDocument(t, current))
			if err != nil || !changed {
				t.Fatalf("inverse permutation failed against a shifted current array: %v", err)
			}
			restored, err := compose.Restore(t.Context(), aggregate.ExistingDocument(unordered), compositionLimits(), baselines)
			if err != nil || !restored.Document().Exists() || !parsedOrderDocument(t, restored.Document().Content()).PackageArrayEqual(before) {
				t.Fatalf("composed recovery lost post-install rows, slots or root: %v", err)
			}

			beforeValues, err := configdocument.Parse(t.Context(), postInstall.Content(), configdocument.JSON, compositionLimits())
			if err != nil {
				t.Fatal(err)
			}
			afterValues, err := configdocument.Parse(t.Context(), restored.Document().Content(), configdocument.JSON, compositionLimits())
			if err != nil {
				t.Fatal(err)
			}
			theme, _ := output.NewKeyPath("theme")
			oldValue, oldPresent, err := beforeValues.Lookup(theme)
			if err != nil {
				t.Fatal(err)
			}
			actualValue, actualPresent, err := afterValues.Lookup(theme)
			if err != nil || actualPresent != oldPresent || oldPresent && !oldValue.Equal(actualValue) {
				t.Fatal("native recovery did not restore the actual selected post-install value")
			}
			for _, key := range []string{"outside", "concurrent"} {
				path, _ := output.NewKeyPath(key)
				currentValues, err := configdocument.Parse(t.Context(), current, configdocument.JSON, compositionLimits())
				if err != nil {
					t.Fatal(err)
				}
				want, present, err := currentValues.Lookup(path)
				if err != nil || !present {
					t.Fatal("fixture lost an independent current value")
				}
				actual, present, err := afterValues.Lookup(path)
				if err != nil || !present || !want.Equal(actual) {
					t.Fatal("composed recovery overwrote an independent current value")
				}
			}

			again, changed, err := permutation.Restore(parsedOrderDocument(t, restored.Document().Content()))
			if err != nil || changed || !bytes.Equal(again, restored.Document().Content()) {
				t.Fatal("already-restored package projection was not a no-op")
			}
			repeated, err := compose.Restore(t.Context(), aggregate.ExistingDocument(again), compositionLimits(), baselines)
			if err != nil || !repeated.Document().Equal(restored.Document()) {
				t.Fatal("already-restored native projection was not a no-op")
			}
		})
	}
}

func readThemeBaselines(t *testing.T, original aggregate.Document) []compose.ProjectionBaseline {
	t.Helper()
	codec, err := nativecodec.New(t.Context(), "native-json-v1", compositionLimits(), compositionLimits())
	if err != nil {
		t.Fatal(err)
	}
	key, _ := output.NewKeyPath("theme")
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(".pi/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, destination, footprint, codec.ContractID())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := compose.NewParticipant(codec, selection, configdocument.JSON, []output.KeyPath{key})
	if err != nil {
		t.Fatal(err)
	}
	baselines, err := compose.ReadBaselines(t.Context(), original, compositionLimits(), []compose.Participant{participant})
	if err != nil {
		t.Fatal(err)
	}
	return baselines
}

func parsedOrderDocument(t *testing.T, content []byte) piconfig.Document {
	t.Helper()
	document, err := piconfig.Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func compositionLimits() configdocument.Limits {
	return configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
}
