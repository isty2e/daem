package pipackage

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology/extension"
)

const testRequestHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func mustPiPendingRemoval(
	t *testing.T,
	commandRoot string,
	scope target.Scope,
	source string,
	baselines durablecarrier.EffectBaselineSet,
) durablecarrier.PendingCarrierRemoval {
	t.Helper()
	carrierKey := mustPiCarrierKey(t, scope, source)
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    "tools-managed",
		Carrier: desiredextension.CarrierPiPackage,
		Target:  target.TargetPi,
		Scope:   scope,
		Source:  carrierKey.Source(),
	})
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extension.NewCarrier(carrierKey)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := extension.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(source)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.Derive(carrierKey, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(pathtest.Exact(
		filepath.Join(commandRoot, ".daem", "state.json"),
	),

		filepath.Join(commandRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	installRequest := mustRouteRequest(t, "pi.package-carrier.install")
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		installRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	interpreted, err := extension.InterpretCarrierSource(carrierKey)
	if err != nil {
		t.Fatal(err)
	}
	requirement := effectpostcondition.CarrierArtifactsAbsent
	if interpreted.Class() == extension.CarrierSourceLocal {
		requirement = effectpostcondition.LocalSourceUnchanged
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		mustRouteRequest(t, "pi.package-carrier.remove"),
		mustEffectRequirements(t, requirement),
		baselines,
	)
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

func mustPiCarrierKey(
	t *testing.T,
	scope target.Scope,
	source string,
) desiredextension.CarrierKey {
	t.Helper()
	ref, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, source)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		scope,
		ref,
	)
	if err != nil {
		t.Fatal(err)
	}
	return carrier
}

func mustEffectRequirements(
	t *testing.T,
	requirement effectpostcondition.Requirement,
) effectpostcondition.Set {
	t.Helper()
	requirements, err := effectpostcondition.NewSet([]effectpostcondition.Requirement{requirement})
	if err != nil {
		t.Fatal(err)
	}
	return requirements
}

func mustRouteRequest(t *testing.T, routeID string) realizationdelegate.Request {
	t.Helper()
	request, err := realizationdelegate.NewRequest(routeID, "v1", testRequestHash)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func writePiSettings(t *testing.T, path string, sources []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, 0, len(sources))
	for _, source := range sources {
		quoted = append(quoted, strconv.Quote(source))
	}
	content := `{"packages":[` + strings.Join(quoted, ",") + `]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSourceContent(t *testing.T, root string, content string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.ts"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolvedTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func pointerTo(value string) *string {
	return &value
}
