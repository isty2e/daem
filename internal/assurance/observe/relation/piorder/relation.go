package piorder

import (
	"fmt"

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
	return ScopedRelation{key: key, scope: scope, sourceContext: context}, nil
}

func (relation ScopedRelation) CorrelationKey() observerelation.CorrelationKey { return relation.key }

func (relation ScopedRelation) Scope() target.Scope { return relation.scope }

func (relation ScopedRelation) SourceContext() extensiontopology.LocalSourceContext {
	return relation.sourceContext
}
