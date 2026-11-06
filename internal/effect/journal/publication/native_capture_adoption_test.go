package publication

import (
	"os"
	"reflect"
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
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
			var mutations []journalrecord.CaptureMutation
			if variant != "no actor" {
				actionDestination, desiredHash, mode := destination, hash, os.FileMode(0o600)
				if variant == "uncorrelated destination" {
					actionDestination = other
				}
				if variant == "uncorrelated hash" {
					desiredHash = artifact.HashFileContent([]byte("different"))
				}
				if variant == "uncorrelated mode" {
					mode = 0o644
				}
				var mutation journalrecord.ManagedPathMutation
				exists, liveHash := true, hash
				switch variant {
				case "create":
					exists, liveHash = false, ""
					mutation, err = journalrecord.NewManagedPathCreateMutation(subject, whole.ConsumerTargets(), whole.Scope(), actionDestination, desiredHash, realization.PathProjectionFile, mode, nil)
				case "replace":
					mutation, err = journalrecord.NewManagedPathReplaceMutation(subject, whole.ConsumerTargets(), whole.Scope(), actionDestination, desiredHash, liveHash, realization.PathProjectionFile, mode, whole)
				default:
					var previous *durable.ManagedPathState
					if variant == "previous whole" {
						previous = &whole
					}
					mutation, err = journalrecord.NewManagedPathRecordMutation(subject, whole.ConsumerTargets(), whole.Scope(), actionDestination, desiredHash, liveHash, realization.PathProjectionFile, mode, previous)
				}
				if err != nil {
					t.Fatal(err)
				}
				evidence := testManagedPathEvidence(t, subject, actionDestination, exists, liveHash)
				mutations, err = journalrecord.NormalizeCaptureMutations([]journalrecord.ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
				if err != nil {
					t.Fatal(err)
				}
			}
			err = validateNativeCaptureWork(before, after, nil, mutations, nil)
			if (err == nil) != (variant == "matched record") {
				t.Fatal("capture confused exact adoption with unsupported key retirement", err)
			}
		})
	}
}
