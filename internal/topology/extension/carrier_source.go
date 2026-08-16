package extension

import (
	"fmt"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
)

// CarrierSourceClass is the structural source class selected by one
// carrier-native source grammar.
type CarrierSourceClass string

const (
	CarrierSourceMarketplace CarrierSourceClass = "marketplace"
	CarrierSourceNPM         CarrierSourceClass = "npm"
	CarrierSourceGit         CarrierSourceClass = "git"
	CarrierSourceLocal       CarrierSourceClass = "local"
	CarrierSourceHost        CarrierSourceClass = "host"
)

// CarrierSourceIdentityPrivacy classifies whether one carrier-native source
// identity is safe to disclose without machine-local provenance.
type CarrierSourceIdentityPrivacy string

const (
	CarrierSourceIdentityPublic  CarrierSourceIdentityPrivacy = "public"
	CarrierSourceIdentityPrivate CarrierSourceIdentityPrivacy = "private"
)

// RelationEvidenceClass describes the strongest passive relation identity
// that the selected carrier source can produce. It grants no management or
// mutation authority.
type RelationEvidenceClass string

const (
	RelationEvidenceSourceExact        RelationEvidenceClass = "source_exact"
	RelationEvidenceBoundedSameSubject RelationEvidenceClass = "bounded_same_subject"
	RelationEvidenceUnavailable        RelationEvidenceClass = "unavailable"
)

// CarrierSource is one immutable carrier-native source interpretation.
// Identity owns normalized source identity. RelationIdentity defaults to that
// identity but may be narrower when the host addresses an installed relation
// independently from its source provenance.
type CarrierSource struct {
	class            CarrierSourceClass
	identity         string
	relationIdentity string
	relationEvidence RelationEvidenceClass
	identityPrivacy  CarrierSourceIdentityPrivacy
}

// InterpretCarrierSource interprets only source grammars admitted by the
// selected carrier. Unsupported carriers remain unclassified.
func InterpretCarrierSource(key desiredextension.CarrierKey) (CarrierSource, error) {
	if err := key.Validate(); err != nil {
		return CarrierSource{}, err
	}
	switch key.Carrier() {
	case desiredextension.CarrierClaudeCodePlugin,
		desiredextension.CarrierCodexPlugin:
		privacy := CarrierSourceIdentityPublic
		if key.Carrier() == desiredextension.CarrierClaudeCodePlugin {
			selector, _ := key.Source().MarketplaceSelector()
			if !stableMarketplaceIdentityToken(selector.Plugin()) ||
				!stableMarketplaceIdentityToken(selector.Marketplace()) {
				privacy = CarrierSourceIdentityPrivate
			}
		}
		return CarrierSource{
			class:            CarrierSourceMarketplace,
			identity:         key.Source().Ref(),
			relationIdentity: key.Source().Ref(),
			relationEvidence: RelationEvidenceSourceExact,
			identityPrivacy:  privacy,
		}, nil
	case desiredextension.CarrierPiPackage:
		return interpretPiPackageSource(key.Source().Ref())
	case desiredextension.CarrierOpenCodePlugin:
		privacy := CarrierSourceIdentityPrivate
		if _, ok := OpenCodePluginPackageName(key.Source().Ref()); ok {
			privacy = CarrierSourceIdentityPublic
		}
		return CarrierSource{
			class:            CarrierSourceHost,
			identity:         key.Source().Ref(),
			relationIdentity: key.Source().Ref(),
			relationEvidence: RelationEvidenceSourceExact,
			identityPrivacy:  privacy,
		}, nil
	case desiredextension.CarrierAntigravityCLIPlugin:
		return interpretAntigravityCLIPluginSource(key.Source().Ref()), nil
	default:
		return CarrierSource{}, fmt.Errorf(
			"extension carrier %q has no admitted source interpretation",
			key.Carrier(),
		)
	}
}

// CarrierSourceClasses returns the closed source-class vocabulary interpreted
// for one carrier.
func CarrierSourceClasses(carrier desiredextension.Carrier) []CarrierSourceClass {
	switch carrier {
	case desiredextension.CarrierClaudeCodePlugin,
		desiredextension.CarrierCodexPlugin:
		return []CarrierSourceClass{CarrierSourceMarketplace}
	case desiredextension.CarrierPiPackage:
		return []CarrierSourceClass{CarrierSourceNPM, CarrierSourceGit, CarrierSourceLocal}
	case desiredextension.CarrierOpenCodePlugin:
		return []CarrierSourceClass{CarrierSourceHost}
	case desiredextension.CarrierAntigravityCLIPlugin:
		return []CarrierSourceClass{CarrierSourceMarketplace, CarrierSourceHost}
	default:
		return nil
	}
}

// Class returns the structural source class.
func (source CarrierSource) Class() CarrierSourceClass { return source.class }

// Identity returns the carrier-native source identity.
func (source CarrierSource) Identity() string { return source.identity }

// RelationIdentity returns the exact host-visible identity selected by this
// source. It may be narrower than source provenance when a host addresses an
// installed relation by name rather than by its installation selector.
func (source CarrierSource) RelationIdentity() string { return source.relationIdentity }

// RelationEvidence returns the passive relation-identity precision supported
// by this source interpretation.
func (source CarrierSource) RelationEvidence() RelationEvidenceClass {
	return source.relationEvidence
}

// IdentityPrivacy returns whether the exact source identity contains
// carrier-local or otherwise opaque host provenance.
func (source CarrierSource) IdentityPrivacy() CarrierSourceIdentityPrivacy {
	return source.identityPrivacy
}

// HostLoadIdentityPrivacy classifies one observed order identity using the
// grammar owned by the target's admitted extension-order carrier. Unknown,
// opaque, and local identities remain private.
func HostLoadIdentityPrivacy(
	carrier desiredextension.Carrier,
	identity string,
) CarrierSourceIdentityPrivacy {
	switch carrier {
	case desiredextension.CarrierOpenCodePlugin:
		name, ok := OpenCodePluginPackageName(identity)
		if ok && name == identity {
			return CarrierSourceIdentityPublic
		}
	case desiredextension.CarrierPiPackage:
		if piHostLoadIdentityIsPublic(identity) {
			return CarrierSourceIdentityPublic
		}
	}
	return CarrierSourceIdentityPrivate
}

// HostVisibleRelationKey returns the host-visible identity used to correlate one
// carrier relation. Most carriers preserve the authored source reference;
// carriers whose host inventory drops source provenance use the narrower
// interpreted relation identity.
func HostVisibleRelationKey(key desiredextension.CarrierKey) (string, error) {
	if err := key.Validate(); err != nil {
		return "", err
	}
	if key.Carrier() != desiredextension.CarrierAntigravityCLIPlugin {
		return key.Source().Ref(), nil
	}
	source, err := InterpretCarrierSource(key)
	if err != nil {
		return "", err
	}
	return source.RelationIdentity(), nil
}

func interpretPiPackageSource(source string) (CarrierSource, error) {
	if strings.HasPrefix(source, "npm:") {
		name, privacy, err := npmSourceIdentity(
			strings.TrimSpace(strings.TrimPrefix(source, "npm:")),
		)
		if err != nil {
			return CarrierSource{}, err
		}
		return CarrierSource{
			class:            CarrierSourceNPM,
			identity:         name,
			relationIdentity: name,
			relationEvidence: RelationEvidenceSourceExact,
			identityPrivacy:  privacy,
		}, nil
	}
	// Git grammar is the authority on git-shaped sources and runs before any
	// local interpretation, so direct scp spellings and other prefix-less git
	// forms are not claimed by the local fallback. Local semantics apply only
	// to what the grammar fails to parse.
	if gitSource, ok := desiredextension.ParseGitSource(source); ok {
		privacy := CarrierSourceIdentityPublic
		if !gitSource.Public() {
			privacy = CarrierSourceIdentityPrivate
		}
		return CarrierSource{
			class:            CarrierSourceGit,
			identity:         gitSource.Identity(),
			relationIdentity: gitSource.Identity(),
			relationEvidence: RelationEvidenceSourceExact,
			identityPrivacy:  privacy,
		}, nil
	}
	return CarrierSource{
		class:            CarrierSourceLocal,
		identity:         source,
		relationIdentity: source,
		relationEvidence: RelationEvidenceSourceExact,
		identityPrivacy:  CarrierSourceIdentityPrivate,
	}, nil
}

func interpretAntigravityCLIPluginSource(source string) CarrierSource {
	plugin, marketplace, found := strings.Cut(source, "@")
	if found &&
		!strings.Contains(marketplace, "@") &&
		stableMarketplaceIdentityToken(plugin) &&
		stableMarketplaceIdentityToken(marketplace) {
		return CarrierSource{
			class:            CarrierSourceMarketplace,
			identity:         source,
			relationIdentity: plugin,
			relationEvidence: RelationEvidenceBoundedSameSubject,
			identityPrivacy:  CarrierSourceIdentityPublic,
		}
	}
	return CarrierSource{
		class:            CarrierSourceHost,
		identity:         source,
		relationIdentity: source,
		relationEvidence: RelationEvidenceUnavailable,
		identityPrivacy:  CarrierSourceIdentityPrivate,
	}
}

// OpenCodePluginPackageName returns the canonical npm package identity for an
// admitted OpenCode package source. Opaque host-source values are not packages.
func OpenCodePluginPackageName(source string) (string, bool) {
	spec := strings.TrimPrefix(source, "npm:")
	parsed, ok := desiredextension.ParseNPMPackageSpec(spec)
	if !ok || !parsed.DirectRegistry() {
		return "", false
	}
	return parsed.Name(), true
}

func stableMarketplaceIdentityToken(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		asciiAlnum := (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9')
		if index == 0 && !asciiAlnum {
			return false
		}
		if !asciiAlnum && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func piHostLoadIdentityIsPublic(identity string) bool {
	if spec, found := strings.CutPrefix(identity, "npm:"); found {
		parsed, ok := desiredextension.ParseNPMPackageSpec(spec)
		return ok && !parsed.HasSelector() && parsed.Name() == spec
	}
	value, found := strings.CutPrefix(identity, "git:")
	if !found {
		return false
	}
	source, ok := desiredextension.ParseGitSource(identity)
	return ok && source.Public() && source.Identity() == value
}

func npmSourceIdentity(
	spec string,
) (string, CarrierSourceIdentityPrivacy, error) {
	if spec == "" {
		return "", "", fmt.Errorf("npm package source name is required")
	}
	identity := legacyNPMRelationIdentity(spec)
	parsed, ok := desiredextension.ParseNPMPackageSpec(spec)
	if !ok {
		return identity, CarrierSourceIdentityPrivate, nil
	}
	privacy := CarrierSourceIdentityPublic
	if !parsed.Public() {
		privacy = CarrierSourceIdentityPrivate
	}
	return parsed.Name(), privacy, nil
}

// legacyNPMRelationIdentity preserves the bounded same-subject diagnostic key
// for malformed npm-prefixed rows without treating that fallback as admitted
// package grammar.
func legacyNPMRelationIdentity(spec string) string {
	searchFrom := 0
	if strings.HasPrefix(spec, "@") {
		searchFrom = 1
	}
	separator := strings.IndexByte(spec[searchFrom:], '@')
	if separator < 0 {
		return spec
	}
	separator += searchFrom
	if separator == searchFrom || separator == len(spec)-1 {
		return spec
	}
	return spec[:separator]
}
