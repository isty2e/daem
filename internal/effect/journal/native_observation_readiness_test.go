package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeObservationBeforeQualificationSharesCaptureInterpretation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, change := range []string{"none", "missing binding", "wrong binding", "missing claim", "scoped claims", "reserved claim"} {
			if scope == target.ScopeProject && (change == "missing claim" || change == "scoped claims" || change == "reserved claim") {
				continue
			}
			t.Run(string(scope)+"/"+change, func(t *testing.T) {
				fixture, input, reader, _ := nativeInitialFixture(t, scope, "record")
				batch, err := ObserveNativeDocuments(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				initial, err := reader.store.Load(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				claims, bindings := initial.Claims(), initial.DocumentBindings()
				switch change {
				case "missing binding":
					bindings = nil
				case "wrong binding":
					bindings[0], err = ownership.NewDocumentBinding(input.Owner, bindings[0].Destination(), input.Owner.StatefileAuthority())
				case "missing claim":
					claims = nil
				case "scoped claims":
					exact := claims[0].Address().PathAuthority()
					claims = nil
					for _, name := range []string{"first", "second"} {
						key, err := output.NewKeyPath(name)
						if err != nil {
							t.Fatal(err)
						}
						footprint, err := output.NewKeyFootprint(key)
						if err != nil {
							t.Fatal(err)
						}
						address, err := ownership.NewManagedAddress(exact, footprint)
						if err != nil {
							t.Fatal(err)
						}
						claim, err := ownership.NewActiveClaim(address, input.Owner)
						if err != nil {
							t.Fatal(err)
						}
						claims = append(claims, claim)
					}
				case "reserved claim":
					claims[0], err = ownership.NewReservedClaim(claims[0].Address(), input.Owner, "interrupted")
				}
				if err != nil {
					t.Fatal(err)
				}
				current, err := ownership.NewRegistry(claims, bindings...)
				if err != nil {
					t.Fatal(err)
				}
				input.Registry = &nativeCaptureRegistryReader{value: current}
				batch, err = ObserveNativeDocuments(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				remaining := input.Budget.RemainingBytes()
				qualification := batch.Evidence().ValidateBefore(t.Context())
				prepared, continuation := batch.prepareOwnership(t.Context(), fixture.envelope.after, fixture.input.OperationID())
				if change == "none" {
					if qualification != nil || continuation != nil || len(prepared) != 1 {
						t.Fatal("actual matching history was refused", qualification, continuation)
					}
				} else if qualification == nil || continuation == nil || len(prepared) != 0 {
					t.Fatal("before qualifier and capture disagree on invalid history", qualification, continuation)
				}
				if reader.loads != 1 || input.Budget.RequireCapturePending() != nil || input.Budget.RemainingBytes() != remaining-int64(len(fixture.original.Content())) {
					t.Fatal("before validation reread Registry, read an image or changed budget admission")
				}
				fixture.requireUnchangedHost(t)
			})
		}
	}
}
