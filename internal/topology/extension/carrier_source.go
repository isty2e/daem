package extension

import (
	"fmt"
	"net/url"
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
	if piSourceIsLocal(source) {
		return CarrierSource{
			class:            CarrierSourceLocal,
			identity:         source,
			relationIdentity: source,
			relationEvidence: RelationEvidenceSourceExact,
			identityPrivacy:  CarrierSourceIdentityPrivate,
		}, nil
	}
	if identity, ok := gitSourceIdentity(source); ok {
		return identity, nil
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
	parsed, ok := parseNPMPackageSpec(spec)
	if !ok || (parsed.hasSelector && !registryNPMSelector(parsed.selector)) {
		return "", false
	}
	return parsed.name, true
}

type npmPackageSpec struct {
	name        string
	selector    string
	hasSelector bool
}

func parseNPMPackageSpec(spec string) (npmPackageSpec, bool) {
	if spec == "" || strings.HasSuffix(spec, "@") {
		return npmPackageSpec{}, false
	}
	name := npmRelationIdentity(spec)
	if !validOpenCodeNPMPackageName(name) {
		return npmPackageSpec{}, false
	}
	hasSelector := name != spec
	selector := ""
	if hasSelector {
		selector = spec[len(name)+1:]
	}
	return npmPackageSpec{name: name, selector: selector, hasSelector: hasSelector}, true
}

func registryNPMSelector(selector string) bool {
	if selector == "" || strings.TrimSpace(selector) != selector {
		return false
	}
	tokenStart := 0
	for index, character := range selector {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '-', character == '_':
		case character == '+', character == '*', character == '<':
		case character == '>', character == '~', character == '^':
		case character == '|':
			if (index == 0 || selector[index-1] != '|') &&
				(index+1 == len(selector) || selector[index+1] != '|') {
				return false
			}
			if index != 0 && selector[index-1] == '|' {
				tokenStart = index + 1
			}
		case character == '=':
			if index != tokenStart &&
				!(index == tokenStart+1 &&
					(selector[tokenStart] == '<' || selector[tokenStart] == '>')) {
				return false
			}
		case character == ' ':
			tokenStart = index + 1
		default:
			return false
		}
	}
	return true
}

func validOpenCodeNPMPackageName(name string) bool {
	if name == "" ||
		strings.ContainsRune(name, '\\') ||
		strings.HasPrefix(name, ".") ||
		strings.HasSuffix(name, ".") {
		return false
	}
	if strings.HasPrefix(name, "@") {
		scope, packageName, ok := strings.Cut(strings.TrimPrefix(name, "@"), "/")
		return ok && validOpenCodeNPMPackageSegment(scope) &&
			validOpenCodeNPMPackageSegment(packageName)
	}
	return validOpenCodeNPMPackageSegment(name)
}

func validOpenCodeNPMPackageSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	for _, character := range segment {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-', character == '_', character == '.':
		default:
			return false
		}
	}
	return true
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
		parsed, ok := parseNPMPackageSpec(spec)
		return ok && !parsed.hasSelector && parsed.name == spec
	}
	value, found := strings.CutPrefix(identity, "git:")
	if !found {
		return false
	}
	host, path, found := strings.Cut(value, "/")
	if !found {
		return false
	}
	source, ok := buildGitSource(host, path)
	return ok && source.Identity() == value
}

func npmSourceIdentity(
	spec string,
) (string, CarrierSourceIdentityPrivacy, error) {
	if spec == "" {
		return "", "", fmt.Errorf("npm package source name is required")
	}
	identity := npmRelationIdentity(spec)
	parsed, ok := parseNPMPackageSpec(spec)
	if !ok {
		return identity, CarrierSourceIdentityPrivate, nil
	}
	privacy := CarrierSourceIdentityPublic
	if parsed.hasSelector && !registryNPMSelector(parsed.selector) {
		privacy = CarrierSourceIdentityPrivate
	}
	return identity, privacy, nil
}

func npmRelationIdentity(spec string) string {
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

func piSourceIsLocal(source string) bool {
	for _, prefix := range []string{"npm:", "git:", "github:", "http:", "https:", "ssh:"} {
		if strings.HasPrefix(source, prefix) {
			return false
		}
	}
	return true
}

func gitSourceIdentity(source string) (CarrierSource, bool) {
	value := strings.TrimSpace(source)
	if strings.HasPrefix(value, "github:") {
		repository, _ := splitGitRef(strings.TrimPrefix(value, "github:"))
		return buildGitSource("github.com", repository)
	}
	hasGitPrefix := strings.HasPrefix(value, "git:")
	if hasGitPrefix {
		value = strings.TrimSpace(strings.TrimPrefix(value, "git:"))
	} else if !hasExplicitGitProtocol(value) {
		return CarrierSource{}, false
	}

	repository, _ := splitGitRef(value)
	if strings.HasPrefix(repository, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(repository, "git@"), ":", 2)
		if len(parts) != 2 {
			return CarrierSource{}, false
		}
		return buildGitSource(parts[0], parts[1])
	}
	if hasExplicitGitProtocol(repository) {
		parsed, err := url.Parse(repository)
		if err != nil || parsed.Hostname() == "" {
			return CarrierSource{}, false
		}
		return buildGitSource(parsed.Hostname(), strings.TrimPrefix(parsed.EscapedPath(), "/"))
	}
	if !hasGitPrefix {
		return CarrierSource{}, false
	}
	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 || (!strings.Contains(parts[0], ".") && parts[0] != "localhost") {
		return CarrierSource{}, false
	}
	return buildGitSource(parts[0], parts[1])
}

func hasExplicitGitProtocol(value string) bool {
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "ssh://") ||
		strings.HasPrefix(value, "git://")
}

func splitGitRef(value string) (string, string) {
	if strings.HasPrefix(value, "git@") {
		colon := strings.IndexByte(value, ':')
		if colon < 0 {
			return value, ""
		}
		if separator := strings.IndexByte(value[colon+1:], '@'); separator >= 0 {
			index := colon + 1 + separator
			if index > colon+1 && index+1 < len(value) {
				return value[:index], value[index+1:]
			}
		}
		return value, ""
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return value, ""
		}
		path := parsed.Path
		if separator := strings.IndexByte(strings.TrimPrefix(path, "/"), '@'); separator >= 0 {
			offset := 0
			if strings.HasPrefix(path, "/") {
				offset = 1
			}
			index := offset + separator
			if index > offset && index+1 < len(path) {
				ref := path[index+1:]
				parsed.Path = path[:index]
				return strings.TrimSuffix(parsed.String(), "/"), ref
			}
		}
		return value, ""
	}
	slash := strings.IndexByte(value, '/')
	if slash >= 0 {
		if separator := strings.IndexByte(value[slash+1:], '@'); separator >= 0 {
			index := slash + 1 + separator
			if index > slash+1 && index+1 < len(value) {
				return value[:index], value[index+1:]
			}
		}
	}
	return value, ""
}

func buildGitSource(host string, path string) (CarrierSource, bool) {
	if strings.HasPrefix(path, "/") {
		return CarrierSource{}, false
	}
	normalizedPath := strings.TrimPrefix(path, "/")
	normalizedPath = strings.TrimSuffix(normalizedPath, ".git")
	if host == "" || normalizedPath == "" || len(strings.Split(normalizedPath, "/")) < 2 {
		return CarrierSource{}, false
	}
	if unsafeGitSourcePart(host, false) || unsafeGitSourcePart(normalizedPath, true) {
		return CarrierSource{}, false
	}
	return CarrierSource{
		class:            CarrierSourceGit,
		identity:         host + "/" + normalizedPath,
		relationIdentity: host + "/" + normalizedPath,
		relationEvidence: RelationEvidenceSourceExact,
		identityPrivacy:  CarrierSourceIdentityPublic,
	}, true
}

func unsafeGitSourcePart(value string, allowSlash bool) bool {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return true
	}
	for _, candidate := range []string{value, decoded} {
		if strings.ContainsRune(candidate, '\x00') ||
			strings.Contains(candidate, `\`) ||
			strings.HasPrefix(candidate, "/") ||
			(!allowSlash && strings.Contains(candidate, "/")) {
			return true
		}
		for _, part := range strings.Split(candidate, "/") {
			if part == ".." {
				return true
			}
		}
	}
	return false
}
