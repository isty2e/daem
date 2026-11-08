package configuration

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/text/cases"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type NativeEnvelope uint8

const (
	NativeDirectory NativeEnvelope = iota + 1
	NativeEndpoints
)

type NativeRootContract struct {
	reference output.RootReference
	host      target.Target
	envelope  NativeEnvelope
	endpoints []string
}

// NativeRootContracts distinguishes an exclusive configuration directory from
// a broad locator anchor that authorizes only particular native endpoints.
func NativeRootContracts() ([]NativeRootContract, error) {
	var result []NativeRootContract
	for _, entry := range []struct {
		root     rootFact
		host     target.Target
		envelope NativeEnvelope
	}{
		{piRoot, target.TargetPi, NativeDirectory},
		{codexRoot, target.TargetCodex, NativeDirectory},
		{claudePreferences, target.TargetClaudeCode, NativeDirectory},
		{claudeAccount, target.TargetClaudeCode, NativeEndpoints},
		{openCodeRoot, target.TargetOpenCode, NativeDirectory},
		{openCodeExtra, target.TargetOpenCode, NativeDirectory},
		{openCodeLegacy, target.TargetOpenCode, NativeDirectory},
		{antigravityRoot, target.TargetAntigravityCLI, NativeDirectory},
		{antigravityShared, target.TargetAntigravityCLI, NativeEndpoints},
	} {
		reference, err := entry.root.reference()
		if err != nil {
			return nil, err
		}
		contract := NativeRootContract{reference: reference, host: entry.host, envelope: entry.envelope}
		if entry.envelope == NativeEndpoints {
			for _, alias := range dedicatedDocumentAliases {
				if alias.root == entry.root && alias.scope == target.ScopeGlobal {
					contract.endpoints = append(contract.endpoints, alias.relative)
				}
			}
			if len(contract.endpoints) == 0 {
				return nil, fmt.Errorf("native endpoint envelope has no admitted endpoints")
			}
		}
		result = append(result, contract)
	}
	slices.SortFunc(result, func(left, right NativeRootContract) int {
		return left.reference.Compare(right.reference)
	})
	return result, nil
}

func (contract NativeRootContract) Reference() output.RootReference { return contract.reference }
func (contract NativeRootContract) Host() target.Target             { return contract.host }
func (contract NativeRootContract) Envelope() NativeEnvelope        { return contract.envelope }
func (contract NativeRootContract) Endpoints() []string             { return slices.Clone(contract.endpoints) }

// SupportsAliases distinguishes admitted document placements from additional
// configuration locations observed only for physical rules and protection.
func (contract NativeRootContract) SupportsAliases() bool {
	for _, layout := range nativeLayouts {
		reference, err := layout.root.reference()
		if err == nil && reference == contract.reference {
			return true
		}
	}
	for _, alias := range dedicatedDocumentAliases {
		reference, err := alias.root.reference()
		if err == nil && reference == contract.reference {
			return true
		}
	}
	return false
}

func NativeRootDescription(reference output.RootReference) string {
	for _, entry := range []struct {
		root rootFact
		name string
	}{
		{projectRoot, "manifest root"},
		{piRoot, "pi agent root"},
		{codexRoot, "codex home"},
		{claudePreferences, "claude preferences directory"},
		{claudeAccount, "claude account location"},
		{openCodeRoot, "opencode configuration directory"},
		{openCodeExtra, "opencode additional configuration directory"},
		{openCodeLegacy, "opencode home configuration directory"},
		{openCodeData, "opencode data directory"},
		{openCodeState, "opencode state directory"},
		{openCodeCache, "opencode cache directory"},
		{openCodeDatabase, "opencode database override"},
		{openCodeMetadata, "opencode plugin metadata override"},
		{antigravityRoot, "antigravity preferences directory"},
		{antigravityShared, "antigravity shared configuration location"},
	} {
		selected, err := entry.root.reference()
		if err == nil && selected == reference {
			return entry.name
		}
	}
	return "native host location"
}

type ExclusionCoverage uint8

const (
	ExclusionUnknown ExclusionCoverage = iota
	ExclusionKnownNone
	ExclusionAudited
)

// ExclusionPolicy preserves an unaudited host separately from a checked empty
// policy. Selection of a document or writer does not select protection coverage.
type ExclusionPolicy struct {
	target   target.Target
	coverage ExclusionCoverage
	facets   nativePathFacets
}

func NativeExclusionPolicies() ([]ExclusionPolicy, error) {
	var result []ExclusionPolicy
	for _, host := range target.SupportedTargets() {
		facets, present := nativePathPolicies[host]
		if !present {
			result = append(result, ExclusionPolicy{target: host})
			continue
		}
		requirements, err := protectionForFacets(facets)
		if err != nil {
			return nil, fmt.Errorf("native exclusions for %s: %w", host, err)
		}
		references, err := requirements.References()
		if err != nil {
			return nil, err
		}
		coverage := ExclusionAudited
		if len(references) == 0 {
			coverage = ExclusionKnownNone
		}
		result = append(result, ExclusionPolicy{target: host, coverage: coverage, facets: facets})
	}
	return result, nil
}

func (policy ExclusionPolicy) Target() target.Target       { return policy.target }
func (policy ExclusionPolicy) Coverage() ExclusionCoverage { return policy.coverage }

func (policy ExclusionPolicy) Facets() ([]ExclusionFacet, error) {
	if policy.coverage == ExclusionUnknown {
		return nil, fmt.Errorf("native exclusion coverage for %s has not been audited", policy.target)
	}
	result := make([]ExclusionFacet, 0, len(policy.facets))
	for _, facet := range policy.facets {
		reference, err := facet.root.reference()
		if err != nil {
			return nil, err
		}
		result = append(result, ExclusionFacet{reference: reference, fact: facet})
	}
	slices.SortFunc(result, func(left, right ExclusionFacet) int {
		return left.reference.Compare(right.reference)
	})
	return result, nil
}

// ExclusionFacet projects one canonical root-bound reservation table to the
// physical observer. It grants neither a location nor filesystem authority.
type ExclusionFacet struct {
	reference output.RootReference
	fact      nativePathFacet
}

func (facet ExclusionFacet) Reference() output.RootReference { return facet.reference }

func (facet ExclusionFacet) RequiresObservation() bool { return facet.fact.hasReservations() }

type PathReservation struct {
	path  string
	owner string
}

func (reservation PathReservation) Path() string  { return reservation.path }
func (reservation PathReservation) Owner() string { return reservation.owner }

func (facet ExclusionFacet) FixedReservations() []PathReservation {
	result := make([]PathReservation, 0, len(facet.fact.paths))
	for _, reservation := range facet.fact.paths {
		result = append(result, PathReservation{path: reservation.path, owner: reservation.owner})
	}
	if facet.fact.runtimeFiles {
		for _, stem := range daemonRuntimeStems {
			for _, suffix := range daemonRuntimeSuffixes {
				reservation, _ := facet.ReservationAt("app-server-daemon/" + stem + suffix)
				result = append(result, reservation)
			}
		}
	}
	return result
}

// ReservationAt always applies global rules: the observed physical region, not
// the spelling or scope of a document alias, determines these reservations.
func (facet ExclusionFacet) ReservationAt(relative string) (PathReservation, bool) {
	reservation, reserved := facet.fact.reservationAt(target.ScopeGlobal, relative)
	return PathReservation{path: reservation.path, owner: reservation.owner}, reserved
}

func (facet ExclusionFacet) ModuleDirectory() string  { return facet.fact.moduleDirectory }
func (facet ExclusionFacet) HasRuntimePatterns() bool { return facet.fact.runtimeFiles }

func (facet ExclusionFacet) CouldReserveBelow(prefix string) bool {
	if prefix == "" {
		return facet.RequiresObservation()
	}
	if _, reserved := facet.ReservationAt(prefix); reserved {
		return true
	}
	parts := strings.Split(prefix, "/")
	if directory := facet.ModuleDirectory(); directory != "" && parts[0] == directory && len(parts) <= 2 {
		return true
	}
	return facet.HasRuntimePatterns() && prefix == "app-server-daemon"
}

func (facet ExclusionFacet) RuntimeReservations(names []string) []PathReservation {
	if !facet.fact.runtimeFiles {
		return nil
	}
	var result []PathReservation
	for _, name := range names {
		// This is candidate discovery, not filesystem identity. The observer
		// must resolve the host spelling and compare its physical endpoint.
		canonical := cases.Fold().String(name)
		if versionedRuntimePath(canonical) {
			reservation, _ := facet.ReservationAt(canonical)
			result = append(result, reservation)
		}
	}
	return result
}

// ModuleReservations uses names from an immediate directory observation and
// package candidates independently established as directories or future paths.
func (facet ExclusionFacet) ModuleReservations(names, packages []string) []PathReservation {
	directory := facet.ModuleDirectory()
	if directory == "" {
		return nil
	}
	paths := make([]string, 0, len(names)+len(packages)*len(moduleEntryNames))
	for _, group := range [][]string{moduleEntryNames[:], moduleIgnoreNames[:]} {
		for _, name := range group {
			paths = append(paths, directory+"/"+name)
		}
	}
	for _, name := range names {
		if strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".js") {
			paths = append(paths, directory+"/"+name)
		}
	}
	for _, name := range packages {
		for _, entry := range moduleEntryNames {
			paths = append(paths, directory+"/"+name+"/"+entry)
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	result := make([]PathReservation, 0, len(paths))
	for _, path := range paths {
		reservation, _ := facet.ReservationAt(path)
		result = append(result, reservation)
	}
	return result
}

// KnownRootDocuments projects the existing document rules, including named
// profiles found by a bounded directory observation. It performs no I/O.
func KnownRootDocuments(reference output.RootReference, names []string) ([]DocumentSpec, error) {
	for _, root := range []rootFact{openCodeExtra, openCodeLegacy} {
		selected, err := root.reference()
		if err != nil {
			return nil, err
		}
		if reference == selected {
			reference, err = openCodeRoot.reference()
			if err != nil {
				return nil, err
			}
			break
		}
	}
	if err := reference.Validate(); err != nil {
		return nil, err
	}
	var result []DocumentSpec
	for context, layout := range nativeLayouts {
		selected, err := layout.root.reference()
		if err != nil {
			return nil, err
		}
		if selected != reference {
			continue
		}
		for _, rule := range documentRules {
			if rule.target != context.target || rule.scope != "" && rule.scope != context.scope || rule.dedicatedOnly {
				continue
			}
			paths := []string{rule.path}
			if rule.namedProfile {
				paths = nil
				for _, name := range names {
					if namedProfilePath(name) {
						paths = append(paths, name)
					}
				}
			}
			for _, path := range paths {
				if !layout.contains(path) {
					continue
				}
				spec, err := NewDocumentSpec(DocumentInput{Target: context.target, Scope: context.scope, Path: path})
				if err != nil {
					return nil, err
				}
				result = append(result, spec)
			}
		}
	}
	for _, alias := range dedicatedDocumentAliases {
		selected, err := alias.root.reference()
		if err != nil {
			return nil, err
		}
		if selected != reference {
			continue
		}
		destination, err := output.Parse(alias.alias)
		if err != nil {
			return nil, err
		}
		spec, err := DocumentSpecForAlias(AliasInput{Target: alias.target, Scope: alias.scope, Destination: destination})
		if err != nil {
			return nil, err
		}
		result = append(result, spec)
	}
	slices.SortFunc(result, func(left, right DocumentSpec) int {
		return cmp.Or(cmp.Compare(left.Target(), right.Target()), cmp.Compare(left.Scope(), right.Scope()), cmp.Compare(left.RootRelativePath(), right.RootRelativePath()), cmp.Compare(left.PolicyID(), right.PolicyID()))
	})
	return slices.Compact(result), nil
}

func RootDocumentsRequireNames(reference output.RootReference) bool {
	for context, layout := range nativeLayouts {
		selected, err := layout.root.reference()
		if err != nil || selected != reference {
			continue
		}
		for _, rule := range documentRules {
			if rule.target == context.target && (rule.scope == "" || rule.scope == context.scope) && rule.namedProfile {
				return true
			}
		}
	}
	return false
}
