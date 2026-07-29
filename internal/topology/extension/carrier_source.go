package extension

import (
	"fmt"
	"net/url"
	"regexp"
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

// RelationEvidenceClass describes the strongest passive relation identity
// that the selected carrier source can produce. It grants no management or
// mutation authority.
type RelationEvidenceClass string

const (
	RelationEvidenceSourceExact        RelationEvidenceClass = "source_exact"
	RelationEvidenceBoundedSameSubject RelationEvidenceClass = "bounded_same_subject"
	RelationEvidenceUnavailable        RelationEvidenceClass = "unavailable"
)

var npmSourcePattern = regexp.MustCompile(`^(@?[^@]+(?:/[^@]+)?)(?:@(.+))?$`)

// CarrierSource is one immutable carrier-native source interpretation.
// Identity owns normalized source identity. RelationIdentity defaults to that
// identity but may be narrower when the host addresses an installed relation
// independently from its source provenance.
type CarrierSource struct {
	class            CarrierSourceClass
	identity         string
	relationIdentity string
	relationEvidence RelationEvidenceClass
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
		return CarrierSource{
			class:            CarrierSourceMarketplace,
			identity:         key.Source().Ref(),
			relationIdentity: key.Source().Ref(),
			relationEvidence: RelationEvidenceSourceExact,
		}, nil
	case desiredextension.CarrierPiPackage:
		return interpretPiPackageSource(key.Source().Ref())
	case desiredextension.CarrierOpenCodePlugin:
		return CarrierSource{
			class:            CarrierSourceHost,
			identity:         key.Source().Ref(),
			relationIdentity: key.Source().Ref(),
			relationEvidence: RelationEvidenceSourceExact,
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
		name, err := npmSourceName(strings.TrimSpace(strings.TrimPrefix(source, "npm:")))
		if err != nil {
			return CarrierSource{}, err
		}
		return CarrierSource{
			class:            CarrierSourceNPM,
			identity:         name,
			relationIdentity: name,
			relationEvidence: RelationEvidenceSourceExact,
		}, nil
	}
	if piSourceIsLocal(source) {
		return CarrierSource{
			class:            CarrierSourceLocal,
			identity:         source,
			relationIdentity: source,
			relationEvidence: RelationEvidenceSourceExact,
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
	}, nil
}

func interpretAntigravityCLIPluginSource(source string) CarrierSource {
	plugin, marketplace, found := strings.Cut(source, "@")
	if found &&
		!strings.Contains(marketplace, "@") &&
		stableAntigravityPluginToken(plugin) &&
		stableAntigravityPluginToken(marketplace) {
		return CarrierSource{
			class:            CarrierSourceMarketplace,
			identity:         source,
			relationIdentity: plugin,
			relationEvidence: RelationEvidenceBoundedSameSubject,
		}
	}
	return CarrierSource{
		class:            CarrierSourceHost,
		identity:         source,
		relationIdentity: source,
		relationEvidence: RelationEvidenceUnavailable,
	}
}

func stableAntigravityPluginToken(value string) bool {
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

func npmSourceName(spec string) (string, error) {
	if spec == "" {
		return "", fmt.Errorf("npm package source name is required")
	}
	match := npmSourcePattern.FindStringSubmatch(spec)
	if len(match) == 0 {
		return spec, nil
	}
	return match[1], nil
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
