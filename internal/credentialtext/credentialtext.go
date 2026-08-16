// Package credentialtext recognizes credential-bearing key/value text without
// depending on format-specific delimiter lists.
package credentialtext

import "strings"

type valueSpan uint8

const (
	valueToken valueSpan = iota
	valueLine
)

type field struct {
	valueStart int
	valueEnd   int
}

// ContainsCredential reports whether value contains a credential-bearing key
// followed by ':' or '=' at an identifier boundary.
func ContainsCredential(value string) bool {
	_, ok := nextField(value, 0, true, false)
	return ok
}

// ContainsAssignment reports whether value contains an identifier followed by
// '=' at an identifier boundary. The key need not be credential-bearing.
func ContainsAssignment(value string) bool {
	_, ok := nextField(value, 0, false, true)
	return ok
}

// Redact replaces credential-bearing values while preserving surrounding text.
func Redact(value string, marker string) (string, bool) {
	var result strings.Builder
	result.Grow(len(value))
	cursor := 0
	searchStart := 0
	redacted := false
	for searchStart < len(value) {
		match, ok := nextField(value, searchStart, true, false)
		if !ok {
			break
		}
		result.WriteString(value[cursor:match.valueStart])
		result.WriteString(marker)
		cursor = match.valueEnd
		searchStart = cursor
		redacted = true
	}
	if !redacted {
		return value, false
	}
	result.WriteString(value[cursor:])
	return result.String(), true
}

func nextField(value string, start int, credentialOnly bool, assignmentOnly bool) (field, bool) {
	for index := max(0, start); index < len(value); index++ {
		if !isASCIILetter(value[index]) || index > 0 && isIdentifierByte(value[index-1]) {
			continue
		}
		keyStart := index
		for index < len(value) && isIdentifierByte(value[index]) {
			index++
		}
		keyEnd := index
		if keyEnd-keyStart > 128 {
			continue
		}
		if index < len(value) && (value[index] == '\'' || value[index] == '"') {
			index++
		}
		for index < len(value) && isASCIISpace(value[index]) {
			index++
		}
		if index >= len(value) || value[index] != ':' && value[index] != '=' {
			index = keyEnd - 1
			continue
		}
		assignment := value[index] == '='
		if assignmentOnly && !assignment {
			index = keyEnd - 1
			continue
		}
		span, credential := classifyCredentialKey(value[keyStart:keyEnd])
		if credentialOnly && !credential {
			index = keyEnd - 1
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
	if value[start] == '"' || value[start] == '\'' {
		quote := value[start]
		for index := start + 1; index < len(value); index++ {
			if value[index] == quote && !isEscapedQuote(value, index) {
				return index + 1
			}
		}
		return len(value)
	}
	if span == valueLine {
		if end := strings.IndexAny(value[start:], "\r\n"); end >= 0 {
			return start + end
		}
		return len(value)
	}
	for index := start; index < len(value); index++ {
		switch value[index] {
		case ' ', '\t', '\r', '\n', ',', ';':
			return index
		}
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
