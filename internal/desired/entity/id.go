package entity

import (
	"cmp"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Kind identifies one authored desired family.
type Kind string

const (
	KindSkill        Kind = "skill"
	KindHook         Kind = "hook"
	KindHookAsset    Kind = "hook_asset"
	KindInstructions Kind = "instructions"
	KindMCPServer    Kind = "mcp_server"
	KindExtension    Kind = "extension"
)

// ParseKind validates a desired family kind.
func ParseKind(value string) (Kind, error) {
	switch Kind(value) {
	case KindSkill, KindHook, KindHookAsset, KindInstructions, KindMCPServer, KindExtension:
		return Kind(value), nil
	default:
		return "", fmt.Errorf("unknown desired entity kind %q", value)
	}
}

// ID identifies one authored desired aggregate independently of target
// binding, source resolution, host placement, or runtime occurrence.
type ID struct {
	kind Kind
	name string
}

// New constructs an authored desired entity ID.
func New(kind Kind, name string) (ID, error) {
	parsedKind, err := ParseKind(string(kind))
	if err != nil {
		return ID{}, err
	}
	if strings.TrimSpace(name) == "" {
		return ID{}, fmt.Errorf("desired entity name is required")
	}
	if !utf8.ValidString(name) {
		return ID{}, fmt.Errorf("desired entity name must be valid UTF-8")
	}

	return ID{kind: parsedKind, name: name}, nil
}

// Parse decodes the canonical kind:name representation of an entity ID.
func Parse(value string) (ID, error) {
	kindValue, escapedName, ok := strings.Cut(value, ":")
	if !ok {
		return ID{}, fmt.Errorf("desired entity id must use kind:name form")
	}
	name, err := url.PathUnescape(escapedName)
	if err != nil {
		return ID{}, fmt.Errorf("decode desired entity name: %w", err)
	}
	id, err := New(Kind(kindValue), name)
	if err != nil {
		return ID{}, err
	}
	if id.String() != value {
		return ID{}, fmt.Errorf("desired entity id %q is not canonical", value)
	}

	return id, nil
}

// Kind returns the entity's closed desired family kind.
func (id ID) Kind() Kind {
	return id.kind
}

// Name returns the authored family-local identity.
func (id ID) Name() string {
	return id.name
}

// Validate rejects a zero or malformed desired entity identity.
func (id ID) Validate() error {
	_, err := New(id.kind, id.name)
	return err
}

// String returns the canonical round-trippable kind:name representation.
func (id ID) String() string {
	if id == (ID{}) {
		return ""
	}
	return string(id.kind) + ":" + url.PathEscape(id.name)
}

// Compare orders IDs by family kind and then authored name.
func Compare(left ID, right ID) int {
	if order := cmp.Compare(left.kind, right.kind); order != 0 {
		return order
	}
	return cmp.Compare(left.name, right.name)
}
