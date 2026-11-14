package qualification

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// RecoveryRootDemand distinguishes live host observations from committed source
// metadata. It contains no filesystem capability or permission to restore.
type RecoveryRootDemand struct {
	count        int
	live         []int
	retired      []int
	afterSources []aggregate.DocumentAddress
	afterOnly    bool
	valid        bool
}

func liveRootDemand(records journalrecord.LoadedDocuments) RecoveryRootDemand {
	indexes := make([]int, records.Len())
	for index := range indexes {
		indexes[index] = index
	}
	return RecoveryRootDemand{count: records.Len(), live: indexes, valid: true}
}

// DetermineRecoveryRootDemand uses completed record facts and authenticated
// persistence observations, before any historical host root is opened.
func DetermineRecoveryRootDemand(ctx context.Context, journal journalrecord.Native, records journalrecord.LoadedDocuments, state durable.Snapshot, registry ownership.Registry) (RecoveryRootDemand, error) {
	if ctx == nil {
		return RecoveryRootDemand{}, fmt.Errorf("recovery root demand requires context")
	}
	documents, err := journal.Documents()
	if err != nil {
		return RecoveryRootDemand{}, err
	}
	if len(documents) != records.Len() {
		return RecoveryRootDemand{}, fmt.Errorf("recovery root demand requires complete document facts")
	}
	before, after := state.Equal(journal.Before()), state.Equal(journal.After())
	if !before && !after {
		return RecoveryRootDemand{}, fmt.Errorf("native recovery state differs from both recorded Snapshots")
	}
	demand := RecoveryRootDemand{count: records.Len(), afterOnly: after && !before, valid: true}
	indexes, err := journaldocument.IndexSnapshotsForClaims(ctx, journal.Before(), journal.After(), journaldocument.NativeClaims)
	if err != nil {
		return RecoveryRootDemand{}, err
	}
	var routes []journaldocument.AliasRoute
	for index := range records.Len() {
		loaded, _ := records.At(index)
		routes = append(routes, loaded.Group().Routes()...)
	}
	for index := range records.Len() {
		if err := ctx.Err(); err != nil {
			return RecoveryRootDemand{}, err
		}
		loaded, present := records.At(index)
		if !present {
			return RecoveryRootDemand{}, fmt.Errorf("recovery root demand requires complete document facts")
		}
		group := loaded.Group()
		ownedSource := false
		for _, route := range group.Routes() {
			ownedSource = ownedSource || route.OwnedSource()
			if demand.afterOnly && route.OwnedSource() && !slices.ContainsFunc(routes, func(other journaldocument.AliasRoute) bool {
				return other.Address() == route.Address() && other.CurrentCarrier()
			}) {
				subjects, err := indexes.After().Subjects(ctx, []aggregate.DocumentAddress{route.Address()})
				if err != nil {
					return RecoveryRootDemand{}, err
				}
				if len(subjects) != 0 {
					demand.afterSources = append(demand.afterSources, route.Address())
				}
			}
		}
		if !demand.afterOnly || !ownedSource {
			demand.live = append(demand.live, index)
			continue
		}
		var afterSubjects int
		if aliases := group.AfterAliases(); len(aliases) != 0 {
			subjects, err := indexes.After().Subjects(ctx, aliases)
			if err != nil {
				return RecoveryRootDemand{}, err
			}
			afterSubjects = len(subjects)
		}
		claims := loaded.Claims()
		if afterSubjects != 0 || group.Comparison().HasSelectedSequences() {
			demand.live = append(demand.live, index)
		} else if len(claims.Before()) == 0 || len(claims.After()) != 0 || len(loaded.Provisional()) != 0 {
			return RecoveryRootDemand{}, fmt.Errorf("rootless source requires exact before-only claims")
		}
		if slices.EqualFunc(claims.Prepared(), claims.After(), ownership.Claim.Equal) {
			continue
		}
		retired, err := sourceClaimFinalizationApplied(registry, claims.Prepared(), claims.After())
		if err != nil {
			return RecoveryRootDemand{}, err
		}
		if retired {
			demand.retired = append(demand.retired, index)
		}
	}
	slices.SortFunc(demand.afterSources, journaldocument.CompareAliases)
	demand.afterSources = slices.Compact(demand.afterSources)
	return demand, ctx.Err()
}

// Compare this operation's whole own cover against its two recorded phases.
// Disjoint own rows and replacement foreign rows do not authorize replay.
func sourceClaimFinalizationApplied(registry ownership.Registry, expected, after []ownership.Claim) (bool, error) {
	if len(expected) == 0 {
		return false, fmt.Errorf("source retirement requires recorded claims")
	}
	change, err := ownership.NewDocumentClaimChange(expected, after)
	if err != nil {
		return false, err
	}
	var actual []ownership.Claim
	for _, address := range change.FootprintCover() {
		overlaps, err := registry.Overlapping(address)
		if err != nil {
			return false, err
		}
		for _, claim := range overlaps {
			if claim.Owner().Equal(expected[0].Owner()) && !slices.ContainsFunc(actual, claim.Equal) {
				actual = append(actual, claim)
			}
		}
	}
	canonical, err := ownership.NewRegistry(actual)
	if err != nil {
		return false, err
	}
	if slices.EqualFunc(canonical.Claims(), change.Target(), ownership.Claim.Equal) {
		return true, nil
	}
	if slices.EqualFunc(canonical.Claims(), change.Expected(), ownership.Claim.Equal) {
		return false, nil
	}
	return false, fmt.Errorf("source retirement differs from both exact recorded own phases")
}

func (demand RecoveryRootDemand) Equal(other RecoveryRootDemand) bool {
	return demand.SameRoots(other) && slices.Equal(demand.retired, other.retired)
}

func (demand RecoveryRootDemand) SameRoots(other RecoveryRootDemand) bool {
	return demand.valid && other.valid && demand.count == other.count && demand.afterOnly == other.afterOnly && slices.Equal(demand.live, other.live) && slices.Equal(demand.afterSources, other.afterSources)
}

func (demand RecoveryRootDemand) LiveIndexes() []int { return slices.Clone(demand.live) }

func (demand RecoveryRootDemand) CommittedAfter() bool { return demand.valid && demand.afterOnly }

func (demand RecoveryRootDemand) aliases(document journalrecord.Document) []journalrecord.DocumentAlias {
	var result []journalrecord.DocumentAlias
	for _, alias := range document.Aliases() {
		if binding, bound := alias.Binding(); demand.afterOnly && bound && configuration.NativeRootIsOwnedPoint(binding.RootReference()) {
			address, err := alias.Address()
			if err != nil || !slices.Contains(demand.afterSources, address) {
				continue
			}
		}
		result = append(result, alias)
	}
	return result
}

func (demand RecoveryRootDemand) DocumentAliases(journal journalrecord.Native) ([][]aggregate.DocumentAddress, error) {
	if !demand.valid {
		return nil, fmt.Errorf("recovery root demand is required")
	}
	result := make([][]aggregate.DocumentAddress, 0, len(demand.live))
	for _, index := range demand.live {
		document, err := journal.DocumentAt(index)
		if err != nil {
			return nil, err
		}
		var addresses []aggregate.DocumentAddress
		for _, alias := range demand.aliases(document) {
			address, err := alias.Address()
			if err != nil {
				return nil, err
			}
			if !slices.Contains(addresses, address) {
				addresses = append(addresses, address)
			}
		}
		result = append(result, addresses)
	}
	return result, nil
}

func (demand RecoveryRootDemand) RequiresRoot(journal journalrecord.Native, reference output.RootReference) (bool, error) {
	if !demand.valid {
		return false, fmt.Errorf("recovery root demand is required")
	}
	manifest, err := journal.ManifestRootReference()
	if err != nil {
		return false, err
	}
	if reference == manifest {
		return true, nil
	}
	for _, index := range demand.live {
		document, err := journal.DocumentAt(index)
		if err != nil {
			return false, err
		}
		pin, err := document.WriterPin()
		if err != nil {
			return false, err
		}
		if writer, bound := pin.RootReference(); bound && writer == reference && (!demand.afterOnly || !configuration.NativeRootIsOwnedPoint(writer)) {
			return true, nil
		}
		for _, alias := range demand.aliases(document) {
			binding, bound := alias.Binding()
			if bound {
				if binding.RootReference() == reference {
					return true, nil
				}
			} else {
				location, err := alias.Location()
				if err != nil {
					return false, err
				}
				if location.RootReference() == reference {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
