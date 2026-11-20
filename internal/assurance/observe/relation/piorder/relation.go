package piorder

import (
	"fmt"
	"path/filepath"
	"strings"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ScopedRelation pairs one exact daem relation expectation with the command
// root from which Pi interprets an operator-provided local source.
type ScopedRelation struct {
	key           observerelation.CorrelationKey
	scope         target.Scope
	sourceContext extensiontopology.LocalSourceContext
}

// NewScopedRelation binds lexical source interpretation without
// selecting roots from the process environment.
func NewScopedRelation(key observerelation.CorrelationKey, scope target.Scope, context extensiontopology.LocalSourceContext) (ScopedRelation, error) {
	if err := context.Validate(); err != nil {
		return ScopedRelation{}, err
	}
	if err := key.Validate(); err != nil {
		return ScopedRelation{}, err
	}
	if scope != target.ScopeProject && scope != target.ScopeGlobal {
		return ScopedRelation{}, fmt.Errorf("invalid Pi relation scope %q", scope)
	}
	if _, err := CleanPackageRoot("Pi package command root", context.BaseRoot()); err != nil {
		return ScopedRelation{}, err
	}
	return ScopedRelation{key: key, scope: scope, sourceContext: context}, nil
}

// CleanPackageRoot validates a lexical Pi package root before it is used for
// observation or bound to a relation in a journal.
func CleanPackageRoot(label, root string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(root) != root {
		return "", fmt.Errorf("%s must be non-empty and trimmed", label)
	}
	if strings.ContainsRune(root, '\x00') {
		return "", fmt.Errorf("%s must not contain a NUL byte", label)
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%s must be absolute", label)
	}
	return filepath.Clean(root), nil
}

func (relation ScopedRelation) CorrelationKey() observerelation.CorrelationKey { return relation.key }

func (relation ScopedRelation) Scope() target.Scope { return relation.scope }

func (relation ScopedRelation) SourceContext() extensiontopology.LocalSourceContext {
	return relation.sourceContext
}
