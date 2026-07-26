package relation

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// CorrelationKey identifies one exact relation expectation. Subject identity
// alone is insufficient because a replacement may reuse the same subject while
// changing source, scope, or managed-instance identity.
type CorrelationKey struct {
	subject  topology.SubjectID
	expected hostrelation.ExpectedRelation
}

// NewCorrelationKey validates one exact passive-observation identity.
func NewCorrelationKey(
	subject topology.SubjectID,
	expected hostrelation.ExpectedRelation,
) (CorrelationKey, error) {
	if err := subject.Validate(); err != nil {
		return CorrelationKey{}, fmt.Errorf("relation observation subject: %w", err)
	}
	if subject.Kind() != topology.SubjectHostRelation {
		return CorrelationKey{}, fmt.Errorf("relation observation subject must be a host relation")
	}
	if err := expected.Validate(); err != nil {
		return CorrelationKey{}, fmt.Errorf("relation observation expected relation: %w", err)
	}
	return CorrelationKey{
		subject:  subject,
		expected: expected,
	}, nil
}

// Validate rejects zero or forged passive-observation identities.
func (key CorrelationKey) Validate() error {
	canonical, err := NewCorrelationKey(key.subject, key.expected)
	if err != nil {
		return err
	}
	if key != canonical {
		return fmt.Errorf("relation observation correlation key is not canonical")
	}
	return nil
}

// Subject returns the exact daem relation subject.
func (key CorrelationKey) Subject() topology.SubjectID { return key.subject }

// ExpectedRelation returns the selected host-visible structural correlation identity.
func (key CorrelationKey) ExpectedRelation() hostrelation.ExpectedRelation {
	return key.expected
}

// Correlation binds one exact relation expectation to current evidence. The
// evidence grants no mutation or ownership authority by itself.
type Correlation struct {
	Key    CorrelationKey
	Result CorrelationResult
}

// AuthorityPath is one host inventory path consumed while producing a Batch.
type AuthorityPath struct {
	path   string
	target target.Target
	scope  target.Scope
}

// NewAuthorityPath validates one passive host inventory authority path.
func NewAuthorityPath(path string, selectedTarget target.Target, scope target.Scope) (AuthorityPath, error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(path) != path {
		return AuthorityPath{}, fmt.Errorf("relation observation authority path is required and must be trimmed")
	}
	if strings.ContainsRune(path, '\x00') {
		return AuthorityPath{}, fmt.Errorf("relation observation authority path must not contain a NUL byte")
	}
	if !filepath.IsAbs(path) {
		return AuthorityPath{}, fmt.Errorf("relation observation authority path must be absolute")
	}
	cleanPath := filepath.Clean(path)
	if cleanPath != path {
		return AuthorityPath{}, fmt.Errorf("relation observation authority path must be clean")
	}
	parsedTarget, err := target.ParseTarget(string(selectedTarget))
	if err != nil {
		return AuthorityPath{}, fmt.Errorf("relation observation authority target: %w", err)
	}
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return AuthorityPath{}, fmt.Errorf("relation observation authority scope: %w", err)
	}
	return AuthorityPath{path: cleanPath, target: parsedTarget, scope: parsedScope}, nil
}

// Path returns the observed host inventory path.
func (path AuthorityPath) Path() string { return path.path }

// Target returns the host target owning the observed inventory.
func (path AuthorityPath) Target() target.Target { return path.target }

// Scope returns the host scope of the observed inventory.
func (path AuthorityPath) Scope() target.Scope { return path.scope }

// BatchSpec contains current carrier correlations and their read authority.
type BatchSpec struct {
	Correlations   []Correlation
	AuthorityPaths []AuthorityPath
}

// Batch is an immutable current-observation batch keyed by exact relation
// expectation.
type Batch struct {
	correlations   map[CorrelationKey]CorrelationResult
	authorityPaths []AuthorityPath
}

// NewBatch validates and constructs one current carrier observation batch.
func NewBatch(spec BatchSpec) (Batch, error) {
	correlations := make(map[CorrelationKey]CorrelationResult, len(spec.Correlations))
	for index, entry := range spec.Correlations {
		if err := entry.Key.Validate(); err != nil {
			return Batch{}, fmt.Errorf("relation observation correlations[%d].key: %w", index, err)
		}
		if entry.Result.EvidenceAvailability() == "" || entry.Result.EvidenceFreshness() == "" {
			return Batch{}, fmt.Errorf("relation observation correlations[%d].result is invalid", index)
		}
		if _, exists := correlations[entry.Key]; exists {
			subject := entry.Key.Subject()
			return Batch{}, fmt.Errorf(
				"relation observation expectation %s/%s/%s managed key %q appears more than once",
				subject.Kind(),
				subject.Namespace(),
				subject.Key(),
				entry.Key.ExpectedRelation().ManagedInstanceKey(),
			)
		}
		correlations[entry.Key] = entry.Result
	}

	authorityPaths := make([]AuthorityPath, 0, len(spec.AuthorityPaths))
	seenAuthorityPaths := make(map[AuthorityPath]struct{}, len(spec.AuthorityPaths))
	for index, path := range spec.AuthorityPaths {
		normalized, err := NewAuthorityPath(path.Path(), path.Target(), path.Scope())
		if err != nil {
			return Batch{}, fmt.Errorf("relation observation authority_paths[%d]: %w", index, err)
		}
		if _, exists := seenAuthorityPaths[normalized]; exists {
			continue
		}
		seenAuthorityPaths[normalized] = struct{}{}
		authorityPaths = append(authorityPaths, normalized)
	}

	return Batch{correlations: correlations, authorityPaths: authorityPaths}, nil
}

// Correlation returns current evidence for one exact relation expectation.
func (batch Batch) Correlation(key CorrelationKey) (CorrelationResult, bool) {
	result, ok := batch.correlations[key]
	return result, ok
}

// AuthorityPaths returns a defensive copy of passive host inventory paths
// consumed by this batch.
func (batch Batch) AuthorityPaths() []AuthorityPath {
	return append([]AuthorityPath(nil), batch.authorityPaths...)
}

// Clone returns an immutable copy suitable for a separate planning pass.
func (batch Batch) Clone() Batch {
	correlations := make(map[CorrelationKey]CorrelationResult, len(batch.correlations))
	maps.Copy(correlations, batch.correlations)
	return Batch{
		correlations:   correlations,
		authorityPaths: batch.AuthorityPaths(),
	}
}
