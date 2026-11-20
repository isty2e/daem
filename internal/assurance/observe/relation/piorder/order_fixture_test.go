package piorder_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/observe/relation/piorder"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type settingsOrderFixture struct {
	Settings   pipackage.SettingsInput
	Constraint hostrelation.RelationOrderConstraint
	Relations  []piorder.ScopedRelation
	Home       string
}

func readFixtureOrder(input settingsOrderFixture) (piorder.OrderObservation, error) {
	inv, err := pipackage.ReadSettings(input.Settings)
	if err != nil {
		return piorder.OrderObservation{}, err
	}
	ctx, err := extensiontopology.NewLocalSourceContext(filepath.Dir(inv.SettingsPath()), input.Home)
	if err != nil {
		return piorder.OrderObservation{}, err
	}
	capability, _ := profile.Profile(target.TargetPi).ExtensionOrder(desiredextension.CarrierPiPackage, input.Settings.Scope)
	return piorder.ReconstructOrder(piorder.LoadedOrderInput{Scope: input.Settings.Scope, Capability: capability, SettingsContext: ctx, Document: inv.Document(), Constraint: input.Constraint, Relations: input.Relations})
}

func writeSettings(t *testing.T, path string, content string) {
	t.Helper()
	writeSettingsBytes(t, path, []byte(content))
}

func writeSettingsBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
