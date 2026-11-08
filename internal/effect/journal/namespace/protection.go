package namespace

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

type ProtectionFailureKind = configuration.ProtectionFailureKind

const (
	ProtectionReserved   = configuration.ProtectionReserved
	ProtectionUnverified = configuration.ProtectionUnverified
)

type ProtectionUncertainty = configuration.ProtectionUncertainty

const (
	ProtectionLocationUnavailable   = configuration.ProtectionLocationUnavailable
	ProtectionEndpointUnavailable   = configuration.ProtectionEndpointUnavailable
	ProtectionDocumentUnavailable   = configuration.ProtectionDocumentUnavailable
	ProtectionDocumentIndeterminate = configuration.ProtectionDocumentIndeterminate
	ProtectionRegionIndeterminate   = configuration.ProtectionRegionIndeterminate
	ProtectionSelectorMissing       = configuration.ProtectionSelectorMissing
	ProtectionAllocationUnavailable = configuration.ProtectionAllocationUnavailable
	ProtectionWorkLimit             = configuration.ProtectionWorkLimit
)

// ProtectionError contains safe native identities, not resolved paths or file
// contents. Its cause remains available to phase-owned diagnostic boundaries.
type ProtectionError struct {
	kind        ProtectionFailureKind
	uncertainty ProtectionUncertainty
	address     aggregate.DocumentAddress
	host        target.Target
	reference   output.RootReference
	source      string
	reservation configuration.PathReservation
	endpoint    string
	documents   bool
	cause       error
}

func (failure *ProtectionError) Kind() ProtectionFailureKind { return failure.kind }
func (failure *ProtectionError) Uncertainty() ProtectionUncertainty {
	var allocation *physicalBudgetRefusal
	if failure.kind == ProtectionUnverified && errors.As(failure.cause, &allocation) {
		return ProtectionWorkLimit
	}
	return failure.uncertainty
}
func (failure *ProtectionError) Address() aggregate.DocumentAddress { return failure.address }
func (failure *ProtectionError) Host() target.Target                { return failure.host }
func (failure *ProtectionError) Reference() output.RootReference    { return failure.reference }
func (failure *ProtectionError) Source() string                     { return failure.source }
func (failure *ProtectionError) Endpoint() string {
	if failure.endpoint != "" {
		return failure.endpoint
	}
	if failure.reservation.Path() == "." {
		return "selected file"
	}
	return failure.reservation.Path()
}

func (failure *ProtectionError) Reservation() configuration.PathReservation {
	return failure.reservation
}
func (failure *ProtectionError) Unwrap() error { return failure.cause }

func (failure *ProtectionError) Error() string {
	if failure.kind == ProtectionReserved {
		return fmt.Sprintf("native document %q reaches %s %q, which belongs to %s; daem does not read or manage it", failure.address.AggregateRoot().String(), failure.host, failure.Endpoint(), failure.reservation.Owner())
	}
	location := "the " + configuration.NativeRootDescription(failure.reference)
	if failure.source == "default location" {
		location = "the default " + configuration.NativeRootDescription(failure.reference)
	} else if failure.source != "" && failure.source != "selected location" {
		location += " selected by " + failure.source
	}
	detail := location + " could not be checked"
	switch failure.Uncertainty() {
	case ProtectionEndpointUnavailable:
		detail = fmt.Sprintf("the %s %q endpoint could not be checked", failure.host, failure.Endpoint())
	case ProtectionDocumentUnavailable:
		detail = "the native document's route could not be checked"
	case ProtectionDocumentIndeterminate:
		detail = "the native document's future path cannot yet be compared unambiguously"
	case ProtectionRegionIndeterminate:
		detail = location + " has a future path that cannot yet be compared unambiguously"
		if failure.Endpoint() != "" {
			detail = fmt.Sprintf("the future location of %s %q cannot yet be compared unambiguously", failure.host, failure.Endpoint())
		}
	case ProtectionSelectorMissing:
		detail = location + " was not supplied for protection verification"
	case ProtectionAllocationUnavailable:
		detail = "the operation's shared filesystem-work allocation is unavailable"
	case ProtectionWorkLimit:
		detail = "verification reached the operation's filesystem-work limit"
	}
	if failure.documents {
		return fmt.Sprintf("cannot verify which native configuration rules apply to document %q: %s", failure.address.AggregateRoot().String(), detail)
	}
	return fmt.Sprintf("cannot verify that native document %q stays outside reserved %s data: %s", failure.address.AggregateRoot().String(), failure.host, detail)
}

type protectionRelation uint8

const (
	protectionDisjoint protectionRelation = iota
	protectionOverlap
	protectionIndeterminate
)

// Region comparison does not promote future entries to exact authority. The
// path model owns confinement and separation proofs; other overlap is unknown.
func protectionPathRelation(left, right mutation.DirectoryEntryAuthorityObservation) protectionRelation {
	leftExact, leftKnown := left.Exact()
	rightExact, rightKnown := right.Exact()
	if leftKnown && rightKnown {
		if leftExact.Contains(rightExact) || rightExact.Contains(leftExact) {
			return protectionOverlap
		}
		return protectionDisjoint
	}
	if !leftKnown {
		provisional, _ := left.Provisional()
		if rightKnown {
			if provisional.ConfinedWithin(rightExact) {
				return protectionOverlap
			}
			if !provisional.CouldOverlap(rightExact) {
				return protectionDisjoint
			}
		}
		leftExact = provisional.Namespace()
	}
	if !rightKnown {
		provisional, _ := right.Provisional()
		if leftKnown {
			if provisional.ConfinedWithin(leftExact) {
				return protectionOverlap
			}
			if !provisional.CouldOverlap(leftExact) {
				return protectionDisjoint
			}
		}
		rightExact = provisional.Namespace()
	}
	if leftExact.Contains(rightExact) || rightExact.Contains(leftExact) {
		return protectionIndeterminate
	}
	return protectionDisjoint
}

func observeProtectionPath(path string, budget rootedpath.PhysicalTraversalBudget) (mutation.DirectoryEntryAuthorityObservation, error) {
	value := observeProtectionLocation(path, budget)
	return value.observation, value.err
}

func observeProtectionLocation(path string, budget rootedpath.PhysicalTraversalBudget) physicalRootObservation {
	var value physicalRootObservation
	value.path, value.err = rootedpath.ResolveFutureReferentPathBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if value.err == nil {
		value.observation, value.err = mutation.ObserveDirectoryEntryAuthorityBounded(value.path, physicalwork.MaximumPhysicalPathDepth, budget)
	}
	return value
}

// protectionFailures keeps every cause for bounded evidence while presenting
// one actionable refusal, not a repeated message for every endpoint comparison.
type protectionFailures struct {
	causes []error
}

func (failures *protectionFailures) add(cause error) {
	if cause != nil {
		failures.causes = append(failures.causes, cause)
	}
}

func (failures *protectionFailures) result() error {
	if len(failures.causes) == 0 {
		return nil
	}
	return failures
}

func (failures *protectionFailures) Error() string   { return failures.causes[0].Error() }
func (failures *protectionFailures) Unwrap() []error { return slices.Clone(failures.causes) }

func (observer *physicalObserver) requireUnreserved(ctx context.Context, aliases []Alias) error {
	policies, err := configuration.NativeExclusionPolicies()
	if err != nil {
		return err
	}
	var unverified protectionFailures
	for _, policy := range policies {
		// Unknown hosts remain explicit inventory work, not checked-empty facts.
		// Final host admission requires their audited policies independently.
		if policy.Coverage() == configuration.ExclusionUnknown {
			continue
		}
		facets, err := policy.Facets()
		if err != nil {
			return err
		}
		for _, facet := range facets {
			if !facet.RequiresObservation() {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			selector, supplied := observer.selectors.selectors[facet.Reference()]
			if !supplied {
				unverified.add(&ProtectionError{kind: ProtectionUnverified, uncertainty: ProtectionSelectorMissing, address: aliases[0].address, host: policy.Target(), reference: facet.Reference(), cause: fmt.Errorf("required protection selector is missing")})
				continue
			}
			if err := observer.requireFacet(ctx, aliases, policy.Target(), facet, selector); err != nil {
				if canceled := ctx.Err(); canceled != nil {
					return canceled
				}
				var failure *ProtectionError
				if errors.As(err, &failure) && failure.kind == ProtectionReserved {
					return err
				}
				unverified.add(err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return unverified.result()
}

func (observer *physicalObserver) requireFacet(ctx context.Context, aliases []Alias, host target.Target, facet configuration.ExclusionFacet, selector ReservationSelector) error {
	if selector.absent {
		return ctx.Err()
	}
	budget := observer.budget
	unverified := func(address aggregate.DocumentAddress, uncertainty ProtectionUncertainty, reservation configuration.PathReservation, cause error) *ProtectionError {
		return &ProtectionError{kind: ProtectionUnverified, uncertainty: uncertainty, address: address, host: host, reference: facet.Reference(), source: selector.source, reservation: reservation, cause: cause}
	}
	indeterminate := func(address aggregate.DocumentAddress, selected mutation.DirectoryEntryAuthorityObservation, reservation configuration.PathReservation) *ProtectionError {
		uncertainty := ProtectionRegionIndeterminate
		if _, exact := selected.Exact(); !exact {
			uncertainty = ProtectionDocumentIndeterminate
		}
		return unverified(address, uncertainty, reservation, fmt.Errorf("physical protection membership is normalization-sensitive"))
	}
	// Resolve the literal root before joining relative catalog paths. Cleaning
	// the selector first would change a symlink followed by a parent component.
	resolved := observer.root(selector)
	if resolved.err != nil {
		return unverified(aliases[0].address, ProtectionLocationUnavailable, configuration.PathReservation{}, resolved.err)
	}
	physical := resolved.path
	var incomplete protectionFailures
	routes := make([]mutation.DirectoryEntryAuthorityObservation, len(aliases))
	for index, alias := range aliases {
		route, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(alias.root.selection.SelectedPath, filepath.FromSlash(alias.relative.Path())), physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			incomplete.add(unverified(alias.address, ProtectionDocumentUnavailable, configuration.PathReservation{}, err))
			continue
		}
		routes[index] = route
	}
	checkRegion := func(root mutation.DirectoryEntryAuthorityObservation, prefix string) error {
		for index, alias := range aliases {
			for _, selected := range []mutation.DirectoryEntryAuthorityObservation{routes[index], alias.observation} {
				if selected.Validate() != nil {
					continue
				}
				exactRoot, exact := root.Exact()
				selectedKey, selectedExact := selected.Exact()
				if exact && selectedExact && exactRoot.Contains(selectedKey) {
					relative, err := filepath.Rel(exactRoot.Key(), selectedKey.Key())
					if err != nil {
						return unverified(alias.address, ProtectionDocumentUnavailable, configuration.PathReservation{}, err)
					}
					if reservation, reserved := facet.ReservationAt(filepath.ToSlash(filepath.Join(prefix, relative))); reserved {
						return &ProtectionError{kind: ProtectionReserved, address: alias.address, host: host, reference: facet.Reference(), reservation: reservation, cause: configuration.ErrReserved}
					}
				} else if exact && !selectedExact {
					provisional, _ := selected.Provisional()
					relativePrefix, stable := provisional.StableRelativePrefix(exactRoot)
					if provisional.CouldOverlap(exactRoot) && (!stable || facet.CouldReserveBelow(filepath.ToSlash(filepath.Join(prefix, relativePrefix)))) {
						incomplete.add(indeterminate(alias.address, selected, configuration.PathReservation{}))
					}
				} else if protectionPathRelation(root, selected) != protectionDisjoint {
					incomplete.add(indeterminate(alias.address, selected, configuration.PathReservation{}))
				}
			}
		}
		return nil
	}
	if err := checkRegion(resolved.observation, ""); err != nil {
		return err
	}
	checkObservation := func(endpoint mutation.DirectoryEntryAuthorityObservation, reservation configuration.PathReservation, label string) error {
		for index, alias := range aliases {
			for _, selected := range []mutation.DirectoryEntryAuthorityObservation{routes[index], alias.observation} {
				if selected.Validate() != nil {
					continue
				}
				switch protectionPathRelation(endpoint, selected) {
				case protectionOverlap:
					return &ProtectionError{kind: ProtectionReserved, address: alias.address, host: host, reference: facet.Reference(), reservation: reservation, endpoint: label, cause: configuration.ErrReserved}
				case protectionIndeterminate:
					failure := indeterminate(alias.address, selected, reservation)
					failure.endpoint = label
					incomplete.add(failure)
				}
			}
		}
		return nil
	}
	checkEndpoint := func(path string, reservation configuration.PathReservation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		endpoint := observeProtectionLocation(path, budget)
		if endpoint.err != nil {
			incomplete.add(unverified(aliases[0].address, ProtectionEndpointUnavailable, reservation, endpoint.err))
		} else if err := checkObservation(endpoint.observation, reservation, ""); err != nil {
			return err
		}
		for _, suffix := range reservation.CompanionSuffixes() {
			label := reservation.Path() + suffix
			if reservation.Path() == "." {
				label = "selected file" + suffix
			}
			anchors := []string{path}
			if endpoint.err == nil && endpoint.path != path {
				anchors = append(anchors, endpoint.path)
			}
			for _, anchor := range anchors {
				if err := ctx.Err(); err != nil {
					return err
				}
				companion, err := observeProtectionPath(anchor+suffix, budget)
				if err != nil {
					failure := unverified(aliases[0].address, ProtectionEndpointUnavailable, reservation, err)
					failure.endpoint = label
					incomplete.add(failure)
					continue
				}
				if err := checkObservation(companion, reservation, label); err != nil {
					return err
				}
			}
		}
		return nil
	}
	checkReservations := func(reservations []configuration.PathReservation) error {
		for _, reservation := range reservations {
			path := filepath.Join(physical, filepath.FromSlash(reservation.Path()))
			if reservation.Path() == "." {
				path = selector.Path()
			}
			if err := checkEndpoint(path, reservation); err != nil {
				return err
			}
		}
		return nil
	}
	if err := checkReservations(facet.FixedReservations()); err != nil {
		return err
	}
	linkedFailure := func(label string, cause error) {
		reason := ProtectionEndpointUnavailable
		if observer.budget.work == nil {
			reason = ProtectionAllocationUnavailable
		}
		failure := unverified(aliases[0].address, reason, configuration.PathReservation{}, cause)
		failure.endpoint = label
		if label == "." {
			failure.endpoint = "root directory"
		}
		incomplete.add(failure)
	}
	nonDirectories := make(map[string]struct{})
	isNonDirectory := func(relative string) bool {
		for candidate := relative; candidate != "."; candidate = path.Dir(candidate) {
			if _, present := nonDirectories[candidate]; present {
				return true
			}
		}
		return false
	}
	directoryEntries := func(relative, label string) (rootedpath.DirectoryNames, bool) {
		if isNonDirectory(relative) {
			return rootedpath.DirectoryNames{}, false
		}
		candidate, err := rootedpath.ResolveFutureReferentPathBounded(filepath.Join(physical, filepath.FromSlash(relative)), physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			linkedFailure(label, err)
			return rootedpath.DirectoryNames{}, false
		}
		entries, err := observer.directory(ctx, candidate)
		if relative != "." && nonDirectoryObservation(candidate, err) {
			nonDirectories[relative] = struct{}{}
			return rootedpath.DirectoryNames{}, false
		}
		if err != nil {
			linkedFailure(label, err)
			return rootedpath.DirectoryNames{}, false
		}
		return entries, entries.Present()
	}
	for _, pattern := range facet.LinkedEntryDirectories() {
		directories := []string{pattern}
		if namePattern := filepath.Base(pattern); strings.ContainsAny(namePattern, "*?[") {
			parent := filepath.Dir(pattern)
			entries, present := directoryEntries(parent, pattern)
			if !present {
				continue
			}
			directories = nil
			for _, name := range entries.Names() {
				if matched, _ := filepath.Match(namePattern, name); matched {
					directories = append(directories, filepath.ToSlash(filepath.Join(parent, name)))
				}
			}
		}
		for _, directory := range directories {
			entries, present := directoryEntries(directory, pattern)
			if !present {
				continue
			}
			root, err := observeProtectionPath(entries.PhysicalPath(), budget)
			if err != nil {
				linkedFailure(pattern, err)
				continue
			}
			if err := checkRegion(root, directory); err != nil {
				return err
			}
			key, exact := root.Exact()
			if !exact {
				incomplete.add(indeterminate(aliases[0].address, root, configuration.PathReservation{}))
				continue
			}
			for _, name := range entries.Names() {
				if err := ctx.Err(); err != nil {
					return err
				}
				path := filepath.Join(entries.PhysicalPath(), name)
				route, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
				if err != nil {
					linkedFailure(pattern, err)
					continue
				}
				entry, exact := route.Exact()
				canonical, immediate := key.ImmediateChildName(entry)
				if !exact || !immediate {
					incomplete.add(indeterminate(aliases[0].address, route, configuration.PathReservation{}))
					continue
				}
				reservation, reserved := facet.ReservationAt(filepath.ToSlash(filepath.Join(directory, canonical)))
				if reserved {
					if err := checkEndpoint(path, reservation); err != nil {
						return err
					}
				}
			}
		}
	}
	observeNames := func(path string, reservation configuration.PathReservation) ([]string, bool) {
		entries, err := observer.directory(ctx, path)
		if err != nil {
			reason := ProtectionEndpointUnavailable
			if observer.budget.work == nil {
				reason = ProtectionAllocationUnavailable
			}
			incomplete.add(unverified(aliases[0].address, reason, reservation, err))
			return nil, false
		}
		return entries.Names(), true
	}
	var patterns []configuration.PathReservation
	if facet.HasRuntimePatterns() {
		if names, observed := observeNames(physical, configuration.PathReservation{}); observed {
			patterns = append(patterns, facet.RuntimeReservations(names)...)
		}
	}
	if directory := facet.ModuleDirectory(); directory != "" {
		reservation, _ := facet.ReservationAt(directory)
		module, err := observeProtectionPath(filepath.Join(physical, directory), budget)
		if err != nil {
			incomplete.add(unverified(aliases[0].address, ProtectionEndpointUnavailable, reservation, err))
		} else if err := checkRegion(module, directory); err != nil {
			return err
		}
		entries, observed := directoryEntries(directory, directory)
		names := entries.Names()
		var packages []string
		if observed {
			for _, name := range names {
				candidate, err := rootedpath.ResolveFutureReferentPathBounded(filepath.Join(physical, directory, name), physicalwork.MaximumPhysicalPathDepth, budget)
				if err != nil {
					incomplete.add(unverified(aliases[0].address, ProtectionEndpointUnavailable, reservation, err))
					continue
				}
				_, err = observer.directory(ctx, candidate)
				if nonDirectoryObservation(candidate, err) {
					continue
				}
				if err != nil {
					incomplete.add(unverified(aliases[0].address, ProtectionEndpointUnavailable, reservation, err))
					continue
				}
				packages = append(packages, name)
			}
		}
		if !isNonDirectory(directory) {
			patterns = append(patterns, facet.ModuleReservations(names, packages)...)
		}
	}
	if err := checkReservations(patterns); err != nil {
		return err
	}
	return incomplete.result()
}

// A non-directory candidate cannot supply package entrypoints. Joined cleanup
// or other failures still prevent treating the observation as irrelevant.
func nonDirectoryObservation(candidate string, err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !nonDirectoryObservation(candidate, cause) {
				return false
			}
		}
		return true
	}
	if failure, ok := err.(*rootedpath.Failure); ok {
		return failure.Kind() == rootedpath.FailureAncestorNotDirectory && failure.Path() == candidate
	}
	return false
}
