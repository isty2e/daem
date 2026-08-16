package extension

import (
	"net"
	"net/netip"
	"net/url"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/credentialtext"
)

// GitSource is one canonical structured interpretation of an extension git
// source: a validated repository locator plus an optional git ref. It is the
// single structural authority for git source syntax and disclosure privacy;
// carrier interpretation and presentation consume this result instead of
// re-parsing the authored reference. Which carriers admit git sources at all
// remains a carrier contract, not a property of this grammar.
type GitSource struct {
	host           string
	path           string
	ref            string
	credentialFree bool
	public         bool
}

// gitSourcePath keeps the carrier-visible repository identity together with
// the spelling from which security and privacy inspection must be derived.
// URL parsing decodes Path once, while EscapedPath retains the evidence needed
// to detect nested encodings without decoding a literal percent twice.
type gitSourcePath struct {
	identity    string
	observation string
}

func literalGitSourcePath(value string) gitSourcePath {
	return gitSourcePath{identity: value, observation: value}
}

func parsedURLGitSourcePath(parsed *url.URL) gitSourcePath {
	return gitSourcePath{
		identity:    strings.TrimPrefix(parsed.Path, "/"),
		observation: strings.TrimPrefix(parsed.EscapedPath(), "/"),
	}
}

// ParseGitSource parses one git source spelling. It admits the git:/github:
// shorthand prefixes, scp-style user@host:path addresses, and explicit git
// protocol URLs. Credential-bearing userinfo keeps structural Git identity but
// carries no execution or disclosure authority; ordinary transport users such
// as ssh git@ remain credential-free. Queries are rejected. A URL fragment
// becomes the ref unless the URL also carries an in-path ref suffix; the
// combination has no git spelling and is rejected. The returned privacy covers
// the complete source: locator and ref.
func ParseGitSource(source string) (GitSource, bool) {
	value := strings.TrimSpace(source)
	credentialInspection := credentialtext.InspectPasswordUserInfo(value)
	credentialFree := credentialInspection == credentialtext.UserInfoAbsent
	if strings.HasPrefix(value, "github:") {
		repository, ref := splitGitRef(strings.TrimPrefix(value, "github:"))
		return buildGitSource("github.com", literalGitSourcePath(repository), ref, credentialFree)
	}
	hasGitPrefix := strings.HasPrefix(value, "git:")
	if hasGitPrefix {
		value = strings.TrimSpace(strings.TrimPrefix(value, "git:"))
	} else if !hasExplicitGitProtocol(value) && !scpLikeGitSource(value) {
		return GitSource{}, false
	}

	repository, ref := splitGitRef(value)
	if _, host, path, ok := splitScpLikeGitSource(repository); ok {
		return buildGitSource(host, literalGitSourcePath(path), ref, credentialFree)
	}
	if hasExplicitGitProtocol(repository) {
		parsed, err := url.Parse(repository)
		if err != nil || parsed.Hostname() == "" {
			return GitSource{}, false
		}
		if parsed.RawQuery != "" ||
			parsed.Fragment != "" {
			// A surviving fragment means the URL combined an in-path ref
			// suffix with a fragment; splitGitRef consumes fragments only
			// when they are the sole ref spelling.
			return GitSource{}, false
		}
		return buildGitSource(
			parsed.Hostname(),
			parsedURLGitSourcePath(parsed),
			ref,
			credentialFree && !urlUserInfoCarriesCredential(parsed),
		)
	}
	if !hasGitPrefix {
		return GitSource{}, false
	}
	if credentialInspection == credentialtext.UserInfoPresent {
		if host, path, ok := splitSchemeLessCredentialGitSource(repository); ok {
			return buildGitSource(host, literalGitSourcePath(path), ref, false)
		}
	}
	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 || (!strings.Contains(parts[0], ".") && parts[0] != "localhost") {
		return GitSource{}, false
	}
	return buildGitSource(parts[0], literalGitSourcePath(parts[1]), ref, credentialFree)
}

func splitSchemeLessCredentialGitSource(value string) (string, string, bool) {
	separator := strings.LastIndexByte(value, '@')
	if separator <= 0 || !strings.ContainsRune(value[:separator], ':') {
		return "", "", false
	}
	tail := value[separator+1:]
	pathSeparator := strings.IndexAny(tail, ":/")
	if pathSeparator <= 0 || pathSeparator+1 >= len(tail) {
		return "", "", false
	}
	return tail[:pathSeparator], tail[pathSeparator+1:], true
}

// Identity returns the canonical host/path locator identity. It never
// includes the ref or any URL userinfo.
func (source GitSource) Identity() string { return source.host + "/" + source.path }

// CredentialFree reports whether the complete authored spelling has proven
// password-userinfo absence. Structurally recoverable legacy sources may be
// uninspectable and therefore carry no effect or disclosure authority.
func (source GitSource) CredentialFree() bool { return source.credentialFree }

// Public reports whether the complete authored source, locator and ref, is
// safe to disclose without machine-local provenance.
func (source GitSource) Public() bool { return source.public }

func buildGitSource(
	host string,
	path gitSourcePath,
	ref string,
	credentialFree bool,
) (GitSource, bool) {
	if strings.HasPrefix(path.identity, "/") {
		return GitSource{}, false
	}
	normalizedPath := strings.TrimSuffix(path.identity, ".git")
	if host == "" || normalizedPath == "" {
		return GitSource{}, false
	}
	decodedHost, hostSafe := inspectGitSourcePart(host, host, false)
	decodedPath, pathSafe := inspectGitSourcePart(
		normalizedPath,
		path.observation,
		true,
	)
	if !hostSafe || !pathSafe {
		return GitSource{}, false
	}
	if gitHostCarriesUserInfo(host) {
		return GitSource{}, false
	}
	return GitSource{
		host:           host,
		path:           normalizedPath,
		ref:            ref,
		credentialFree: credentialFree,
		public: credentialFree &&
			publicGitLocator(host, decodedHost, normalizedPath, decodedPath) &&
			publicGitRef(ref),
	}, true
}

func publicGitLocator(
	host string,
	decodedHost string,
	path string,
	decodedPath string,
) bool {
	for _, text := range []string{host, decodedHost, path, decodedPath} {
		if strings.IndexFunc(text, isUnsafeControl) >= 0 {
			return false
		}
	}
	return publicGitHost(host) &&
		publicGitHost(decodedHost) &&
		publicGitLocatorPath(path) &&
		publicGitLocatorPath(decodedPath)
}

func publicGitHost(host string) bool {
	hostname := gitPrivacyHostname(host)
	if hostname == "" {
		return false
	}
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return false
	}
	if hostname == "local" || strings.HasSuffix(hostname, ".local") {
		return false
	}
	if hostname == "home.arpa" || strings.HasSuffix(hostname, ".home.arpa") {
		return false
	}
	if address := net.ParseIP(hostname); address != nil {
		if hostname != address.String() {
			return false
		}
		return !gitIPIsNonGlobal(address)
	}
	if gitHostLooksLikeIPv4Alias(hostname) {
		return false
	}
	if strings.ContainsRune(hostname, ':') {
		return false
	}
	return strings.ContainsRune(hostname, '.')
}

// gitPrivacyHostname returns the hostname used for public-output classification.
// An optional :port is transport, not a privacy identity; shorthand host:port
// spellings must not bypass IP or special-use checks. Identity() keeps the
// authored host spelling, including any port. Embedded whitespace after
// host/port splitting is not a hostname.
func gitPrivacyHostname(host string) string {
	value := strings.TrimSpace(host)
	if hostname, _, err := net.SplitHostPort(value); err == nil {
		value = strings.TrimSpace(hostname)
	}
	value = strings.ToLower(strings.Trim(value, "[]"))
	value = strings.TrimRight(value, ".")
	if value == "" || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return ""
	}
	return value
}

func gitHostLooksLikeIPv4Alias(host string) bool {
	if !strings.ContainsRune(host, '.') {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !gitIPv4AliasLabel(label) {
			return false
		}
	}
	return true
}

func gitIPv4AliasLabel(label string) bool {
	if label == "" {
		return false
	}
	if len(label) > 2 && (label[0] == '0' && (label[1] == 'x' || label[1] == 'X')) {
		for index := 2; index < len(label); index++ {
			character := label[index]
			if (character < '0' || character > '9') &&
				(character < 'a' || character > 'f') &&
				(character < 'A' || character > 'F') {
				return false
			}
		}
		return true
	}
	for index := 0; index < len(label); index++ {
		if label[index] < '0' || label[index] > '9' {
			return false
		}
	}
	return true
}

func gitIPIsNonGlobal(address net.IP) bool {
	if address == nil {
		return true
	}
	parsed, ok := netip.AddrFromSlice(address)
	if !ok {
		return true
	}
	parsed = parsed.Unmap()
	if !parsed.IsGlobalUnicast() || parsed.IsPrivate() {
		return true
	}
	bits := -1
	nonGlobal := false
	for _, entry := range gitIANASpecialPurpose {
		if !entry.prefix.Contains(parsed) {
			continue
		}
		if entry.prefix.Bits() < bits {
			continue
		}
		bits = entry.prefix.Bits()
		nonGlobal = !entry.public
	}
	return nonGlobal
}

// gitIANASpecialPurpose is a dated IANA special-purpose snapshot used after
// Go's IsGlobalUnicast/IsPrivate filters. Longest-prefix match wins:
// Globally Reachable=false and empty/N/A flags are private; more-specific
// GR=true allocations are public exceptions. Snapshot retrieved 2026-08-16
// from the IPv4 and IPv6 special-purpose registries. It is not an open-world
// special-use DNS or routing registry.
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
type gitIANASpecialPurposePrefix struct {
	prefix netip.Prefix
	public bool
}

var gitIANASpecialPurpose = []gitIANASpecialPurposePrefix{
	{prefix: netip.MustParsePrefix("0.0.0.0/8")},
	{prefix: netip.MustParsePrefix("100.64.0.0/10")},
	{prefix: netip.MustParsePrefix("192.0.0.0/24")},
	{prefix: netip.MustParsePrefix("192.0.0.9/32"), public: true},
	{prefix: netip.MustParsePrefix("192.0.0.10/32"), public: true},
	{prefix: netip.MustParsePrefix("192.0.2.0/24")},
	{prefix: netip.MustParsePrefix("192.88.99.0/24")},
	{prefix: netip.MustParsePrefix("198.18.0.0/15")},
	{prefix: netip.MustParsePrefix("198.51.100.0/24")},
	{prefix: netip.MustParsePrefix("203.0.113.0/24")},
	{prefix: netip.MustParsePrefix("240.0.0.0/4")},
	{prefix: netip.MustParsePrefix("64:ff9b:1::/48")},
	{prefix: netip.MustParsePrefix("100::/64")},
	{prefix: netip.MustParsePrefix("100:0:0:1::/64")},
	{prefix: netip.MustParsePrefix("2001::/23")},
	{prefix: netip.MustParsePrefix("2001:1::1/128"), public: true},
	{prefix: netip.MustParsePrefix("2001:1::2/128"), public: true},
	{prefix: netip.MustParsePrefix("2001:1::3/128"), public: true},
	{prefix: netip.MustParsePrefix("2001:3::/32"), public: true},
	{prefix: netip.MustParsePrefix("2001:4:112::/48"), public: true},
	{prefix: netip.MustParsePrefix("2001:20::/28"), public: true},
	{prefix: netip.MustParsePrefix("2001:30::/28"), public: true},
	{prefix: netip.MustParsePrefix("2001:db8::/32")},
	{prefix: netip.MustParsePrefix("2002::/16")},
	{prefix: netip.MustParsePrefix("3fff::/20")},
	{prefix: netip.MustParsePrefix("5f00::/16")},
}

func publicGitLocatorPath(path string) bool {
	start := 0
	for {
		if gitLocatorAtPrivate(path[start:]) {
			return false
		}
		slash := strings.IndexByte(path[start:], '/')
		if slash < 0 {
			return true
		}
		start += slash + 1
	}
}

func gitLocatorAtPrivate(suffix string) bool {
	if suffix == "" {
		return false
	}
	segment := suffix
	if slash := strings.IndexByte(suffix, '/'); slash >= 0 {
		segment = suffix[:slash]
	}
	if gitRefHasSchemePrefix(segment) {
		return true
	}
	if gitRefIsWindowsPath(suffix) {
		return true
	}
	for _, prefix := range []string{"/", "./", "../", "~/", `\`} {
		if strings.HasPrefix(suffix, prefix) {
			return true
		}
	}
	return false
}

// PathSafeGitHost reports whether host can be a portable directory name in a
// git/<host>/<path> install layout. Pi clones use the identity host as-is, so
// characters that are not portable path components cannot be observed or
// removed after install.
func PathSafeGitHost(host string) bool {
	if host == "" || host != strings.ToLower(host) {
		return false
	}
	for index := 0; index < len(host); index++ {
		character := host[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' ||
			character == '-' ||
			character == '+' {
			continue
		}
		return false
	}
	return true
}

// publicGitRef classifies one git ref for disclosure. Ordinary refs,
// including slash-separated branch names, remain public; machine-local path
// shapes, scheme-prefixed locators, opaque URL or traversal forms, and
// control characters are private. Both the raw ref and its canonical
// decoded form are classified, and each hash-delimited part of the ref is
// classified on its own, so a private shape cannot hide behind a public
// prefix in spellings that combine a ref suffix with a fragment.
func publicGitRef(ref string) bool {
	if ref == "" {
		return true
	}
	if !publicGitRefParts(ref) {
		return false
	}
	decoded, _, stable := credentialtext.CanonicalDecode(ref)
	if !stable {
		return false
	}
	return decoded == ref || publicGitRefParts(decoded)
}

func publicGitRefParts(ref string) bool {
	for _, part := range strings.Split(ref, "#") {
		if !publicGitRefForm(part) {
			return false
		}
	}
	return true
}

func publicGitRefForm(ref string) bool {
	if strings.IndexFunc(ref, isUnsafeControl) >= 0 {
		return false
	}
	if strings.Contains(ref, "://") ||
		strings.ContainsRune(ref, '@') ||
		strings.ContainsRune(ref, '\\') ||
		!publicGitLocatorPath(ref) {
		return false
	}
	for _, segment := range strings.Split(ref, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

// gitRefHasSchemePrefix reports whether ref starts with an RFC-style scheme
// followed by ':', such as "file:/...". Scheme-prefixed refs locate
// machine-local or opaque state and are never safe for public disclosure.
func gitRefHasSchemePrefix(ref string) bool {
	colon := strings.IndexByte(ref, ':')
	if colon <= 0 || !isGitRefSchemeByte(ref[0], true) {
		return false
	}
	for index := 1; index < colon; index++ {
		if !isGitRefSchemeByte(ref[index], false) {
			return false
		}
	}
	return true
}

func isGitRefSchemeByte(value byte, first bool) bool {
	switch {
	case value >= 'A' && value <= 'Z', value >= 'a' && value <= 'z':
		return true
	case first:
		return false
	default:
		return value >= '0' && value <= '9' || value == '+' || value == '-' || value == '.'
	}
}

func gitRefIsWindowsPath(ref string) bool {
	if len(ref) < 3 || ref[1] != ':' || (ref[2] != '/' && ref[2] != '\\') {
		return false
	}
	drive := ref[0]
	return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
}

func hasExplicitGitProtocol(value string) bool {
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "ssh://") ||
		strings.HasPrefix(value, "git://") ||
		strings.HasPrefix(value, "git+http://") ||
		strings.HasPrefix(value, "git+https://") ||
		strings.HasPrefix(value, "git+ssh://")
}

// splitGitRef separates the repository locator from the trailing ref. The
// ref delimiter is the first '@' or '#' inside the path region: '@' covers
// git URL ref suffixes, '#' covers the documented shorthand ref spelling, so
// a machine-local suffix carried by a ref reaches ref privacy classification
// instead of blending into the repository path.
func splitGitRef(value string) (string, string) {
	if _, _, _, ok := splitScpLikeGitSource(value); ok {
		colon := strings.IndexByte(value, ':')
		path := value[colon+1:]
		if cut := strings.IndexAny(path, "@#"); cut >= 0 && cut+1 < len(path) {
			return value[:colon+1+cut], path[cut+1:]
		}
		return value, ""
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return value, ""
		}
		escapedPath := parsed.EscapedPath()
		if separator := strings.IndexByte(strings.TrimPrefix(escapedPath, "/"), '@'); separator >= 0 {
			offset := 0
			if strings.HasPrefix(escapedPath, "/") {
				offset = 1
			}
			index := offset + separator
			if index > offset && index+1 < len(escapedPath) {
				repositoryPath, repositoryErr := url.PathUnescape(escapedPath[:index])
				ref, refErr := url.PathUnescape(escapedPath[index+1:])
				if repositoryErr != nil || refErr != nil {
					return value, ""
				}
				parsed.Path = repositoryPath
				parsed.RawPath = escapedPath[:index]
				return strings.TrimSuffix(parsed.String(), "/"), ref
			}
		}
		if parsed.Fragment != "" {
			ref := parsed.Fragment
			parsed.Fragment = ""
			parsed.RawFragment = ""
			return strings.TrimSuffix(parsed.String(), "/"), ref
		}
		return value, ""
	}
	slash := strings.IndexByte(value, '/')
	if slash >= 0 {
		if separator := strings.IndexAny(value[slash+1:], "@#"); separator >= 0 {
			index := slash + 1 + separator
			if index > slash+1 && index+1 < len(value) {
				return value[:index], value[index+1:]
			}
		}
	}
	return value, ""
}

// scpLikeGitSource reports whether value is shaped like an scp-style git
// address, user@host:path, so git grammar can handle it structurally before
// generic URL parsing rejects the path colon.
func scpLikeGitSource(value string) bool {
	_, _, _, ok := splitScpLikeGitSource(value)
	return ok
}

// splitScpLikeGitSource splits one scp-style git address into its transport
// user, host, and path. The user precedes the first colon and the host sits
// between the last '@' before that colon and the colon itself, matching how
// git itself reads the form.
func splitScpLikeGitSource(value string) (user, host, path string, ok bool) {
	if strings.Contains(value, "://") {
		return "", "", "", false
	}
	colon := strings.IndexByte(value, ':')
	if colon <= 0 || colon+1 >= len(value) {
		return "", "", "", false
	}
	head := value[:colon]
	separator := strings.LastIndexByte(head, '@')
	if separator < 0 {
		return "", "", "", false
	}
	user = head[:separator]
	host = head[separator+1:]
	// An scp user is a login name: it cannot carry path segments, which is
	// what separates scp syntax from a ref that merely contains a colon.
	if user == "" || strings.ContainsRune(user, '/') {
		return "", "", "", false
	}
	if host == "" || strings.ContainsAny(host, "/\\") {
		return "", "", "", false
	}
	return user, host, value[colon+1:], true
}

func gitHostCarriesUserInfo(host string) bool {
	if strings.Contains(host, "@") {
		return true
	}
	decoded, _, _ := credentialtext.CanonicalDecode(host)
	return decoded != host && strings.Contains(decoded, "@")
}

func inspectGitSourcePart(
	value string,
	observation string,
	allowSlash bool,
) (string, bool) {
	decoded, _, stable := credentialtext.CanonicalDecode(observation)
	if !stable {
		return "", false
	}
	for _, candidate := range []string{value, decoded} {
		if strings.ContainsRune(candidate, '\x00') ||
			strings.Contains(candidate, `\`) ||
			strings.HasPrefix(candidate, "/") ||
			(!allowSlash && strings.Contains(candidate, "/")) {
			return "", false
		}
		for _, part := range strings.Split(candidate, "/") {
			if part == ".." {
				return "", false
			}
		}
	}
	return decoded, true
}
