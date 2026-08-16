package extension

import (
	"strings"

	"github.com/isty2e/daem/internal/credentialtext"
)

// NPMPackageSpec is one structurally interpreted npm package operand. Package
// names, registry selectors, and npm aliases are grammar facts; opaque
// selectors remain package-owned but do not gain public identity authority.
type NPMPackageSpec struct {
	raw           string
	name          string
	selector      string
	hasSelector   bool
	selectorClass npmSelectorClass
}

type npmSelectorClass uint8

const (
	npmSelectorNone npmSelectorClass = iota
	npmSelectorRegistry
	npmSelectorAlias
	npmSelectorLocal
	npmSelectorRemote
	npmSelectorOpaque
)

// ParseNPMPackageSpec parses one package operand without the carrier's leading
// npm: source marker. It preserves opaque npm selectors for host pass-through
// while distinguishing the registry and alias forms that prove public package
// identity.
func ParseNPMPackageSpec(value string) (NPMPackageSpec, bool) {
	return parseNPMPackageSpec(value)
}

func parseNPMPackageSpec(value string) (NPMPackageSpec, bool) {
	name, selector, hasSelector, ok := splitNPMPackageSpec(value)
	if !ok {
		return NPMPackageSpec{}, false
	}
	result := NPMPackageSpec{
		raw:         value,
		name:        name,
		selector:    selector,
		hasSelector: hasSelector,
	}
	switch {
	case !hasSelector:
		result.selectorClass = npmSelectorNone
	case validNPMAliasSelector(selector):
		result.selectorClass = npmSelectorAlias
	case remoteNPMSelector(selector):
		result.selectorClass = npmSelectorRemote
	case localNPMSelector(selector):
		result.selectorClass = npmSelectorLocal
	case registryNPMSelector(selector):
		result.selectorClass = npmSelectorRegistry
	default:
		result.selectorClass = npmSelectorOpaque
	}
	return result, true
}

// Name returns the package identity selected by the outer package operand.
// For an npm alias this is the installed alias, not its registry target.
func (spec NPMPackageSpec) Name() string { return spec.name }

// HasSelector reports whether the package operand includes an explicit
// selector.
func (spec NPMPackageSpec) HasSelector() bool { return spec.hasSelector }

// DirectRegistry reports whether the operand is a direct registry package
// name with no selector or with a registry version, tag, or range selector.
func (spec NPMPackageSpec) DirectRegistry() bool {
	return spec.structurallyDirectRegistry() && spec.CredentialFree()
}

func (spec NPMPackageSpec) structurallyDirectRegistry() bool {
	return spec.name != "" &&
		(spec.selectorClass == npmSelectorNone ||
			spec.selectorClass == npmSelectorRegistry)
}

// Public reports whether the complete package operand proves a non-local
// registry identity. Valid npm aliases retain that property through their
// registry target.
func (spec NPMPackageSpec) Public() bool {
	return spec.CredentialFree() &&
		(spec.structurallyDirectRegistry() || spec.selectorClass == npmSelectorAlias)
}

// CredentialFree reports whether the raw and canonical package partitions agree
// and the owning selector grammar proves that the operand carries no inline
// credential.
func (spec NPMPackageSpec) CredentialFree() bool {
	if spec.name == "" || spec.raw == "" {
		return false
	}
	decoded, _, stable := credentialtext.CanonicalDecode(spec.raw)
	if !stable {
		return false
	}
	decodedSpec, ok := parseNPMPackageSpec(decoded)
	if !ok || !spec.sameAuthorityPartition(decodedSpec) {
		return false
	}
	return spec.credentialFreeText()
}

func (spec NPMPackageSpec) containsCredentialField() bool {
	if spec.name == "" || !spec.hasSelector {
		return false
	}
	if credentialtext.ContainsCredentialAssignment(spec.selector) {
		return true
	}
	return spec.selectorClass == npmSelectorOpaque &&
		credentialtext.ContainsCredential(spec.selector)
}

func (spec NPMPackageSpec) sameAuthorityPartition(other NPMPackageSpec) bool {
	return spec.name == other.name &&
		spec.hasSelector == other.hasSelector &&
		spec.selectorClass == other.selectorClass
}

func (spec NPMPackageSpec) credentialFreeText() bool {
	if spec.containsCredentialField() {
		return false
	}
	switch spec.selectorClass {
	case npmSelectorNone, npmSelectorRegistry:
		return true
	case npmSelectorAlias:
		target, ok := npmAliasTarget(spec.selector)
		if !ok {
			return false
		}
		parsed, ok := parseNPMPackageSpec(target)
		return ok && parsed.structurallyDirectRegistry() && !parsed.containsCredentialField()
	case npmSelectorLocal, npmSelectorRemote, npmSelectorOpaque:
		return credentialtext.InspectPasswordUserInfo(spec.selector) ==
			credentialtext.UserInfoAbsent
	default:
		return false
	}
}

func splitNPMPackageSpec(value string) (name string, selector string, hasSelector bool, ok bool) {
	if value == "" || strings.HasSuffix(value, "@") {
		return "", "", false, false
	}
	name = npmPackageName(value)
	if !validNPMPackageName(name) {
		return "", "", false, false
	}
	if name == value {
		return name, "", false, true
	}
	return name, value[len(name)+1:], true, true
}

func npmPackageName(value string) string {
	searchFrom := 0
	if strings.HasPrefix(value, "@") {
		searchFrom = 1
	}
	separator := strings.IndexByte(value[searchFrom:], '@')
	if separator < 0 {
		return value
	}
	separator += searchFrom
	if separator == searchFrom || separator == len(value)-1 {
		return value
	}
	return value[:separator]
}

func validNPMAliasSelector(selector string) bool {
	target, found := npmAliasTarget(selector)
	if !found {
		return false
	}
	_, targetSelector, hasSelector, ok := splitNPMPackageSpec(target)
	return ok && (!hasSelector || registryNPMSelector(targetSelector))
}

func npmAliasTarget(selector string) (string, bool) {
	if len(selector) < len("npm:") || !strings.EqualFold(selector[:len("npm:")], "npm:") {
		return "", false
	}
	return selector[len("npm:"):], true
}

func localNPMSelector(selector string) bool {
	lower := strings.ToLower(selector)
	if strings.HasPrefix(lower, "file:") ||
		strings.HasPrefix(selector, ".") ||
		strings.HasPrefix(selector, "~/") ||
		strings.HasPrefix(selector, "/") ||
		strings.HasPrefix(selector, `\`) ||
		len(selector) >= 2 && isASCIIAlpha(selector[0]) && selector[1] == ':' {
		return true
	}
	return strings.ContainsAny(selector, `/\`) ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar") ||
		strings.HasSuffix(lower, ".tar.gz")
}

func remoteNPMSelector(selector string) bool {
	colon := strings.IndexByte(selector, ':')
	if colon <= 0 || !isASCIIAlpha(selector[0]) {
		return false
	}
	for index := 1; index < colon; index++ {
		character := selector[index]
		if !isASCIIAlpha(character) &&
			(character < '0' || character > '9') &&
			character != '+' && character != '-' && character != '.' {
			return false
		}
	}
	switch strings.ToLower(selector[:colon]) {
	case "git", "git+http", "git+https", "git+ssh", "http", "https", "ssh", "github", "gitlab", "bitbucket", "gist":
		return true
	default:
		return false
	}
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
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

func validNPMPackageName(name string) bool {
	if name == "" ||
		strings.ContainsRune(name, '\\') ||
		strings.HasPrefix(name, ".") ||
		strings.HasSuffix(name, ".") {
		return false
	}
	if strings.HasPrefix(name, "@") {
		scope, packageName, ok := strings.Cut(strings.TrimPrefix(name, "@"), "/")
		return ok && validNPMPackageSegment(scope) &&
			validNPMPackageSegment(packageName)
	}
	return validNPMPackageSegment(name)
}

func validNPMPackageSegment(segment string) bool {
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
