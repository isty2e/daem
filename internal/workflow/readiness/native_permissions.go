package readiness

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

type NativeClaimRefusalKind = configuration.NativeClaimRefusalKind

const (
	NativeClaimProjectGlobal = configuration.NativeClaimProjectGlobal
	NativeClaimPhysicalRole  = configuration.NativeClaimPhysicalRole
	NativeClaimScopeTransfer = configuration.NativeClaimScopeTransfer
)

// NativeClaimRefusal describes new planning intent, never frozen recovery or
// retirement of a recorded claim. Its fields contain no resolved host paths.
type NativeClaimRefusal struct {
	kind     NativeClaimRefusalKind
	resource string
	address  aggregate.DocumentAddress
	global   output.RootReference
	role     configuration.DocumentSpec
	previous target.Scope
	cause    error
}

func (failure *NativeClaimRefusal) Kind() NativeClaimRefusalKind       { return failure.kind }
func (failure *NativeClaimRefusal) Resource() string                   { return failure.resource }
func (failure *NativeClaimRefusal) Address() aggregate.DocumentAddress { return failure.address }
func (failure *NativeClaimRefusal) GlobalLocation() (output.RootReference, bool) {
	return failure.global, failure.global.Validate() == nil
}
func (failure *NativeClaimRefusal) PhysicalRole() configuration.DocumentSpec { return failure.role }
func (failure *NativeClaimRefusal) Unwrap() error                            { return failure.cause }

func (failure *NativeClaimRefusal) Error() string {
	if failure.kind == NativeClaimScopeTransfer {
		return fmt.Sprintf("native document %q is already managed at %s scope; resource %q cannot transfer that management to %s scope in the same apply", failure.address.AggregateRoot().String(), failure.previous, failure.resource, failure.address.Scope())
	}
	if failure.kind == NativeClaimPhysicalRole {
		return fmt.Sprintf("host configuration resource %q cannot manage native document %q as declared: the same file is %s %q; %v", failure.resource, failure.address.AggregateRoot().String(), failure.role.Target(), failure.role.RootRelativePath(), failure.cause)
	}
	return fmt.Sprintf("project resource %q cannot manage native document %q: that document is also global host configuration in the %s; project claims do not grant global write authority", failure.resource, failure.address.AggregateRoot().String(), configuration.NativeRootDescription(failure.global))
}

func nativeClaimNamespaces(documents *journalpublication.NativeDocumentObservationBatch) map[aggregate.DocumentAddress]journalnamespace.DocumentNamespace {
	result := make(map[aggregate.DocumentAddress]journalnamespace.DocumentNamespace)
	for _, namespace := range documents.Evidence().Namespaces() {
		for _, address := range namespace.Addresses() {
			result[address] = namespace
		}
	}
	return result
}

func requireNativeSourcePermissions(sources []lockobserve.NativeSourceObservation, documents *journalpublication.NativeDocumentObservationBatch, before durable.Snapshot) error {
	byAddress := nativeClaimNamespaces(documents)
	history := recordedNativeFootprints(before)
	for _, source := range sources {
		placement, _ := source.SourceContract().HostConfigPlacement()
		address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
		if err != nil {
			return err
		}
		projections := source.Projections()
		if len(projections) == 0 {
			continue
		}
		namespace, present := byAddress[address]
		if !present {
			return fmt.Errorf("native claim requires its observed document namespace")
		}
		if err := namespace.RequireCompletePhysicalRules(); err != nil {
			return err
		}
		locations := namespace.GlobalLocations()
		footprints := make([]output.Footprint, 0, len(projections))
		for _, projection := range projections {
			footprint := output.WholePathFootprint()
			contribution, aggregateProjection, err := projection.ManagedAggregateContribution()
			if err != nil {
				return err
			}
			if aggregateProjection {
				footprint = contribution.Contribution().Footprint()
			}
			footprints = append(footprints, footprint)
			for _, role := range namespace.PhysicalRoles() {
				err := role.RequireWriterGrammar(placement.Grammar())
				if err == nil {
					err = profile.ValidatePhysicalHostConfigFootprint(role, footprint)
				}
				if err != nil {
					failure := &NativeClaimRefusal{kind: NativeClaimPhysicalRole, resource: source.SourceContract().EntityID().String(), address: address, role: role, cause: err}
					if len(locations) != 0 {
						failure.global = locations[0]
					}
					return failure
				}
			}
		}
		if address.Scope() == target.ScopeProject && len(locations) != 0 {
			return &NativeClaimRefusal{kind: NativeClaimProjectGlobal, resource: source.SourceContract().EntityID().String(), address: address, global: locations[0]}
		}
		for _, footprint := range footprints {
			if previous, occupied := priorNativeScope(namespace.Addresses(), address.Scope(), footprint, history); occupied {
				return &NativeClaimRefusal{kind: NativeClaimScopeTransfer, resource: source.SourceContract().EntityID().String(), address: address, previous: previous}
			}
		}
	}
	return nil
}

func recordedNativeFootprints(before durable.Snapshot) map[aggregate.DocumentAddress][]output.Footprint {
	result := make(map[aggregate.DocumentAddress][]output.Footprint)
	for _, state := range before.ManagedAggregates() {
		previous := state.Contribution().Address()
		result[previous.Document()] = append(result[previous.Document()], previous.Footprint())
	}
	for _, state := range before.ManagedPaths() {
		for _, consumer := range state.ConsumerTargets() {
			address, err := aggregate.NewDocumentAddress(consumer, state.Scope(), state.Destination())
			if err == nil {
				result[address] = append(result[address], output.WholePathFootprint())
			}
		}
	}
	return result
}

func priorNativeScope(aliases []aggregate.DocumentAddress, scope target.Scope, footprint output.Footprint, history map[aggregate.DocumentAddress][]output.Footprint) (target.Scope, bool) {
	for _, alias := range aliases {
		if alias.Scope() == scope {
			continue
		}
		if slices.ContainsFunc(history[alias], footprint.Overlaps) {
			return alias.Scope(), true
		}
	}
	return "", false
}

func requireNativeAggregatePermissions(desired []aggregate.SubjectContribution, documents *journalpublication.NativeDocumentObservationBatch) error {
	byAddress := nativeClaimNamespaces(documents)
	for _, item := range desired {
		address := item.Contribution().Address().Document()
		namespace, native := byAddress[address]
		if !native {
			continue
		}
		if err := namespace.RequireCompletePhysicalRules(); err != nil {
			return err
		}
		if address.Scope() != target.ScopeProject {
			continue
		}
		if locations := namespace.GlobalLocations(); len(locations) != 0 {
			return &NativeClaimRefusal{kind: NativeClaimProjectGlobal, resource: item.SubjectID().String(), address: address, global: locations[0]}
		}
	}
	return nil
}
