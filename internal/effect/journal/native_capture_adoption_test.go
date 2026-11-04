package journal

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativeCaptureWholeRecordCoversOnlyItsOwnExactKeyRetirement(t *testing.T) {
	for _, variant := range []string{"matched record", "no actor", "create", "replace", "previous whole", "foreign declaration", "other document", "uncorrelated destination", "uncorrelated hash", "uncorrelated mode"} {
		t.Run(variant, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			before := fixture.before
			pending, _ := captureProjectCompletionFacts(t, fixture.claims.owner, "unrelated")
			var err error
			before, err = before.WithPendingCarrierInstalls([]durablecarrier.PendingCarrierInstall{pending})
			if err != nil {
				t.Fatal(err)
			}
			row := before.ManagedAggregates()[0]
			owner, _, native := topologyprojection.HostConfigKeyIdentity(row.Subject())
			if !native {
				t.Fatal("fixture has no real native key identity")
			}
			if variant == "foreign declaration" {
				owner, err = entity.New(entity.KindHostConfig, "foreign")
				if err != nil {
					t.Fatal(err)
				}
			}
			document := row.Contribution().Contract().Address().Document()
			destination := document.AggregateRoot()
			other, err := output.Parse(destination.String() + "-other")
			if err != nil {
				t.Fatal(err)
			}
			if variant == "other document" {
				destination = other
			}
			subject, err := aggregate.HostConfigDocumentSubject(owner, document.Target(), document.Scope())
			if err != nil {
				t.Fatal(err)
			}
			hash := artifact.HashFileContent([]byte(`{"model":true,"outside":1}`))
			whole, err := durable.NewManagedPathState(subject, []target.Target{document.Target()}, document.Scope(), destination, hash, realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := before.WithManagedPaths([]durable.ManagedPathState{whole}); variant != "other document" && err == nil {
				t.Fatal("fixture did not expose the invalid sequential key/whole overlap")
			}
			after, err := before.WithManagedProjections([]durable.ManagedPathState{whole}, nil)
			if err != nil || !reflect.DeepEqual(after.PendingCarrierInstalls(), before.PendingCarrierInstalls()) {
				t.Fatal("joint projection transition lost unrelated complete-state facts", err)
			}
			mutation := pathMutation{Kind: pathMutationRecord, Subject: subject, ConsumerTargets: whole.ConsumerTargets(), Scope: whole.Scope(), Destination: destination, DesiredHash: hash, ExpectedPathMode: 0o600, ContentKind: realization.PathProjectionFile}
			switch variant {
			case "create":
				mutation.Kind = pathMutationCreate
			case "replace":
				mutation.Kind = pathMutationReplace
			case "previous whole":
				mutation.PreviousState = previousPathStateFromManaged(whole)
			case "uncorrelated destination":
				mutation.Destination = other
			case "uncorrelated hash":
				mutation.DesiredHash = artifact.HashFileContent([]byte("different"))
			case "uncorrelated mode":
				mutation.ExpectedPathMode = 0o644
			}
			mutations := []pathMutation{mutation}
			if variant == "no actor" {
				mutations = nil
			}
			err = validateNativeCaptureWork(before, after, nil, mutations, nil)
			if (err == nil) != (variant == "matched record") {
				t.Fatal("capture confused exact adoption with unsupported key retirement", err)
			}
		})
	}
}
