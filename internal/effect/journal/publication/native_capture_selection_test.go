package publication

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestCaptureMembersRetainsProspectiveBindingFactsWithoutEffects(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, action := range []string{"create", "record", "remove"} {
			t.Run(string(scope)+"/"+action, func(t *testing.T) {
				fixture, input, reader, registryPath := nativeInitialFixture(t, scope, action)
				batch, err := ObserveNativeDocuments(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				reads, bytes := fixture.reader.imageReads, input.Budget.RemainingBytes()
				selected, members, err := batch.CaptureMembers(fixture.envelope.after, fixture.input.OperationID())
				if err != nil || len(members) != 1 || len(selected.Evidence().Namespaces()) != 1 {
					t.Fatal("prospective document membership was not retained", err)
				}
				member := members[0]
				intents := member.BindingIntents()
				if len(intents) != 1 || intents[0].Validate() != nil || !intents[0].Owner().Equal(input.Owner) || intents[0].Destination() != input.Aliases[0].AggregateRoot() {
					t.Fatal("prospective binding lost its actual owner or destination")
				}
				_, had := intents[0].Before().Get()
				if had != (action != "create") {
					t.Fatal("before binding membership was changed")
				}
				if provisional, pending := intents[0].ProvisionalAfter(); pending {
					observed, missing := member.Namespace().Observation().Provisional()
					if action != "create" || !missing || !provisional.Equal(observed) || !member.RequiresPromotion() {
						t.Fatal("pending binding gained exact identity or lost promotion work")
					}
				} else {
					after, known := intents[0].KnownAfter()
					binding, present := after.Get()
					if !known || present != (action != "remove") {
						t.Fatal("known after membership was changed")
					}
					if present {
						exact, observed := member.Namespace().Observation().Exact()
						if !observed || !binding.PathAuthority().Equal(exact) {
							t.Fatal("exact binding lost the observed namespace")
						}
					}
				}
				expectedScope, err := selected.Evidence().RegistryScopeFor(member.Namespace().Addresses())
				if err != nil || !member.RegistryScope().Equal(expectedScope) {
					t.Fatal("member Registry comparison scope changed", err)
				}
				intents[0] = ownership.DocumentBindingIntent{}
				if member.BindingIntents()[0].Validate() != nil {
					t.Fatal("caller mutated retained membership")
				}
				if fixture.reader.imageReads != reads || reader.loads != 1 || input.Budget.RemainingBytes() != bytes {
					t.Fatal("prospective projection performed another observation")
				}
				fixture.requireUnchangedHost(t)
				if action == "create" {
					if _, err := os.Lstat(registryPath); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("prospective projection published the absent Registry", err)
					}
				}
			})
		}
	}
}

func TestCaptureMembersKeepsAliasesWithOnlyActualBindingMembership(t *testing.T) {
	fixture, input, _, _ := nativeInitialFixture(t, target.ScopeGlobal, "record")
	path := filepath.ToSlash(filepath.Join("global", input.Aliases[0].AggregateRoot().RelativePath()))
	alias, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, outputtest.Parse(t, path))
	if err != nil {
		t.Fatal(err)
	}
	input.Aliases = append(input.Aliases, alias)
	batch, err := ObserveNativeDocuments(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	selected, members, err := batch.CaptureMembers(fixture.envelope.after, fixture.input.OperationID())
	if err != nil || len(members) != 1 || len(selected.Evidence().Namespaces()) != 1 || len(members[0].Namespace().Addresses()) != 2 {
		t.Fatal("prospective projection split a physical alias group", err)
	}
	bindings := members[0].BindingIntents()
	if len(bindings) != 1 || bindings[0].Destination() != input.Aliases[0].AggregateRoot() || members[0].RequiresPromotion() {
		t.Fatal("an observed alias became an owned binding or promotion")
	}
}

func TestCaptureMembersDropsUnselectedNamespacesAndFailedResults(t *testing.T) {
	fixture, input, _, _ := nativeInitialFixture(t, target.ScopeProject, "create")
	batch, err := ObserveNativeDocuments(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	selected, members, err := batch.CaptureMembers(durable.EmptySnapshot(), fixture.input.OperationID())
	if err != nil || len(selected.Evidence().Namespaces()) != 0 || len(members) != 0 {
		t.Fatal("unselected namespace gained prospective membership", err)
	}
	selected, members, err = batch.CaptureMembers(fixture.envelope.after, "invalid/operation")
	if err == nil || len(members) != 0 || len(selected.Evidence().Namespaces()) != 0 {
		t.Fatal("failed membership derivation returned a usable prefix")
	}
}
