package credentialtext

import (
	"strings"
	"unicode/utf8"
)

// ContainsURLUserInfo reports whether value contains a URL authority carrying
// userinfo: a scheme occurrence followed by '@' before the first path,
// query, or fragment delimiter, or a password-bearing user:password@ locator
// that has no :// marker. The scan is textual, so userinfo shapes survive
// URL parse failures, and both the raw and the canonical decoded forms are
// checked, so encoded userinfo such as "%40" is recognized. This
// context-free boundary treats an ambiguous terminal user:password@host as
// credential-bearing. Package and marketplace exceptions require their owning
// grammar and are intentionally unavailable at this boundary.
func ContainsURLUserInfo(value string) bool {
	return containsUserInfo(value, false)
}

// UserInfoInspection is the closed result of password-userinfo inspection.
// Only UserInfoAbsent proves that a value is credential-free.
type UserInfoInspection uint8

const (
	UserInfoUninspectable UserInfoInspection = iota
	UserInfoAbsent
	UserInfoPresent
)

// InspectPasswordUserInfo distinguishes proven absence, presence, and text
// whose bounded canonical or host grammar cannot be inspected completely.
func InspectPasswordUserInfo(value string) UserInfoInspection {
	return inspectUserInfo(value, true)
}

func containsUserInfo(value string, passwordOnly bool) bool {
	return inspectUserInfo(value, passwordOnly) == UserInfoPresent
}

func inspectUserInfo(value string, passwordOnly bool) UserInfoInspection {
	inspect := func(candidate string) UserInfoInspection {
		result := UserInfoAbsent
		visitUserInfoObservations(candidate, func(observation userInfoObservation) bool {
			if !observation.inspectable {
				result = UserInfoUninspectable
				return true
			}
			if !passwordOnly || observation.password {
				result = UserInfoPresent
				return false
			}
			return true
		})
		return result
	}
	if !utf8.ValidString(value) {
		return UserInfoUninspectable
	}
	raw := inspect(value)
	decoded, _, stable := CanonicalDecode(value)
	if raw == UserInfoPresent {
		return UserInfoPresent
	}
	if !stable || !utf8.ValidString(decoded) {
		return UserInfoUninspectable
	}
	canonical := UserInfoAbsent
	if decoded != value {
		canonical = inspect(decoded)
	}
	if canonical == UserInfoPresent {
		return UserInfoPresent
	}
	if raw == UserInfoUninspectable || canonical == UserInfoUninspectable {
		return UserInfoUninspectable
	}
	return UserInfoAbsent
}

type userInfoObservation struct {
	span        textSpan
	password    bool
	credential  bool
	inspectable bool
}

// visitUserInfoObservations is the single textual authority for URL and
// scheme-less userinfo. Presence checks and redaction spans derive from these
// observations, so recognizing a credential cannot diverge from removing it.
// The visitor may stop the scan; presence checks therefore use constant
// auxiliary memory and stop at the first relevant observation.
func visitUserInfoObservations(value string, visit func(userInfoObservation) bool) {
	if !visitURLUserInfoObservations(value, visit) {
		return
	}
	visitSchemeLessPasswordUserInfoObservations(value, visit)
}

func visitURLUserInfoObservations(value string, visit func(userInfoObservation) bool) bool {
	searchStart := 0
	for {
		marker := strings.Index(value[searchStart:], "://")
		if marker < 0 {
			return true
		}
		marker += searchStart
		schemeStart := marker
		for schemeStart > 0 && isURLSchemeByte(value[schemeStart-1]) {
			schemeStart--
		}
		authorityStart := marker + 3
		authorityEnd := len(value)
		if end := strings.IndexAny(value[authorityStart:], "/?# \t\r\n"); end >= 0 {
			authorityEnd = authorityStart + end
		}
		if separator := strings.LastIndexByte(value[authorityStart:authorityEnd], '@'); separator >= 0 {
			userinfo := value[authorityStart : authorityStart+separator]
			password := strings.Contains(userinfo, ":")
			if !visit(userInfoObservation{
				span:        textSpan{start: authorityStart, end: authorityStart + separator},
				password:    password,
				credential:  password || httpTransportScheme(value[schemeStart:marker]),
				inspectable: true,
			}) {
				return false
			}
		}
		searchStart = marker + 3
	}
}

// visitSchemeLessPasswordUserInfoObservations reports user:password@host locators
// inside arbitrary text, including terminal locators and locators enclosed by
// balanced square brackets. Token and locator-component scans only move
// forward; each disjoint component is parsed a constant number of times. URL
// syntax in one component therefore cannot blind a later query or fragment
// component, and incomplete bracketed hosts cannot trigger growing-prefix
// rescans.
func visitSchemeLessPasswordUserInfoObservations(
	value string,
	visit func(userInfoObservation) bool,
) bool {
	for tokenStart := 0; tokenStart < len(value); {
		for tokenStart < len(value) && schemeLessTokenBoundary(value[tokenStart]) {
			tokenStart++
		}
		tokenEnd := tokenStart
		for tokenEnd < len(value) && !schemeLessTokenBoundary(value[tokenEnd]) {
			tokenEnd++
		}
		if !visitSchemeLessPasswordUserInfoTokenObservations(value, tokenStart, tokenEnd, visit) {
			return false
		}
		tokenStart = tokenEnd + 1
	}
	return true
}

func visitSchemeLessPasswordUserInfoTokenObservations(
	value string,
	tokenStart int,
	tokenEnd int,
	visit func(userInfoObservation) bool,
) bool {
	if tokenStart >= tokenEnd {
		return true
	}
	for tokenStart < tokenEnd && value[tokenStart] == '[' {
		closingOffset := strings.IndexByte(value[tokenStart+1:tokenEnd], ']')
		if closingOffset < 0 {
			tokenStart++
			break
		}
		closing := tokenStart + 1 + closingOffset
		if !visitSchemeLessPasswordUserInfoRangeObservations(
			value,
			tokenStart+1,
			closing,
			visit,
		) {
			return false
		}
		tokenStart = closing + 1
	}
	return visitSchemeLessPasswordUserInfoRangeObservations(value, tokenStart, tokenEnd, visit)
}

func visitSchemeLessPasswordUserInfoRangeObservations(
	value string,
	start int,
	end int,
	visit func(userInfoObservation) bool,
) bool {
	for componentStart := start; componentStart < end; {
		componentEnd := componentStart
		for componentEnd < end && !schemeLessComponentBoundary(value[componentEnd]) {
			componentEnd++
		}
		if observation, ok := schemeLessPasswordUserInfoObservation(
			value,
			componentStart,
			componentEnd,
		); ok {
			if !visit(observation) {
				return false
			}
		}
		componentStart = componentEnd + 1
	}
	return true
}

func schemeLessPasswordUserInfoObservation(
	value string,
	componentStart int,
	componentEnd int,
) (userInfoObservation, bool) {
	if componentStart >= componentEnd {
		return userInfoObservation{}, false
	}
	lastAtOffset := strings.LastIndexByte(value[componentStart:componentEnd], '@')
	if lastAtOffset <= 0 {
		return userInfoObservation{}, false
	}
	lastAt := componentStart + lastAtOffset
	credentialStart := componentStart
	head := value[credentialStart:lastAt]
	if strings.HasPrefix(head, "npm:") {
		// npm: is a case-sensitive source namespace, not credential data.
		// Remove only the namespace from this observation; the remaining
		// payload is still inspected normally, so an opaque
		// npm:user:password@host lookalike cannot gain an exemption.
		credentialStart += len("npm:")
		head = head[len("npm:"):]
	}
	prefixed := false
	switch {
	case hasFoldedPrefix(head, "git:"):
		prefixed = true
		credentialStart += len("git:")
		head = head[len("git:"):]
	case hasFoldedPrefix(head, "github:"):
		prefixed = true
		credentialStart += len("github:")
		head = head[len("github:"):]
	}
	colon := strings.IndexByte(head, ':')
	if colon <= 0 || colon >= len(head)-1 {
		if colon >= 0 {
			return uninspectablePasswordUserInfoObservation(credentialStart, lastAt), true
		}
		return userInfoObservation{}, false
	}
	hostSuffix, ok := classifySchemeLessHost(value[lastAt+1 : componentEnd])
	if !ok {
		return uninspectablePasswordUserInfoObservation(credentialStart, lastAt), true
	}
	switch hostSuffix {
	case schemeLessHostOnly:
	case schemeLessHostPort:
	case schemeLessHostSCPPath:
		if !prefixed {
			return uninspectablePasswordUserInfoObservation(credentialStart, lastAt), true
		}
	default:
		return userInfoObservation{}, false
	}
	return userInfoObservation{
		span:        textSpan{start: credentialStart, end: lastAt},
		password:    true,
		credential:  true,
		inspectable: true,
	}, true
}

func uninspectablePasswordUserInfoObservation(start int, end int) userInfoObservation {
	return userInfoObservation{
		span:        textSpan{start: start, end: end},
		password:    true,
		credential:  true,
		inspectable: false,
	}
}

func schemeLessComponentBoundary(value byte) bool {
	switch value {
	case '/', '\\', '?', '#':
		return true
	default:
		return false
	}
}

func schemeLessTokenBoundary(value byte) bool {
	if isASCIISpace(value) {
		return true
	}
	switch value {
	case '"', '\'', '(', ')', '{', '}', '<', '>', ',', ';':
		return true
	default:
		return false
	}
}

type schemeLessHostSuffix uint8

const (
	schemeLessHostInvalid schemeLessHostSuffix = iota
	schemeLessHostOnly
	schemeLessHostPort
	schemeLessHostSCPPath
)

// classifySchemeLessHost recognizes one complete host spelling with an
// optional decimal port or an scp-style path delimiter. It scans bracketed
// hosts once instead of retrying each interior colon against a growing prefix.
func classifySchemeLessHost(value string) (schemeLessHostSuffix, bool) {
	if value == "" {
		return schemeLessHostInvalid, false
	}
	remainder := ""
	if value[0] == '[' {
		closingOffset := strings.IndexByte(value[1:], ']')
		if closingOffset <= 0 {
			return schemeLessHostInvalid, false
		}
		closing := closingOffset + 1
		if strings.ContainsAny(value[1:closing], "@[]?#\\") {
			return schemeLessHostInvalid, false
		}
		remainder = value[closing+1:]
	} else {
		hostEnd := strings.IndexByte(value, ':')
		if hostEnd < 0 {
			hostEnd = len(value)
		}
		if hostEnd == 0 || !validSchemeLessHostName(value[:hostEnd]) {
			return schemeLessHostInvalid, false
		}
		remainder = value[hostEnd:]
	}
	if remainder == "" {
		return schemeLessHostOnly, true
	}
	if remainder[0] != ':' {
		return schemeLessHostInvalid, false
	}
	suffix := remainder[1:]
	if suffix != "" && allASCIIDigits(suffix) {
		return schemeLessHostPort, true
	}
	return schemeLessHostSCPPath, true
}

func validSchemeLessHostName(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			character == '.' || character == '-' || character == '_' || character == '+' {
			// RFC reg-name also admits '+'. Other sub-delimiters remain
			// token grammar and do not prove a complete host here.
			continue
		}
		return false
	}
	return value != ""
}

func allASCIIDigits(value string) bool {
	for index := 0; index < len(value); index++ {
		if !isASCIIDigit(value[index]) {
			return false
		}
	}
	return true
}

func hasFoldedPrefix(value string, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}

// userInfoCredentialSpans returns every userinfo span that carries a password
// or belongs to an http(s) transport. The host stays visible.
func userInfoCredentialSpans(value string) []textSpan {
	var spans []textSpan
	visitUserInfoObservations(value, func(observation userInfoObservation) bool {
		if observation.credential {
			spans = append(spans, observation.span)
		}
		return true
	})
	return spans
}

func httpTransportScheme(scheme string) bool {
	lowerScheme := strings.ToLower(scheme)
	return lowerScheme == "http" ||
		lowerScheme == "https" ||
		strings.HasSuffix(lowerScheme, "+http") ||
		strings.HasSuffix(lowerScheme, "+https")
}

func isURLSchemeByte(value byte) bool {
	switch {
	case value >= 'A' && value <= 'Z', value >= 'a' && value <= 'z', value >= '0' && value <= '9':
		return true
	default:
		return value == '+' || value == '-' || value == '.'
	}
}
