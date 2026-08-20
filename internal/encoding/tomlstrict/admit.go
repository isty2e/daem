// Package tomlstrict admits untrusted TOML structure before BurntSushi decode.
package tomlstrict

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	// MaximumDepth bounds container nesting and the semantic key path BurntSushi
	// walks from the document root, including implicit dotted-key prefixes.
	MaximumDepth = 64
	// MaximumContainers bounds tables, inline tables, and arrays. It stays
	// above the Codex plugin observation entry budget so key overflow remains
	// an observation-budget fact after a successful decode.
	MaximumContainers = 16384
	// MaximumWork bounds key parts, primitives, container enters, and prefix
	// materialization hops.
	MaximumWork = 65536
	// MaximumKeyBytes bounds one assignment or header key's raw token bytes.
	MaximumKeyBytes = 4096
)

var (
	// ErrMaximumDepthExceeded classifies nesting beyond the caller budget.
	ErrMaximumDepthExceeded = errors.New("TOML nesting depth exceeded")
	// ErrMaximumContainersExceeded classifies too many tables or arrays.
	ErrMaximumContainersExceeded = errors.New("TOML container count exceeded")
	// ErrMaximumWorkExceeded classifies aggregate TOML structure work.
	ErrMaximumWorkExceeded = errors.New("TOML aggregate work exceeded")
	// ErrMalformed classifies structural TOML that cannot be admitted.
	ErrMalformed = errors.New("malformed TOML structure")
)

// Limits are positive structure ceilings applied before toml.Unmarshal.
type Limits struct {
	MaximumDepth      int
	MaximumContainers int
	MaximumWork       int
	MaximumKeyBytes   int
}

// StandardLimits are the shared host-document ceilings.
func StandardLimits() Limits {
	return Limits{
		MaximumDepth:      MaximumDepth,
		MaximumContainers: MaximumContainers,
		MaximumWork:       MaximumWork,
		MaximumKeyBytes:   MaximumKeyBytes,
	}
}

const cancelCheckInterval = 4096

// Admit checks TOML nesting, container count, and aggregate work without
// building a decoded document. Callers must still parse admitted bytes.
func Admit(ctx context.Context, content []byte, limits Limits) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if limits.MaximumDepth <= 0 ||
		limits.MaximumContainers <= 0 ||
		limits.MaximumWork <= 0 ||
		limits.MaximumKeyBytes <= 0 {
		return fmt.Errorf("TOML structure limits must be positive")
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("%w: document is not valid UTF-8", ErrMalformed)
	}

	scanner := scanner{
		src:    content,
		ctx:    ctx,
		limits: limits,
	}
	if len(content) >= 3 && content[0] == 0xef && content[1] == 0xbb && content[2] == 0xbf {
		scanner.index = 3
	}
	return scanner.document()
}

type scanner struct {
	src        []byte
	index      int
	depth      int
	pathDepth  int
	containers int
	work       int
	limits     Limits
	ctx        context.Context
	cancelAt   int
}

func (s *scanner) document() error {
	for {
		if err := s.checkCancel(); err != nil {
			return err
		}
		s.skipSpaceAndComments()
		if s.done() {
			return nil
		}
		if s.src[s.index] == '[' {
			if err := s.tableHeader(); err != nil {
				return err
			}
			continue
		}
		if err := s.assignment(); err != nil {
			return err
		}
	}
}

func (s *scanner) tableHeader() error {
	if s.done() || s.src[s.index] != '[' {
		return fmt.Errorf("%w: expected table header", ErrMalformed)
	}
	s.index++
	arrayTable := false
	if !s.done() && s.src[s.index] == '[' {
		arrayTable = true
		s.index++
	}
	parts, keyBytes, err := s.headerKey()
	if err != nil {
		return err
	}
	if parts == 0 {
		return fmt.Errorf("%w: empty table header", ErrMalformed)
	}
	if s.done() || s.src[s.index] != ']' {
		return fmt.Errorf("%w: unclosed table header", ErrMalformed)
	}
	s.index++
	if arrayTable {
		if s.done() || s.src[s.index] != ']' {
			return fmt.Errorf("%w: unclosed array table header", ErrMalformed)
		}
		s.index++
	}
	s.pathDepth = 0
	if err := s.chargeKey(parts, keyBytes); err != nil {
		return err
	}
	s.pathDepth = parts
	return s.chargeContainer()
}

func (s *scanner) headerKey() (int, int, error) {
	parts := 0
	keyBytes := 0
	for {
		s.skipKeyWhitespace()
		if s.done() {
			return 0, 0, fmt.Errorf("%w: truncated table header", ErrMalformed)
		}
		if s.src[s.index] == ']' {
			return parts, keyBytes, nil
		}
		if parts > 0 {
			if s.src[s.index] != '.' {
				return 0, 0, fmt.Errorf("%w: table header key", ErrMalformed)
			}
			s.index++
			s.skipKeyWhitespace()
			if s.done() {
				return 0, 0, fmt.Errorf("%w: truncated table header", ErrMalformed)
			}
		}
		partBytes, err := s.keyPart()
		if err != nil {
			return 0, 0, err
		}
		parts++
		keyBytes += partBytes
	}
}

func (s *scanner) assignment() error {
	parts, keyBytes, err := s.assignmentKey()
	if err != nil {
		return err
	}
	if err := s.chargeKey(parts, keyBytes); err != nil {
		return err
	}
	s.skipSpaceAndComments()
	if s.done() || s.src[s.index] != '=' {
		return fmt.Errorf("%w: expected assignment", ErrMalformed)
	}
	s.index++
	return s.value(parts)
}

func (s *scanner) assignmentKey() (int, int, error) {
	parts := 0
	keyBytes := 0
	for {
		s.skipKeyWhitespace()
		if s.done() {
			return 0, 0, fmt.Errorf("%w: truncated key", ErrMalformed)
		}
		partBytes, err := s.keyPart()
		if err != nil {
			return 0, 0, err
		}
		parts++
		keyBytes += partBytes
		s.skipKeyWhitespace()
		if s.done() {
			return 0, 0, fmt.Errorf("%w: truncated key", ErrMalformed)
		}
		if s.src[s.index] == '.' {
			s.index++
			continue
		}
		return parts, keyBytes, nil
	}
}

func (s *scanner) keyPart() (int, error) {
	if s.done() {
		return 0, fmt.Errorf("%w: truncated key", ErrMalformed)
	}
	start := s.index
	switch s.src[s.index] {
	case '"', '\'':
		if err := s.skipString(false); err != nil {
			return 0, err
		}
		return s.index - start, nil
	default:
		for !s.done() && isBareKeyByte(s.src[s.index]) {
			s.index++
		}
		if s.index == start {
			return 0, fmt.Errorf("%w: expected key", ErrMalformed)
		}
		return s.index - start, nil
	}
}

func (s *scanner) value(keyParts int) error {
	if err := s.checkCancel(); err != nil {
		return err
	}
	s.skipSpaceAndComments()
	if s.done() {
		return fmt.Errorf("%w: truncated value", ErrMalformed)
	}
	switch s.src[s.index] {
	case '"', '\'':
		if err := s.skipString(true); err != nil {
			return err
		}
		return s.addWork(1)
	case '[':
		return s.array(keyParts)
	case '{':
		return s.inlineTable(keyParts)
	default:
		if err := s.skipBareValue(); err != nil {
			return err
		}
		return s.addWork(1)
	}
}

func (s *scanner) array(keyParts int) error {
	if err := s.enterNestedContainer(); err != nil {
		return err
	}
	s.pathDepth += keyParts
	s.index++
	expectValue := true
	for {
		s.skipSpaceAndComments()
		if s.done() {
			return fmt.Errorf("%w: unclosed array", ErrMalformed)
		}
		if s.src[s.index] == ']' {
			s.index++
			s.pathDepth -= keyParts
			s.leaveContainer()
			return nil
		}
		if !expectValue {
			if s.src[s.index] != ',' {
				return fmt.Errorf("%w: array separator", ErrMalformed)
			}
			s.index++
			expectValue = true
			continue
		}
		if err := s.value(0); err != nil {
			return err
		}
		expectValue = false
	}
}

func (s *scanner) inlineTable(keyParts int) error {
	if err := s.enterNestedContainer(); err != nil {
		return err
	}
	s.pathDepth += keyParts
	s.index++
	expectPair := true
	for {
		s.skipSpaceAndComments()
		if s.done() {
			return fmt.Errorf("%w: unclosed inline table", ErrMalformed)
		}
		if s.src[s.index] == '}' {
			s.index++
			s.pathDepth -= keyParts
			s.leaveContainer()
			return nil
		}
		if !expectPair {
			if s.src[s.index] != ',' {
				return fmt.Errorf("%w: inline table separator", ErrMalformed)
			}
			s.index++
			expectPair = true
			continue
		}
		if err := s.assignment(); err != nil {
			return err
		}
		expectPair = false
	}
}

func (s *scanner) skipString(allowMultiline bool) error {
	if s.done() {
		return fmt.Errorf("%w: truncated string", ErrMalformed)
	}
	quote := s.src[s.index]
	if quote != '"' && quote != '\'' {
		return fmt.Errorf("%w: expected string", ErrMalformed)
	}
	multiline := allowMultiline && s.index+2 < len(s.src) && s.src[s.index+1] == quote && s.src[s.index+2] == quote
	s.index++
	if multiline {
		s.index += 2
		if quote == '"' && !s.done() && (s.src[s.index] == '\n' || (s.src[s.index] == '\r' && s.index+1 < len(s.src) && s.src[s.index+1] == '\n')) {
			if s.src[s.index] == '\r' {
				s.index += 2
			} else {
				s.index++
			}
		}
	}
	escaped := false
	for !s.done() {
		char := s.src[s.index]
		if escaped {
			escaped = false
			s.index++
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			s.index++
			continue
		}
		if !multiline && (char == '\n' || char == '\r') {
			return fmt.Errorf("%w: unescaped newline in string", ErrMalformed)
		}
		if char != quote {
			s.index++
			continue
		}
		if !multiline {
			s.index++
			return nil
		}
		run := 1
		for s.index+run < len(s.src) && s.src[s.index+run] == quote {
			run++
		}
		if run < 3 {
			s.index += run
			continue
		}
		if run > 5 {
			return fmt.Errorf("%w: malformed multiline string closer", ErrMalformed)
		}
		s.index += run
		return nil
	}
	return fmt.Errorf("%w: unclosed string", ErrMalformed)
}

func (s *scanner) skipBareValue() error {
	if s.done() || isBareValueDelimiter(s.src[s.index]) {
		return fmt.Errorf("%w: expected value", ErrMalformed)
	}
	start := s.index
	datetime := false
	for !s.done() {
		char := s.src[s.index]
		if char == ' ' && datetime {
			next := s.index + 1
			if next < len(s.src) && s.src[next] >= '0' && s.src[next] <= '9' {
				s.index++
				continue
			}
			break
		}
		if isBareValueDelimiter(char) {
			break
		}
		if s.index > start && (char == '-' || char == ':') {
			datetime = true
		}
		s.index++
	}
	if s.index == start {
		return fmt.Errorf("%w: expected value", ErrMalformed)
	}
	return nil
}

func (s *scanner) chargeKey(parts int, keyBytes int) error {
	if parts <= 0 {
		return fmt.Errorf("%w: expected key", ErrMalformed)
	}
	if parts > s.limits.MaximumDepth || s.pathDepth > s.limits.MaximumDepth-parts {
		return fmt.Errorf("%w: maximum=%d", ErrMaximumDepthExceeded, s.limits.MaximumDepth)
	}
	if keyBytes > s.limits.MaximumKeyBytes {
		return fmt.Errorf("%w: maximum=%d", ErrMaximumWorkExceeded, s.limits.MaximumWork)
	}
	if err := s.addWork(parts); err != nil {
		return err
	}
	walk := parts*s.pathDepth + parts*(parts+1)/2
	return s.addWork(walk)
}

func (s *scanner) enterNestedContainer() error {
	s.depth++
	if s.depth > s.limits.MaximumDepth {
		return fmt.Errorf("%w: maximum=%d", ErrMaximumDepthExceeded, s.limits.MaximumDepth)
	}
	return s.chargeContainer()
}

func (s *scanner) chargeContainer() error {
	s.containers++
	if s.containers > s.limits.MaximumContainers {
		return fmt.Errorf("%w: maximum=%d", ErrMaximumContainersExceeded, s.limits.MaximumContainers)
	}
	return s.addWork(1)
}

func (s *scanner) leaveContainer() {
	s.depth--
}

func (s *scanner) addWork(amount int) error {
	if amount <= 0 {
		return s.checkCancel()
	}
	if s.work > s.limits.MaximumWork-amount {
		return fmt.Errorf("%w: maximum=%d", ErrMaximumWorkExceeded, s.limits.MaximumWork)
	}
	s.work += amount
	return s.checkCancel()
}

func (s *scanner) checkCancel() error {
	if s.index-s.cancelAt < cancelCheckInterval {
		return nil
	}
	s.cancelAt = s.index
	return s.ctx.Err()
}

func (s *scanner) done() bool {
	return s.index >= len(s.src)
}

func (s *scanner) skipSpaceAndComments() {
	for !s.done() {
		switch s.src[s.index] {
		case ' ', '\t', '\n', '\r':
			s.index++
		case '#':
			s.skipLine()
		default:
			return
		}
	}
}

func (s *scanner) skipKeyWhitespace() {
	for !s.done() {
		switch s.src[s.index] {
		case ' ', '\t', '\n', '\r':
			s.index++
		default:
			return
		}
	}
}

func (s *scanner) skipLine() {
	for !s.done() && s.src[s.index] != '\n' {
		s.index++
	}
	if !s.done() {
		s.index++
	}
}

func isBareKeyByte(char byte) bool {
	return char == '_' || char == '-' ||
		(char >= 'A' && char <= 'Z') ||
		(char >= 'a' && char <= 'z') ||
		(char >= '0' && char <= '9')
}

func isBareValueDelimiter(char byte) bool {
	switch char {
	case ' ', '\t', '\n', '\r', '#', ',', ']', '}':
		return true
	default:
		return false
	}
}
