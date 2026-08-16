// Package credentialtext recognizes credential-bearing key/value text without
// depending on format-specific delimiter lists.
package credentialtext

import (
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type valueSpan uint8

const (
	valueToken valueSpan = iota
	// valueLine withholds a line-scoped credential value. The rest of the
	// opening line is the minimum extent. Every unescaped quote that
	// opens inside the current extent extends it to the closing quote and
	// the remainder of that closing line; an unclosed quote withholds the
	// rest of the text. Authorization headers, HTTP auth-scheme values,
	// and uninspectable keys share this extent; other token-shaped keys
	// keep valueToken.
	valueLine
)

// Span is a raw-byte redaction range. An empty Marker uses the caller-supplied
// default at render time.
type Span struct {
	Start  int
	End    int
	Marker string
}

type field struct {
	valueStart int
	valueEnd   int
}

// ContainsCredential reports whether value contains a credential-bearing key
// followed by ':' or '=' at an identifier boundary. Both the raw value and
// its bounded fixed-point decoded inspection form are checked, so
// percent-encoded delimiters or key characters cannot conceal a credential
// shape. The scan is best-effort for forms that do not stabilize; admission
// and projection boundaries own the fail-closed handling of unresolved
// escapes.
func ContainsCredential(value string) bool {
	decoded, _, _ := CanonicalDecode(value)
	return containsField(value, true, false) ||
		decoded != value && containsField(decoded, true, false)
}

// ContainsCredentialAssignment reports whether value contains a
// credential-bearing key followed by '='. It is intended for grammars where
// ':' is structural data rather than a field delimiter.
func ContainsCredentialAssignment(value string) bool {
	decoded, _, _ := CanonicalDecode(value)
	return containsField(value, true, true) ||
		decoded != value && containsField(decoded, true, true)
}

// ContainsAssignment reports whether value contains an identifier followed by
// '=' at an identifier boundary. The key need not be credential-bearing. Both
// the raw value and its canonical decoded inspection form are checked.
func ContainsAssignment(value string) bool {
	decoded, _, _ := CanonicalDecode(value)
	return containsField(value, false, true) ||
		decoded != value && containsField(decoded, false, true)
}

func containsField(value string, credentialOnly bool, assignmentOnly bool) bool {
	_, ok := nextField(value, 0, credentialOnly, assignmentOnly)
	return ok
}

// maxCanonicalDecodeRounds bounds fixed-point decoding; see CanonicalDecode.
const maxCanonicalDecodeRounds = 4

// CanonicalDecode returns value's bounded fixed-point percent-decoded
// inspection form, a composed map from each decoded byte to the start of its
// raw span (the final entry is len(value); nil is the identity map), and a
// stability flag. One escape always contributes exactly one decoded
// byte, so decoded indices map back to contiguous raw spans. Decoding runs
// as a bounded fixed point: each round that decodes anything strictly
// shrinks the value, and a shared round budget keeps any single input from
// amplifying decode work or hiding a credential behind unbounded encoding
// depth. Malformed escapes pass through literally, so one bad escape cannot
// blind inspection of the rest. The boolean reports whether decoding reached
// a form without unresolved escapes within the budget; admission and
// disclosure decisions must fail closed when it is false.
func CanonicalDecode(value string) (string, []int32, bool) {
	decoded := value
	var composed []int32
	var literals []bool
	for round := 0; round < maxCanonicalDecodeRounds; round++ {
		if !strings.Contains(decoded, "%") {
			break
		}
		next, offsets, nextLiterals, malformed := canonicalDecodeOnce(decoded, literals)
		if malformed {
			return decoded, composed, false
		}
		if next == decoded {
			break
		}
		composed = composeCanonicalOffsets(composed, offsets)
		literals = nextLiterals
		decoded = next
	}
	// A percent sign that does not start a valid escape is literal content
	// once it was produced by a %25 decode; only a valid escape the decode
	// budget could not consume keeps the form unstable.
	return decoded, composed, !containsPercentEscape(decoded)
}

// containsPercentEscape reports whether value still contains a valid
// percent-encoded triplet, meaning decodable structure survived the bounded
// fixed-point budget.
func containsPercentEscape(value string) bool {
	for index := 0; index+2 < len(value); index++ {
		if value[index] == '%' && isHexByte(value[index+1]) && isHexByte(value[index+2]) {
			return true
		}
	}
	return false
}

func isHexByte(value byte) bool {
	return value >= '0' && value <= '9' ||
		value >= 'a' && value <= 'f' ||
		value >= 'A' && value <= 'F'
}

// canonicalDecodeOnce decodes one layer of percent-escapes. A percent sign
// that does not start a valid escape is literal when the provenance mask
// marks it as produced by a %25 decode in any earlier round; otherwise it is
// a malformed or incomplete raw escape and the form is reported malformed.
// The returned mask carries the same provenance for the decoded text, so the
// classification survives every later round. It returns the input unchanged
// with a nil map when nothing decodes, so callers can detect that no
// progress is possible.
func canonicalDecodeOnce(value string, originLiterals []bool) (string, []int32, []bool, bool) {
	decoded := make([]byte, 0, len(value))
	offsets := make([]int32, 0, len(value)+1)
	literals := make([]bool, 0, len(value))
	changed := false
	malformed := false
	for index := 0; index < len(value); {
		if value[index] == '%' && index+2 < len(value) {
			if unescaped, err := url.PathUnescape(value[index : index+3]); err == nil {
				offsets = append(offsets, int32(index))
				decoded = append(decoded, unescaped[0])
				literals = append(literals, unescaped[0] == '%')
				index += 3
				changed = true
				continue
			}
		}
		literal := originLiterals != nil && index < len(originLiterals) && originLiterals[index]
		if value[index] == '%' && !literal {
			malformed = true
		}
		offsets = append(offsets, int32(index))
		decoded = append(decoded, value[index])
		literals = append(literals, literal)
		index++
	}
	if !changed {
		return value, nil, nil, malformed
	}
	offsets = append(offsets, int32(len(value)))
	return string(decoded), offsets, literals, malformed
}

// composeCanonicalOffsets chains one decode round's offsets onto the
// previously composed map, so every final decoded byte maps back to the
// start of its raw span through every round.
func composeCanonicalOffsets(previous []int32, next []int32) []int32 {
	if previous == nil {
		return next
	}
	composed := make([]int32, len(next))
	for index, offset := range next {
		composed[index] = previous[offset]
	}
	return composed
}

// Redact replaces credential-bearing values, credential-bearing URL userinfo,
// and explicit secret values while preserving surrounding text. Matches found
// in the bounded fixed-point decoded inspection form are mapped back to their
// raw spans, so encoded credentials are redacted without decoding any
// non-secret bytes in the output. All spans are merged canonically before
// rendering, so an overlapping longer match cannot re-emit bytes an earlier
// span already covered.
func Redact(value string, marker string, explicitSecrets []string) (string, bool) {
	return RedactWithSpans(value, marker, explicitSecrets, nil)
}

// RedactWithSpans redacts credential, userinfo, and explicit-secret spans
// together with caller-supplied extra spans. Every detector observes the
// original value (and its decoded inspection form); the merged coverage is
// rendered once. Sequential rewriting of already-redacted text is not a
// valid composition.
func RedactWithSpans(value string, marker string, explicitSecrets []string, extra []Span) (string, bool) {
	decoded, offsets, stable := CanonicalDecode(value)
	if !stable || !utf8.ValidString(decoded) {
		// Unresolved escapes leave the value uninspectable: a delimiter or
		// explicit secret may still hide behind the remaining layers. Public
		// output fails closed and withholds the whole value.
		return marker, true
	}
	spans := credentialSpans(value)
	spans = append(spans, bearerCredentialSpans(value)...)
	spans = append(spans, userInfoCredentialSpans(value)...)
	for _, secret := range explicitSecrets {
		spans = append(spans, substringSpans(value, secret)...)
	}
	if offsets != nil && decoded != value {
		for _, span := range credentialSpans(decoded) {
			spans = append(spans, mapCanonicalSpan(span, offsets))
		}
		for _, span := range bearerCredentialSpans(decoded) {
			spans = append(spans, mapCanonicalSpan(span, offsets))
		}
		for _, span := range userInfoCredentialSpans(decoded) {
			spans = append(spans, mapCanonicalSpan(span, offsets))
		}
		for _, secret := range explicitSecrets {
			for _, span := range substringSpans(decoded, secret) {
				spans = append(spans, mapCanonicalSpan(span, offsets))
			}
		}
	}
	for _, span := range extra {
		if span.Start < 0 || span.End > len(value) || span.Start >= span.End {
			continue
		}
		spans = append(spans, textSpan{start: span.Start, end: span.End, marker: span.Marker})
	}
	if len(spans) == 0 {
		return value, false
	}
	var result strings.Builder
	result.Grow(len(value))
	cursor := 0
	for _, span := range mergeTextSpans(spans) {
		result.WriteString(value[cursor:span.start])
		spanMarker := span.marker
		if spanMarker == "" {
			spanMarker = marker
		}
		result.WriteString(spanMarker)
		cursor = span.end
	}
	result.WriteString(value[cursor:])
	return result.String(), true
}

func mapCanonicalSpan(span textSpan, offsets []int32) textSpan {
	return textSpan{start: int(offsets[span.start]), end: int(offsets[span.end])}
}

// mergeTextSpans returns the canonical union of the spans: sorted by
// position, with overlapping and adjacent spans collapsed.
func mergeTextSpans(spans []textSpan) []textSpan {
	sort.Slice(spans, func(left, right int) bool {
		if spans[left].start != spans[right].start {
			return spans[left].start < spans[right].start
		}
		return spans[left].end > spans[right].end
	})
	merged := spans[:0]
	for _, span := range spans {
		if len(merged) > 0 && span.start <= merged[len(merged)-1].end {
			if span.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = span.end
			}
			if merged[len(merged)-1].marker == "" {
				merged[len(merged)-1].marker = span.marker
			}
			continue
		}
		merged = append(merged, span)
	}
	return merged
}

// substringSpans returns every occurrence of needle, advancing one byte so
// overlapping occurrences all contribute coverage.
func substringSpans(value string, needle string) []textSpan {
	if needle == "" {
		return nil
	}
	var spans []textSpan
	searchStart := 0
	for searchStart < len(value) {
		index := strings.Index(value[searchStart:], needle)
		if index < 0 {
			return spans
		}
		start := searchStart + index
		spans = append(spans, textSpan{start: start, end: start + len(needle)})
		searchStart = start + 1
	}
	return spans
}

type textSpan struct {
	start  int
	end    int
	marker string
}

func credentialSpans(value string) []textSpan {
	var spans []textSpan
	searchStart := 0
	for searchStart < len(value) {
		match, ok := nextField(value, searchStart, true, false)
		if !ok {
			break
		}
		spans = append(spans, textSpan{start: match.valueStart, end: match.valueEnd})
		searchStart = match.valueEnd
	}
	return spans
}

// maxCredentialKeyBytes bounds the key run that bounded key grammar
// classifies. Longer runs cannot be inspected, so assignments keyed by them
// fail closed as credential-bearing instead of being skipped.
const maxCredentialKeyBytes = 128

// nextField returns the next assignment field at or after start. The scan is
// delimiter-anchored: every ':' or '=' looks back over its key run, so a key
// is classified whenever it can be inspected and fails closed when it
// cannot. Supported runs are ASCII identifier bytes within
// maxCredentialKeyBytes. ASCII C0 and DEL are not identifier boundaries;
// overlong runs and runs carrying non-ASCII bytes are treated as
// credential-bearing rather than silently skipped, because an uninspected
// key can conceal any credential grammar.
func nextField(value string, start int, credentialOnly bool, assignmentOnly bool) (field, bool) {
	for index := max(0, start); index < len(value); index++ {
		delimiter := value[index]
		if delimiter != ':' && delimiter != '=' {
			continue
		}
		if assignmentOnly && delimiter != '=' {
			continue
		}
		run, present := keyRunBefore(value, index)
		if !present {
			continue
		}
		var span valueSpan
		var credential bool
		if run.overlong || !run.supported {
			// An uninspectable key gives no trustworthy value grammar, so
			// the value uses the line-scoped extent instead of a token
			// boundary that would leak a multi-token secret.
			span, credential = valueLine, true
		} else {
			span, credential = classifyCredentialKey(run.suffix)
			if !credential && run.controlSplit && run.joined != run.suffix {
				if _, joinedCredential := classifyCredentialKey(run.joined); joinedCredential {
					// C0 inside an identifier is not a field boundary. The
					// joined spelling is credential-bearing, but the split
					// form has no trustworthy token grammar.
					span, credential = valueLine, true
				}
			}
		}
		if credentialOnly && !credential {
			continue
		}
		index++
		for index < len(value) && isASCIISpace(value[index]) {
			index++
		}
		return field{valueStart: index, valueEnd: credentialValueEnd(value, index, span)}, true
	}
	return field{}, false
}

type assignmentKeyRun struct {
	suffix       string
	joined       string
	overlong     bool
	supported    bool
	controlSplit bool
}

// keyRunBefore walks back from a delimiter over optional spaces and one
// optional quote to the assignment key. ASCII identifier bytes are supported
// grammar; non-ASCII bytes extend the run but mark it unsupported; ASCII C0
// and DEL are skipped as non-boundaries so percent-decoded controls cannot
// split token into a non-credential observation. Overlong runs and
// unsupported runs fail closed at the caller.
func keyRunBefore(value string, delimiterIndex int) (assignmentKeyRun, bool) {
	index := delimiterIndex
	for index > 0 && isASCIISpace(value[index-1]) {
		index--
	}
	if index > 0 && (value[index-1] == '\'' || value[index-1] == '"') {
		index--
	}
	end := index
	run := assignmentKeyRun{supported: true}
	runLength := 0
	suffixStart := end
	suffixFrozen := false
	joined := make([]byte, 0, 32)
	for index > 0 {
		previous := value[index-1]
		if isIdentifierByte(previous) || previous >= 0x80 {
			if previous >= 0x80 {
				run.supported = false
			}
			index--
			runLength++
			joined = append(joined, previous)
			if !suffixFrozen {
				suffixStart = index
			}
			if runLength > maxCredentialKeyBytes {
				run.overlong = true
				run.supported = false
				return run, true
			}
			continue
		}
		if isASCIIControl(previous) {
			run.controlSplit = true
			suffixFrozen = true
			index--
			continue
		}
		break
	}
	if end <= index {
		return assignmentKeyRun{}, false
	}
	reverseBytes(joined)
	run.joined = string(joined)
	if run.controlSplit {
		run.suffix = value[suffixStart:end]
	} else {
		run.suffix = run.joined
	}
	return run, true
}

func reverseBytes(value []byte) {
	for left, right := 0, len(value)-1; left < right; left, right = left+1, right-1 {
		value[left], value[right] = value[right], value[left]
	}
}

func isASCIIControl(value byte) bool {
	return value < 0x20 || value == 0x7f
}

func classifyCredentialKey(value string) (valueSpan, bool) {
	words, count := credentialKeyWords(value)
	if count == 0 {
		return valueToken, false
	}
	last := words[count-1]
	if last.equal(value, "authorization") {
		return valueLine, true
	}
	if last.equalAny(
		value,
		"token", "secret", "password", "passwd", "passphrase", "auth",
		"credential", "credentials", "apikey", "accesskey", "accesskeyid",
		"privatekey", "secretkey",
	) {
		return valueToken, true
	}
	if last.equal(value, "key") && count >= 2 {
		previous := words[count-2]
		if previous.equalAny(value, "api", "access", "private", "secret") {
			return valueToken, true
		}
	}
	if last.equal(value, "id") && count >= 3 {
		if words[count-2].equal(value, "key") && words[count-3].equal(value, "access") {
			return valueToken, true
		}
	}
	if last.equal(value, "code") && count >= 2 {
		if words[count-2].equalAny(value, "auth", "authorization", "oauth") {
			return valueToken, true
		}
	}
	return valueToken, false
}

type keyWord struct {
	start int
	end   int
}

func (word keyWord) equal(value string, candidate string) bool {
	if word.end-word.start != len(candidate) {
		return false
	}
	for index := range candidate {
		character := value[word.start+index]
		if isASCIIUpper(character) {
			character += 'a' - 'A'
		}
		if character != candidate[index] {
			return false
		}
	}
	return true
}

func (word keyWord) equalAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if word.equal(value, candidate) {
			return true
		}
	}
	return false
}

func credentialKeyWords(value string) ([3]keyWord, int) {
	var words [3]keyWord
	count := 0
	start := 0
	appendWord := func(end int) {
		if end <= start {
			return
		}
		word := keyWord{start: start, end: end}
		if count < len(words) {
			words[count] = word
			count++
			return
		}
		words[0], words[1], words[2] = words[1], words[2], word
	}
	for index := 0; index < len(value); index++ {
		current := value[index]
		if current == '_' || current == '-' {
			appendWord(index)
			start = index + 1
			continue
		}
		if index == start || !isASCIIUpper(current) {
			continue
		}
		previous := value[index-1]
		nextIsLower := index+1 < len(value) && isASCIILower(value[index+1])
		if isASCIILower(previous) || isASCIIDigit(previous) || isASCIIUpper(previous) && nextIsLower {
			appendWord(index)
			start = index
		}
	}
	appendWord(len(value))
	return words, min(count, len(words))
}

func credentialValueEnd(value string, start int, span valueSpan) int {
	if start >= len(value) {
		return len(value)
	}
	if span == valueLine || hasHTTPAuthSchemePrefix(value, start) {
		return lineScopedValueEnd(value, start)
	}
	if quoted, end := quotedValueEnd(value, start); quoted {
		return end
	}
	for index := start; index < len(value); index++ {
		switch value[index] {
		case ' ', '\t', '\r', '\n', ',', ';':
			return index
		}
	}
	return len(value)
}

// lineScopedValueEnd withholds a line-scoped credential value. The rest of
// the opening line is the minimum extent. The scan is a closure: every
// unescaped quote that opens inside the current extent extends it to the
// closing quote and the remainder of that closing line, so a later quoted
// continuation cannot outrun the first pair. An unclosed quote withholds
// the rest of the text.
func lineScopedValueEnd(value string, start int) int {
	end := lineValueEnd(value, start)
	inQuote := false
	var quote byte
	for index := start; index < len(value); index++ {
		if !inQuote && index >= end {
			break
		}
		current := value[index]
		if (current != '"' && current != '\'') || isEscapedQuote(value, index) {
			continue
		}
		if !inQuote {
			inQuote = true
			quote = current
			continue
		}
		if current != quote {
			continue
		}
		inQuote = false
		closeEnd := index + 1
		if closeEnd > end {
			end = closeEnd
			if closeLineEnd := lineValueEnd(value, closeEnd); closeLineEnd > end {
				end = closeLineEnd
			}
		}
	}
	if inQuote {
		return len(value)
	}
	return end
}

func closingQuotedEnd(value string, openAt int, quote byte) (bool, int) {
	for index := openAt + 1; index < len(value); index++ {
		if value[index] == quote && !isEscapedQuote(value, index) {
			return true, index + 1
		}
	}
	return false, len(value)
}

var httpAuthSchemes = []string{
	"bearer", "basic", "digest", "negotiate", "ntlm", "oauth", "dpop", "hoba", "mutual",
}

func hasHTTPAuthSchemePrefix(value string, start int) bool {
	if start >= len(value) {
		return false
	}
	for _, scheme := range httpAuthSchemes {
		if matched, _, _ := logicalAuthSchemePrefix(value, start, scheme); matched {
			return true
		}
	}
	return false
}

// bearerCredentialSpans treats one logical Bearer value as line-scoped. The
// marker and value may contain Unicode format controls or replacement runes
// introduced by an invalid UTF-8 capture; neither can split the redaction span
// into a visible suffix.
func bearerCredentialSpans(value string) []textSpan {
	var spans []textSpan
	for start := 0; start < len(value); start++ {
		if !asciiFoldEqual(value[start], 'b') ||
			(start > 0 && isIdentifierByte(value[start-1])) {
			continue
		}
		matched, uninspectable, valueStart := logicalAuthSchemePrefix(value, start, "bearer")
		if !matched || valueStart >= len(value) {
			continue
		}
		spanStart := valueStart
		if uninspectable {
			spanStart = start
		}
		span := textSpan{
			start: spanStart,
			end:   lineScopedValueEnd(value, valueStart),
		}
		spans = append(spans, span)
		start = span.end - 1
	}
	return spans
}

// logicalAuthSchemePrefix recognizes an ASCII auth scheme while treating Cf
// as a logical separator and U+FFFD as uninspectable capture evidence. During
// scheme letter matching, TAB/LF/CR are C0 gaps like other controls so a
// split marker such as Bea\trer still matches. After the scheme is complete,
// those same bytes are value separators, not uninspectable gaps.
func logicalAuthSchemePrefix(value string, start int, scheme string) (matched bool, uninspectable bool, valueStart int) {
	cursor := start
	for _, letter := range []byte(scheme) {
		for cursor < len(value) {
			character, width := utf8.DecodeRuneInString(value[cursor:])
			skip, invalid := logicalAuthGap(character, width, true)
			if !skip {
				break
			}
			uninspectable = uninspectable || invalid
			cursor += width
		}
		if cursor >= len(value) || !asciiFoldEqual(value[cursor], letter) {
			return false, uninspectable, start
		}
		cursor++
	}

	separator := false
	for cursor < len(value) {
		character, width := utf8.DecodeRuneInString(value[cursor:])
		skip, invalid := logicalAuthGap(character, width, false)
		switch {
		case skip:
			uninspectable = uninspectable || invalid
			separator = true
			cursor += width
		case isASCIISpace(value[cursor]):
			separator = true
			cursor++
		default:
			if value[cursor] == '"' || value[cursor] == '\'' {
				separator = true
			}
			if !separator {
				return false, uninspectable, start
			}
			return true, uninspectable, cursor
		}
	}
	return separator, uninspectable, cursor
}

func logicalAuthGap(value rune, width int, whitespaceIsGap bool) (skip bool, invalid bool) {
	if value == '\t' || value == '\n' || value == '\r' {
		if whitespaceIsGap {
			return true, true
		}
		return false, false
	}
	if value < 0x20 || value == 0x7f {
		return true, true
	}
	if unicode.In(value, unicode.Cf) {
		return true, false
	}
	if value == utf8.RuneError && (width == 1 || width == 3) {
		return true, true
	}
	return false, false
}

func asciiFoldEqual(value byte, lower byte) bool {
	return value == lower || value >= 'A' && value <= 'Z' && value+('a'-'A') == lower
}

// quotedValueEnd reports the span of a quoted token value starting at start.
// An opening quote that never closes is still a quoted value: the span runs
// to the end of the text instead of falling back to a space-delimited token.
func quotedValueEnd(value string, start int) (bool, int) {
	if value[start] != '"' && value[start] != '\'' {
		return false, 0
	}
	_, end := closingQuotedEnd(value, start, value[start])
	return true, end
}

func lineValueEnd(value string, start int) int {
	if end := strings.IndexAny(value[start:], "\r\n"); end >= 0 {
		return start + end
	}
	return len(value)
}

func isEscapedQuote(value string, index int) bool {
	backslashes := 0
	for index--; index >= 0 && value[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 != 0
}

// isKeyStart reports whether the letter at index begins a key. Key
// continuation admits every identifier byte, but a key start requires a
// non-word boundary: a run of '-' option-prefix dashes before the letter
// does not continue a preceding word, so option-style keys such as
// "--token=..." are recognized.
func isIdentifierByte(value byte) bool {
	return isASCIILetter(value) || isASCIIDigit(value) || value == '_' || value == '-'
}

func isASCIILetter(value byte) bool { return isASCIIUpper(value) || isASCIILower(value) }
func isASCIIUpper(value byte) bool  { return value >= 'A' && value <= 'Z' }
func isASCIILower(value byte) bool  { return value >= 'a' && value <= 'z' }
func isASCIIDigit(value byte) bool  { return value >= '0' && value <= '9' }

func isASCIISpace(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}
