package record

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/statefile"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func carrierRecordFixture(t *testing.T, coalesced bool) (nativeRecordFixture, native17JournalRecord, [][2]aggregate.Document) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeGlobal)
	if !ok {
		t.Fatal("missing Claude global MCP placement")
	}
	contract, err := placement.ProjectionContract("search")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	fixture.codecs = aggregatecodec.Catalog()
	fixture.codec, err = statefile.NewNativeCodec(fixture.codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := fixture.codecs.Lookup(selection.CodecContractID())
	rowFor := func(content string) durable.ManagedAggregateState {
		observed, failure := codec.Read(aggregate.ExistingDocument([]byte(content)), selection)
		if failure != nil {
			t.Fatal(failure)
		}
		projection, _ := observed.State(contract)
		contribution, err := placement.Contribution("search", projection.CanonicalProjection())
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), "search")
		if err != nil {
			t.Fatal(err)
		}
		row, err := durable.NewManagedAggregateState(subject, contribution)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	render := func(before aggregate.Document, desired *durable.ManagedAggregateState) aggregate.Document {
		observed, failure := codec.Read(before, selection)
		if failure != nil {
			t.Fatal(failure)
		}
		var contributions *aggregate.ContributionSet
		if desired != nil {
			member, err := aggregate.NewSubjectContribution(desired.Subject(), desired.Contribution())
			if err != nil {
				t.Fatal(err)
			}
			values, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{member})
			if err != nil {
				t.Fatal(err)
			}
			contributions = &values
		}
		intent, err := aggregate.NewProjectionIntent(observed.States()[0], contributions)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := aggregate.NewPlan(observed, []aggregate.ProjectionIntent{intent})
		if err != nil {
			t.Fatal(err)
		}
		result, failure := codec.Render(before, plan)
		if failure != nil {
			t.Fatal(failure)
		}
		return result.Document()
	}
	const original = `{"preserved":1,"mcpServers":{"search":{"type":"stdio","command":"npx"}}}`
	const targetBefore = `{"peer":2}`
	row := rowFor(original)
	fixture.before = selectionState(t, row)
	fixture.after, err = fixture.before.AdvanceNativeDocumentCommit()
	if err != nil {
		t.Fatal(err)
	}
	sourceImage, targetImage := aggregate.ExistingDocument([]byte(original)), aggregate.ExistingDocument([]byte(targetBefore))
	images := [][2]aggregate.Document{{sourceImage, render(sourceImage, nil)}, {targetImage, render(targetImage, &row)}}
	if coalesced {
		const changed = `{"preserved":1,"mcpServers":{"search":{"type":"stdio","command":"node"}}}`
		after := rowFor(changed)
		fixture.after = selectionState(t, after)
		images = [][2]aggregate.Document{{sourceImage, render(sourceImage, &after)}}
	}
	wire := native17PointMetadata(t, fixture)
	sourceReference := nativeRootReference{Identity: "claude-account-owned-source", Locator: "claude-owned-source-point-v1"}
	currentReference := wire.DocumentGroups[0].Writer.Root
	root := wire.Roots[0]
	root.Reference, root.Alternative = sourceReference, ""
	wire.Roots = append(wire.Roots, root)
	slices.SortFunc(wire.Roots, func(left, right native17Root) int {
		a, _ := left.Reference.canonical()
		b, _ := right.Reference.canonical()
		return a.Compare(b)
	})
	base := root.Provenance.PhysicalRoot
	physical := []string{filepath.Join(base, "source.json"), filepath.Join(base, "target.json")}
	if coalesced {
		physical[1] = physical[0]
	}
	claims := make([]ownership.Claim, 2)
	for index, path := range physical {
		facts := fixture.claims
		facts.exact = pathtest.DarwinCaseSensitive(path)
		claims[index] = documentActiveClaim(t, facts, contract.Address().Footprint())
	}
	newBinding, err := ownership.NewDocumentBinding(fixture.claims.owner, contract.Address().Document().AggregateRoot(), claims[1].Address().PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	bindingValue, err := ownership.PresentDocumentBinding(newBinding)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewExactDocumentBindingIntent(fixture.claims.owner, newBinding.Destination(), ownership.NoDocumentBinding(), bindingValue)
	if err != nil {
		t.Fatal(err)
	}
	persistedBinding, err := persistedDocumentBindingIntent(binding)
	if err != nil {
		t.Fatal(err)
	}
	template := wire.DocumentGroups[0]
	wire.DocumentGroups = make([]native17Document, len(images))
	for index, pair := range images {
		group := template
		group.SelectedSubjects = []persistedSubjectRef{{Kind: string(row.Subject().Kind()), Namespace: row.Subject().Namespace(), Name: row.Subject().Key()}}
		group.BindingIntents = []recoveryDocumentBindingIntent{}
		group.Writer.Spelling = physical[index]
		group.CapturedTarget = recoveryDocumentObservation{Kind: "exact", Authority: persistedPathAuthority(claims[index].Address().PathAuthority())}
		alias := template.Aliases[0]
		alias.SelectedObservation, alias.ObservedBefore = group.CapturedTarget, group.CapturedTarget
		alias.Root, alias.Policy = sourceReference, "claude-owned-mcp-source-v1"
		group.Writer.Root = sourceReference
		group.Aliases = []native17Alias{alias}
		beforeClaims, afterClaims := []ownership.Claim{claims[0]}, []ownership.Claim(nil)
		if index == 1 || coalesced {
			alias.Root, alias.Policy = currentReference, "claude-account-carrier-v2"
			group.Writer.Root = currentReference
			if coalesced {
				group.Aliases = append(group.Aliases, alias)
			} else {
				group.Aliases, beforeClaims = []native17Alias{alias}, nil
			}
			afterClaims = []ownership.Claim{claims[1]}
			group.BindingIntents = []recoveryDocumentBindingIntent{persistedBinding}
		}
		slices.SortFunc(group.Aliases, compareNative17Aliases)
		transition, err := ownershipmutation.NewDocumentClaimTransition(beforeClaims, afterClaims, wire.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		group.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, wire.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		for side, document := range pair {
			image, err := persistedDocumentImage(document, recovery.NewPermissionMode(0o600), fmt.Sprintf("images/carrier-%d-%d", index, side))
			if err != nil {
				t.Fatal(err)
			}
			if side == 0 {
				group.Original = image
			} else {
				group.Expected = image
			}
		}
		wire.DocumentGroups[index] = group
	}
	return fixture, wire, images
}

func TestNative17CarrierRecordReconstructsPhysicalPhasesAndRefusesUnsplitClaims(t *testing.T) {
	fixture, wire, images := carrierRecordFixture(t, false)
	content, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := journal.Encode(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	journal, err = decodeNativeJournalRecord(t.Context(), encoded, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	loaded := reconstructCarrierImages(t, journal, fixture, images)
	source, _ := loaded.At(0)
	target, _ := loaded.At(1)
	if len(source.Group().AfterAliases()) != 0 || len(source.Claims().After()) != 0 || len(source.Claims().Before()) != 1 || len(target.Group().BeforeAliases()) != 0 || len(target.Claims().Before()) != 0 || len(target.Claims().After()) != 1 {
		t.Fatal("logical alias membership leaked into the other physical phase")
	}
	contract := fixture.before.ManagedAggregates()[0].Contribution().Contract()
	sourceBefore, beforePresent := source.Group().Comparison().BeforeState().Projection(contract)
	sourceAfter, afterPresent := source.Group().Comparison().ExpectedState().Projection(contract)
	targetBefore, targetBeforePresent := target.Group().Comparison().BeforeState().Projection(contract)
	targetAfter, targetAfterPresent := target.Group().Comparison().ExpectedState().Projection(contract)
	if !beforePresent || !afterPresent || !targetBeforePresent || !targetAfterPresent || !sourceBefore.Present() || sourceAfter.Present() || targetBefore.Present() || !targetAfter.Present() {
		t.Fatal("carrier frontier selection used the other physical phase")
	}
	for name, mutate := range map[string]func(*native17JournalRecord){
		"source after claims": func(record *native17JournalRecord) {
			record.DocumentGroups[0].ExclusiveClaims.After = record.DocumentGroups[0].ExclusiveClaims.Before
		},
		"target before claims": func(record *native17JournalRecord) {
			record.DocumentGroups[1].ExclusiveClaims.Before = record.DocumentGroups[1].ExclusiveClaims.After
		},
		"binding at source": func(record *native17JournalRecord) {
			record.DocumentGroups[0].BindingIntents = record.DocumentGroups[1].BindingIntents
			record.DocumentGroups[1].BindingIntents = []recoveryDocumentBindingIntent{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, bad, _ := carrierRecordFixture(t, false)
			mutate(&bad)
			content, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}
			if value, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec); err == nil || value.requireNativeValue() == nil {
				t.Fatal("invalid carrier metadata returned a usable or partial operation", err)
			}
		})
	}
}

func reconstructCarrierImages(t *testing.T, journal Native, fixture nativeRecordFixture, images [][2]aggregate.Document) LoadedDocuments {
	t.Helper()
	session, err := journal.BeginLoadedDocuments(t.Context(), fixture.limits, fixture.codecs)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range images {
		demand, present, err := session.Next(t.Context())
		if err != nil || !present {
			t.Fatal("missing complete carrier image demand", err)
		}
		before := reconstructionFixtureImage(t, demand.Original(), pair[0])
		after := reconstructionFixtureImage(t, demand.Expected(), pair[1])
		if err := session.SupplyLoaded(t.Context(), demand, before, after); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := session.FinishLoaded(t.Context())
	if err != nil || loaded.Len() != len(images) {
		t.Fatal("carrier reconstruction did not reach its complete seal", err)
	}
	return loaded
}

func TestNative17FreshManagementCannotInventOwnedSource(t *testing.T) {
	fixture, wire, images := carrierRecordFixture(t, true)
	before, after, err := EncodeSnapshots(durable.EmptySnapshot(), fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	wire.StatefileBefore, wire.StatefileAfter = before, after
	empty := []recoveryClaimValue{}
	wire.DocumentGroups[0].ExclusiveClaims.Before = &empty
	images[0][0] = aggregate.ExistingDocument([]byte(`{"preserved":1}`))
	wire.DocumentGroups[0].Original, err = persistedDocumentImage(images[0][0], recovery.NewPermissionMode(0o600), "images/carrier-0-0")
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec); err == nil || value.requireNativeValue() == nil {
		t.Fatal("fresh management acquired a fabricated historical source", err)
	}
	wire.DocumentGroups[0].Aliases = slices.DeleteFunc(wire.DocumentGroups[0].Aliases, func(alias native17Alias) bool {
		return alias.Policy == "claude-owned-mcp-source-v1"
	})
	wire.Roots = slices.DeleteFunc(wire.Roots, func(root native17Root) bool {
		return root.Reference.Identity == "claude-account-owned-source"
	})
	content, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	current, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal("current-only fresh acquisition was rejected", err)
	}
	reconstructCarrierImages(t, current, fixture, images)
}

func TestNative17BoundCarrierBindingNeedsItsSourceAndPromotesWithinThePair(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			fixture, wire, images := carrierRecordFixture(t, false)
			source := wire.DocumentGroups[0].CapturedTarget
			sourceExact, err := canonicalPathAuthority(*source.Authority)
			if err != nil {
				t.Fatal(err)
			}
			address := fixture.before.ManagedAggregates()[0].Contribution().Contract().Address()
			binding, err := ownership.NewDocumentBinding(fixture.claims.owner, address.Document().AggregateRoot(), sourceExact)
			if err != nil {
				t.Fatal(err)
			}
			targetGroup := &wire.DocumentGroups[1]
			targetExact, err := canonicalPathAuthority(*targetGroup.CapturedTarget.Authority)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := ownership.NewExactDocumentBindingTransferIntent(binding, targetExact)
			if err != nil {
				t.Fatal(err)
			}
			if pending {
				base := filepath.Dir(sourceExact.Key())
				targetExact = pathtest.DarwinCaseSensitive(filepath.Join(base, "current", "Caf\u00e9.json"))
				namespace := pathtest.DarwinCaseSensitive(base)
				candidate, err := pathauthority.NewProvisional(targetExact.Key(), targetExact.Witness(), namespace.Key(), namespace.Witness())
				if err != nil {
					t.Fatal(err)
				}
				intent, err = ownership.NewProvisionalDocumentBindingTransferIntent(binding, candidate)
				if err != nil {
					t.Fatal(err)
				}
				targetGroup.CapturedTarget = recoveryDocumentObservation{Kind: "provisional", Authority: persistedPathAuthority(targetExact), NamespaceAuthority: persistedPathAuthority(namespace)}
				targetGroup.Writer.Spelling = targetExact.Key()
				targetGroup.Aliases[0].SelectedObservation, targetGroup.Aliases[0].ObservedBefore = targetGroup.CapturedTarget, targetGroup.CapturedTarget
				acquire, err := ownership.NewProvisionalAcquireIntent(address.Document().AggregateRoot(), address.Footprint(), candidate, fixture.claims.owner, wire.OperationID)
				if err != nil {
					t.Fatal(err)
				}
				targetGroup.ExclusiveClaims, err = persistedDocumentProvisionalClaims([]journaldocument.ProvisionalAcquire{journaldocument.NewProvisionalAcquire(address.Document(), acquire)}, journaldocument.NativeClaims)
				if err != nil {
					t.Fatal(err)
				}
				images[1][0] = aggregate.AbsentDocument()
				var values map[string]json.RawMessage
				if err := json.Unmarshal(images[1][1].Content(), &values); err != nil {
					t.Fatal(err)
				}
				delete(values, "peer")
				created, err := json.Marshal(values)
				if err != nil {
					t.Fatal(err)
				}
				images[1][1] = aggregate.ExistingDocument(created)
				targetGroup.Original, err = persistedDocumentImage(images[1][0], nil, "")
				if err != nil {
					t.Fatal(err)
				}
				targetGroup.Expected, err = persistedDocumentImage(images[1][1], recovery.NewPermissionMode(0o600), "images/carrier-1-1")
				if err != nil {
					t.Fatal(err)
				}
				removal := recoveryDocumentGroup{Aliases: []recoveryDocumentAlias{{Address: persistedDocumentAlias(address.Document())}}, Original: targetGroup.Original, Expected: targetGroup.Expected, Writer: fixture.record.DocumentGroups[0].Writer}
				removal.Writer.RelativePath = "current/Caf\u00e9.json"
				fixtureDocumentRemoval(t, &removal, images[1][0], images[1][1])
				targetGroup.Removal = removal.Removal
			}
			recorded, err := persistedDocumentBindingIntent(intent)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := recorded.canonical(); err == nil {
				t.Fatal("ordinary/retained16 binding decoding admitted a transfer")
			}
			if parsed, err := recorded.canonicalFromSource(&source); err != nil || !parsed.Equal(intent) {
				t.Fatal("source-qualified transition lost its closed transfer meaning", err)
			}
			wrongSource := source
			wrongSource.Authority = persistedPathAuthority(targetExact)
			if _, err := recorded.canonicalFromSource(&wrongSource); err == nil {
				t.Fatal("target observation was accepted as the historical source")
			}
			targetGroup.BindingIntents = []recoveryDocumentBindingIntent{recorded}
			content, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			journal, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
			if err != nil {
				t.Fatal(err)
			}
			reconstructCarrierImages(t, journal, fixture, images)
			if pending {
				journal, changed, err := journal.PromoteDocument(t.Context(), 1, []aggregate.DocumentAddress{address.Document()}, targetExact)
				if err != nil || !changed {
					t.Fatal("pending transfer did not promote with its source pair", err)
				}
				content, err = journal.Encode(t.Context(), fixture.codec)
				if err != nil {
					t.Fatal("promoted transfer could not encode", err)
				}
				promoted, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
				if err != nil {
					t.Fatal(err)
				}
				loaded := reconstructCarrierImages(t, promoted, fixture, images)
				target, _ := loaded.At(1)
				if len(target.Provisional()) != 0 || len(target.Claims().After()) != 1 {
					t.Fatal("promoted transfer retained pending claims or lost its target")
				}
			}
		})
	}
}
