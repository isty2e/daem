package nativefragment

import (
	"context"
	"errors"
	"testing"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

var nativeRefinementTestLimits = configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 512}

func nativeConfig(t *testing.T, selected target.Target, nativePath string) desiredhostconfig.Config {
	t.Helper()
	local, err := source.NewLocalSource("config/input.data", source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	config, err := desiredhostconfig.New(desiredhostconfig.Spec{
		Name: "review", Source: local, Target: selected, Scope: target.ScopeProject, Path: nativePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func nativeContributionsByKey(t *testing.T, config desiredhostconfig.Config, contributions []aggregate.SubjectContribution) map[string]aggregate.ManagedContribution {
	t.Helper()
	result := make(map[string]aggregate.ManagedContribution, len(contributions))
	for _, item := range contributions {
		id, key, ok := topologyprojection.HostConfigKeyIdentity(item.SubjectID())
		if !ok || id != config.ID() {
			t.Fatal("contribution lost its declaration or native key")
		}
		value := item.Contribution()
		if value.Address().KeyPath() != key || value.Target() != config.Target() || value.Scope() != config.Scope() {
			t.Fatal("contribution lost its placement or native scope")
		}
		if err := aggregate.ValidateSubjectContract(item.SubjectID(), value.Contract()); err != nil {
			t.Fatal(err)
		}
		if _, err := profile.HostConfigPlacementForContract(value.Contract()); err != nil {
			t.Fatal(err)
		}
		if _, exists := result[key.String()]; exists {
			t.Fatal("duplicate native key contribution")
		}
		result[key.String()] = value
	}
	return result
}

func TestNativeConfigRefinementKeepsAtomicValuesAndLiteralCredentials(t *testing.T) {
	config := nativeConfig(t, target.TargetPi, "extensions/subagent/config.json")
	content := []byte(`{"globalConcurrencyLimit":100,"provider":{"apiKey":"literal-at-own-risk","empty":{}},"enabledModels":["one","two"],"null":null,"a/b~c":{"":true},"\u0000":2}`)
	contributions, err := HostConfigKeyContributions(t.Context(), config, content, nativeRefinementTestLimits)
	if err != nil {
		t.Fatal(err)
	}
	byKey := nativeContributionsByKey(t, config, contributions)
	want := map[string]string{
		"/globalConcurrencyLimit": `{"value":100}`,
		"/provider/apiKey":        `{"value":"literal-at-own-risk"}`,
		"/provider/empty":         `{"value":{}}`,
		"/enabledModels":          `{"value":["one","two"]}`,
		"/null":                   `{"value":null}`,
		"/a~1b~0c/":               `{"value":true}`,
		"/\x00":                   `{"value":2}`,
	}
	if len(byKey) != len(want) {
		t.Fatalf("got %d atomic contributions, want %d", len(byKey), len(want))
	}
	for key, jsonValue := range want {
		expected, err := configdocument.Parse(t.Context(), []byte(jsonValue), configdocument.JSON, nativeRefinementTestLimits)
		if err != nil {
			t.Fatal(err)
		}
		valueKey, _ := output.NewKeyPath("value")
		value, present, err := expected.Lookup(valueKey)
		if err != nil || !present || byKey[key].CanonicalContribution() != value.Canonical() {
			t.Fatalf("atomic value changed at escaped key %q: %v", key, err)
		}
	}
}

func TestNativeConfigRefinementPreservesTOMLTypesAndEquivalentSpellings(t *testing.T) {
	config := nativeConfig(t, target.TargetCodex, "config.toml")
	inputs := []string{
		"[tui]\ntheme = \"dark\"\nratio = 1.0\nwhen = 2026-10-23\nempty = {}\narray = [{ name = \"one\" }]\n",
		"tui = {\n# inline comment\ntheme = \"dark\", ratio = 1.0, when = 2026-10-23, empty = {}, array = [{name=\"one\"}],\n}\n",
		"tui.theme=\"dark\"\ntui.ratio=1.0\ntui.when=2026-10-23\ntui.empty={}\ntui.array=[{name=\"one\"}]\n",
	}
	var baseline map[string]aggregate.ManagedContribution
	for _, content := range inputs {
		contributions, err := HostConfigKeyContributions(t.Context(), config, []byte(content), nativeRefinementTestLimits)
		if err != nil {
			t.Fatal(err)
		}
		byKey := nativeContributionsByKey(t, config, contributions)
		if len(byKey) != 5 {
			t.Fatalf("got %d leaves, want 5", len(byKey))
		}
		if baseline == nil {
			baseline = byKey
		} else {
			for key, previous := range baseline {
				if !previous.Equal(byKey[key]) {
					t.Fatalf("equivalent TOML spelling changed contribution at %s", key)
				}
			}
		}
	}
	integer, err := HostConfigKeyContributions(t.Context(), config, []byte("tui.ratio=1\n"), nativeRefinementTestLimits)
	if err != nil {
		t.Fatal(err)
	}
	if integer[0].Contribution().CanonicalContribution() == baseline["/tui/ratio"].CanonicalContribution() {
		t.Fatal("TOML float became an integer")
	}
}

func TestNativeConfigRefinementKeepsSubjectsAcrossValueChanges(t *testing.T) {
	config := nativeConfig(t, target.TargetPi, "settings.json")
	identities := make([]map[topology.SubjectID]bool, 0, 2)
	for _, content := range []string{`{"theme":"dark","nested":{"x":1}}`, `{"nested":{"x":2},"theme":"light"}`} {
		contributions, err := HostConfigKeyContributions(t.Context(), config, []byte(content), nativeRefinementTestLimits)
		if err != nil {
			t.Fatal(err)
		}
		set := make(map[topology.SubjectID]bool)
		for _, item := range contributions {
			set[item.SubjectID()] = true
		}
		identities = append(identities, set)
	}
	if len(identities[0]) != 2 || len(identities[1]) != 2 {
		t.Fatal("unexpected leaf membership")
	}
	for id := range identities[0] {
		if !identities[1][id] {
			t.Fatal("value or ordering changed stable subject identity")
		}
	}
	empty, err := HostConfigKeyContributions(t.Context(), config, []byte(`{}`), nativeRefinementTestLimits)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty fragment incorrectly acquired ownership: %v", err)
	}
}

func TestNativeConfigRefinementRejectsCompleteInvalidInputWithoutPartialContributions(t *testing.T) {
	config := nativeConfig(t, target.TargetPi, "settings.json")
	for _, content := range []string{
		`{"a":1,"packages":["npm:example"]}`, `{"a":1,"extensions":[]}`, `{"skills":{}}`,
		`{"a":1,"a":2}`, `{"a":1,}`, `// comment\n{"a":1}`, `[]`, `null`, `{`,
	} {
		items, err := HostConfigKeyContributions(t.Context(), config, []byte(content), nativeRefinementTestLimits)
		if err == nil || len(items) != 0 {
			t.Fatal("invalid fragment admitted or returned partial contributions")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	items, err := HostConfigKeyContributions(ctx, config, []byte(`{"theme":"dark"}`), nativeRefinementTestLimits)
	if !errors.Is(err, context.Canceled) || len(items) != 0 {
		t.Fatalf("cancellation did not discard contributions: %v", err)
	}
	items, err = HostConfigKeyContributions(t.Context(), config, []byte(`{"theme":"dark"}`), configdocument.Limits{MaximumBytes: 4, MaximumDepth: 512})
	if !errors.Is(err, configdocument.ErrLimitExceeded) || len(items) != 0 {
		t.Fatalf("caller admission limit was ignored: %v", err)
	}
}
