package clipresent

import (
	"bytes"
	"io"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierHumanRowsUseDeclarationIdentityAndKeepVerboseSubject(t *testing.T) {
	value := desiredtest.Extension(t, desiredextension.Spec{
		Name: "my-tools", Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target: target.TargetClaudeCode, Scope: target.ScopeProject,
		Source: desiredtest.ExtensionSource(t, desiredextension.SourceKindMarketplace, "different-package@market"),
	})
	file, relation := snapshottest.ExtensionCarrierFile(t, value)
	contract := file.Locked.Subjects()[0]
	fixture := newPresentCarrierAdoptionFixtureFromContract(t, contract, relation)
	adoption := fixture.action(t, true, fixture.lifecycle, nil)
	claim := fixture.claim(t, fixture.owner, durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved)
	absence := presentCarrierAbsenceActionFromContract(t, contract, relation)
	action := claudePluginCarrierActionForSubject(t, contract, relation, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnsupported, Freshness: observerelation.EvidenceFresh,
	})
	for _, test := range []struct {
		name  string
		print func(io.Writer, HumanOptions)
	}{
		{"relation", func(out io.Writer, options HumanOptions) {
			PrintRelationActionsWithOptions(out, []reconcile.RelationAction{action}, options)
		}},
		{"adoption", func(out io.Writer, options HumanOptions) {
			PrintCarrierAdoptionActionsWithOptions(out, []carrieradoption.Action{adoption}, options)
		}},
		{"absence", func(out io.Writer, options HumanOptions) {
			PrintCarrierAbsenceActionsWithOptions(out, []carrierabsence.Action{absence}, options)
		}},
		{"adoption result", func(out io.Writer, options HumanOptions) {
			PrintCarrierAdoptionResultsWithOptions(out, []carrieradoption.Action{adoption}, []durablecarrier.ManagedCarrierClaim{claim}, nil, true, options)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, verbose := range []bool{false, true} {
				var output bytes.Buffer
				test.print(&output, HumanOptions{Verbose: verbose})
				if !strings.Contains(output.String(), `resource="extension/my-tools"`) ||
					!strings.Contains(output.String(), "target=claude-code scope=project") ||
					strings.Contains(output.String(), `resource="extension/different-package"`) {
					t.Fatalf("verbose=%t wrong logical identity:\n%s", verbose, output.String())
				}
				canonical := `subject="host_relation/claude-code.plugin-carrier/my-tools"`
				if strings.Contains(output.String(), canonical) != verbose {
					t.Fatalf("verbose=%t wrong canonical correlation visibility:\n%s", verbose, output.String())
				}
			}
		})
	}
}

func TestCarrierHumanReferenceDoesNotRecoverRedactedAlias(t *testing.T) {
	for _, source := range []string{"plugins/local.ts", "npm:public-package"} {
		contract, _ := hostSourceCarrierFixture(t, "declared-alias", desiredextension.CarrierPiPackage, target.TargetPi, target.ScopeProject, source)
		identity := presentManagedCarrierIdentity(t, contract)
		for _, verbose := range []bool{false, true} {
			fields := carrierHumanReference(identity).fields(verbose)
			if source == "plugins/local.ts" {
				if strings.Contains(fields, "declared-alias") || strings.Contains(fields, source) || !strings.Contains(fields, "subject=") || !strings.Contains(fields, "redacted:sha256:") {
					t.Fatalf("verbose=%t alias bypassed source disclosure: %s", verbose, fields)
				}
			} else if !strings.Contains(fields, `resource="extension/declared-alias"`) {
				t.Fatalf("public carrier lost logical reference: %s", fields)
			}
		}
	}
}
