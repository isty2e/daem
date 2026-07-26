package relationhost

import (
	"context"
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// Input identifies selected current and retained carrier identities to
// correlate against host-private passive inventories. OnlyCorrelation, when
// present, narrows observation to that exact expectation.
type Input struct {
	Paths                daempaths.Paths
	Lockfile             lock.File
	ManagedCarrierClaims []durablecarrier.ManagedCarrierClaim
	Selection            targetselection.Selection
	OnlyCorrelation      *relationobserve.CorrelationKey
}

// Observe reads admitted host-private inventories and returns only generic
// current-relation facts plus the paths consumed to derive them.
func Observe(ctx context.Context, input Input) (relationobserve.Batch, error) {
	if ctx == nil {
		return relationobserve.Batch{}, fmt.Errorf("relation observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return relationobserve.Batch{}, err
	}

	records, err := selectedCarrierRecords(
		input.Lockfile,
		input.ManagedCarrierClaims,
		input.Selection,
	)
	if err != nil {
		return relationobserve.Batch{}, err
	}
	if input.OnlyCorrelation != nil {
		if err := input.OnlyCorrelation.Validate(); err != nil {
			return relationobserve.Batch{}, fmt.Errorf("observation-only expectation: %w", err)
		}
		record, ok := recordForCorrelation(records, *input.OnlyCorrelation)
		if !ok {
			subject := input.OnlyCorrelation.Subject()
			return relationobserve.Batch{}, fmt.Errorf(
				"observation-only expectation %s/%s/%s managed key %q is not a selected carrier",
				subject.Kind(),
				subject.Namespace(),
				subject.Key(),
				input.OnlyCorrelation.ExpectedRelation().ManagedInstanceKey(),
			)
		}
		records = []carrierRecord{record}
	}

	catalog, err := defaultObserverCatalog()
	if err != nil {
		return relationobserve.Batch{}, err
	}
	combined := relationobserve.BatchSpec{}
	for _, observer := range catalog {
		carrierRecords := recordsForCarrier(records, observer.carrier)
		if len(carrierRecords) == 0 {
			continue
		}
		spec, err := observer.observe(input, carrierRecords)
		if err != nil {
			return relationobserve.Batch{}, err
		}
		combined.Correlations = append(combined.Correlations, spec.Correlations...)
		combined.AuthorityPaths = append(combined.AuthorityPaths, spec.AuthorityPaths...)
	}
	return relationobserve.NewBatch(combined)
}

type carrierRecord struct {
	key            relationobserve.CorrelationKey
	carrierKey     desiredextension.CarrierKey
	carrier        desiredextension.Carrier
	scope          target.Scope
	desiredPresent bool
}

type passiveObserver struct {
	carrier desiredextension.Carrier
	observe func(Input, []carrierRecord) (relationobserve.BatchSpec, error)
}

func newObserverCatalog(observers []passiveObserver) ([]passiveObserver, error) {
	catalog := append([]passiveObserver(nil), observers...)
	seen := make(map[desiredextension.Carrier]struct{}, len(catalog))
	for index, observer := range catalog {
		if _, err := desiredextension.ParseCarrier(string(observer.carrier)); err != nil {
			return nil, fmt.Errorf("relation observer catalog[%d] carrier: %w", index, err)
		}
		if observer.observe == nil {
			return nil, fmt.Errorf("relation observer catalog[%d] observer is required", index)
		}
		if _, exists := seen[observer.carrier]; exists {
			return nil, fmt.Errorf("relation observer carrier %q appears more than once", observer.carrier)
		}
		seen[observer.carrier] = struct{}{}
	}
	sort.Slice(catalog, func(left int, right int) bool {
		return catalog[left].carrier < catalog[right].carrier
	})
	return catalog, nil
}

func selectedCarrierRecords(
	locked lock.File,
	claims []durablecarrier.ManagedCarrierClaim,
	selection targetselection.Selection,
) ([]carrierRecord, error) {
	records := make([]carrierRecord, 0, len(locked.Locked.Subjects())+len(claims))
	byKey := make(map[relationobserve.CorrelationKey]int)
	appendIdentity := func(
		identity durablecarrier.ManagedCarrierIdentity,
		desiredPresent bool,
	) error {
		if err := identity.Validate(); err != nil {
			return err
		}
		if !selection.Includes(identity.Target()) {
			return nil
		}
		key, err := relationobserve.NewCorrelationKey(
			identity.RelationSubject(),
			identity.ExpectedRelation(),
		)
		if err != nil {
			return err
		}
		if index, exists := byKey[key]; exists {
			if records[index].carrierKey != identity.Carrier().Key() {
				return fmt.Errorf(
					"relation observation correlation %q maps to conflicting carrier identities",
					key.Subject(),
				)
			}
			records[index].desiredPresent =
				records[index].desiredPresent || desiredPresent
			return nil
		}
		byKey[key] = len(records)
		records = append(records, carrierRecord{
			key:            key,
			carrierKey:     identity.Carrier().Key(),
			carrier:        identity.Carrier().Family(),
			scope:          identity.Scope(),
			desiredPresent: desiredPresent,
		})
		return nil
	}
	for _, contract := range locked.Locked.Subjects() {
		identity, ok, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if err := appendIdentity(identity, true); err != nil {
			return nil, err
		}
	}
	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return nil, fmt.Errorf("managed carrier observation claim: %w", err)
		}
		if err := appendIdentity(claim.Identity(), false); err != nil {
			return nil, fmt.Errorf("managed carrier observation identity: %w", err)
		}
	}
	return records, nil
}

func recordForCorrelation(
	records []carrierRecord,
	key relationobserve.CorrelationKey,
) (carrierRecord, bool) {
	for _, record := range records {
		if record.key == key {
			return record, true
		}
	}
	return carrierRecord{}, false
}

func recordsForCarrier(records []carrierRecord, carrier desiredextension.Carrier) []carrierRecord {
	selected := make([]carrierRecord, 0)
	for _, record := range records {
		if record.carrier == carrier {
			selected = append(selected, record)
		}
	}
	return selected
}
