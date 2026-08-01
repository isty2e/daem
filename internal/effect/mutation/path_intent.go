package mutation

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func newProvisionalPathIntent(
	candidate canonicalPath,
	namespace canonicalPath,
) (pathauthority.Provisional, error) {
	return pathauthority.NewProvisional(
		candidate.keyPath,
		string(candidate.witness),
		namespace.keyPath,
		string(namespace.witness),
	)
}

func sameProvisionalPathIntent(left pathauthority.Provisional, right pathauthority.Provisional) bool {
	if left.IsZero() || right.IsZero() {
		return left.IsZero() && right.IsZero()
	}
	return left.Equal(right)
}

type namespaceLeaseIntent struct {
	key        string
	witness    pathSemanticsWitness
	accessPath string
}

func newNamespaceLeaseIntent(namespace canonicalPath) (namespaceLeaseIntent, error) {
	intent := namespaceLeaseIntent{
		key:        namespace.keyPath,
		witness:    namespace.witness,
		accessPath: namespace.accessPath,
	}
	if err := intent.validate(); err != nil {
		return namespaceLeaseIntent{}, err
	}
	return intent, nil
}

func (intent namespaceLeaseIntent) validate() error {
	if err := validateCanonicalAuthorityPath("namespace lease key", intent.key); err != nil {
		return err
	}
	if intent.witness == "" {
		return fmt.Errorf("namespace lease semantics witness is required")
	}
	return validateCanonicalAuthorityPath("namespace lease access path", intent.accessPath)
}

func (intent namespaceLeaseIntent) isZero() bool {
	return intent == namespaceLeaseIntent{}
}

func (intent namespaceLeaseIntent) matchesCurrent() (bool, error) {
	identity, err := canonicalPathIdentity(intent.accessPath, PathEffectReferent)
	if err != nil {
		return false, err
	}
	return identity.keyPath == intent.key && identity.witness == intent.witness && identity.provisional.IsZero(), nil
}

func validateCanonicalAuthorityPath(name string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return fmt.Errorf("%s %q must be absolute and clean", name, value)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", name)
	}
	return nil
}
