package topology

import (
	"cmp"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SubjectKind identifies one closed structural subject role.
type SubjectKind string

const (
	SubjectResource            SubjectKind = "resource"
	SubjectProjection          SubjectKind = "projection"
	SubjectHostRelation        SubjectKind = "host_relation"
	SubjectBinding             SubjectKind = "binding"
	SubjectCarrier             SubjectKind = "carrier"
	SubjectContribution        SubjectKind = "contribution"
	SubjectProvisionedArtifact SubjectKind = "provisioned-artifact"
	SubjectRuntimeDependency   SubjectKind = "runtime-dependency"
	SubjectCredentialReference SubjectKind = "credential-reference"
)

var subjectNamespacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// ParseSubjectKind validates a structural subject kind.
func ParseSubjectKind(value string) (SubjectKind, error) {
	switch SubjectKind(value) {
	case SubjectResource,
		SubjectProjection,
		SubjectHostRelation,
		SubjectBinding,
		SubjectCarrier,
		SubjectContribution,
		SubjectProvisionedArtifact,
		SubjectRuntimeDependency,
		SubjectCredentialReference:
		return SubjectKind(value), nil
	default:
		return "", fmt.Errorf("unknown topology subject kind %q", value)
	}
}

// SubjectID identifies one lowered managed subject independently of lock,
// observation, route, or effect occurrence.
type SubjectID struct {
	kind      SubjectKind
	namespace string
	key       string
}

// NewSubjectID constructs a canonical subject identity. Namespace names the
// semantic collision domain; key identifies one subject within that domain.
func NewSubjectID(kind SubjectKind, namespace string, key string) (SubjectID, error) {
	parsedKind, err := ParseSubjectKind(string(kind))
	if err != nil {
		return SubjectID{}, err
	}
	if !subjectNamespacePattern.MatchString(namespace) {
		return SubjectID{}, fmt.Errorf("topology subject namespace %q must be a stable token", namespace)
	}
	if err := validateSubjectKey(key); err != nil {
		return SubjectID{}, err
	}
	return SubjectID{kind: parsedKind, namespace: namespace, key: key}, nil
}

// ParseSubjectID decodes the canonical kind/namespace/key representation.
func ParseSubjectID(value string) (SubjectID, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 3 {
		return SubjectID{}, fmt.Errorf("topology subject id must use kind/namespace/key form")
	}
	namespace, err := url.PathUnescape(parts[1])
	if err != nil {
		return SubjectID{}, fmt.Errorf("decode topology subject namespace: %w", err)
	}
	key, err := url.PathUnescape(parts[2])
	if err != nil {
		return SubjectID{}, fmt.Errorf("decode topology subject key: %w", err)
	}
	id, err := NewSubjectID(SubjectKind(parts[0]), namespace, key)
	if err != nil {
		return SubjectID{}, err
	}
	if id.String() != value {
		return SubjectID{}, fmt.Errorf("topology subject id %q is not canonical", value)
	}
	return id, nil
}

// Kind returns the subject's closed structural role.
func (id SubjectID) Kind() SubjectKind { return id.kind }

// Namespace returns the subject's semantic collision domain.
func (id SubjectID) Namespace() string { return id.namespace }

// Key returns the subject identity local to Namespace.
func (id SubjectID) Key() string { return id.key }

// IsZero reports whether no subject identity is present.
func (id SubjectID) IsZero() bool { return id == (SubjectID{}) }

// Validate rejects forged or partially initialized subject identities.
func (id SubjectID) Validate() error {
	_, err := NewSubjectID(id.kind, id.namespace, id.key)
	return err
}

// String returns the canonical round-trippable kind/namespace/key identity.
func (id SubjectID) String() string {
	if id.IsZero() {
		return ""
	}
	return string(id.kind) + "/" + url.PathEscape(id.namespace) + "/" + url.PathEscape(id.key)
}

// CompareSubjectID orders identities by kind, namespace, and unescaped key.
func CompareSubjectID(left SubjectID, right SubjectID) int {
	if order := cmp.Compare(left.kind, right.kind); order != 0 {
		return order
	}
	if order := cmp.Compare(left.namespace, right.namespace); order != 0 {
		return order
	}
	return cmp.Compare(left.key, right.key)
}

func validateSubjectKey(key string) error {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(key) != key {
		return fmt.Errorf("topology subject key must be non-empty and trimmed")
	}
	if !utf8.ValidString(key) {
		return fmt.Errorf("topology subject key must be valid UTF-8")
	}
	if strings.IndexFunc(key, func(value rune) bool {
		return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
	}) >= 0 {
		return fmt.Errorf("topology subject key must not contain control or bidirectional formatting characters")
	}
	return nil
}
