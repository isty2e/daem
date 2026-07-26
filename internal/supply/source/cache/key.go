package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Key identifies one source-cache mutation domain with a path-safe component.
type Key struct {
	namespace string
	digest    string
}

// NewKey constructs a deterministic path-safe key from a namespace and opaque materials.
func NewKey(namespace string, materials ...string) (Key, error) {
	if err := validateNamespace(namespace); err != nil {
		return Key{}, err
	}

	hasher := sha256.New()
	writeHashField(hasher, namespace)
	for _, material := range materials {
		writeHashField(hasher, material)
	}

	return Key{
		namespace: namespace,
		digest:    hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// PathComponent returns the directory/file-name-safe representation of the key.
func (key Key) PathComponent() string {
	if key.namespace == "" || key.digest == "" {
		return ""
	}

	return key.namespace + "-" + key.digest
}

func (key Key) String() string {
	return key.PathComponent()
}

func (key Key) validate() error {
	if key.PathComponent() == "" {
		return fmt.Errorf("cache key is required")
	}

	return nil
}

func validateNamespace(namespace string) error {
	if namespace == "" {
		return fmt.Errorf("cache key namespace is required")
	}
	if strings.TrimSpace(namespace) != namespace {
		return fmt.Errorf("cache key namespace %q must not contain surrounding whitespace", namespace)
	}
	for _, char := range namespace {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '-', char == '_', char == '.':
		default:
			return fmt.Errorf("cache key namespace %q contains unsupported character %q", namespace, char)
		}
	}

	return nil
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func writeHashField(writer hashWriter, field string) {
	writer.Write([]byte(strconv.Itoa(len(field))))
	writer.Write([]byte(":"))
	writer.Write([]byte(field))
	writer.Write([]byte("\n"))
}
