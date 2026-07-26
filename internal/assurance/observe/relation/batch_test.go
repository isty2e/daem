package relation

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestNewBatchRejectsInvalidCorrelationFacts(t *testing.T) {
	hostSubject := mustRelationObservationSubject(t, topology.SubjectHostRelation, "host")
	resourceSubject := mustRelationObservationSubject(t, topology.SubjectResource, "resource")
	validResult := mustUnsupportedCorrelation(t)
	validKey := mustRelationObservationKey(t, hostSubject)
	invalidKindKey := validKey
	invalidKindKey.subject = resourceSubject

	tests := []struct {
		name  string
		entry Correlation
	}{
		{
			name:  "zero subject",
			entry: Correlation{Result: validResult},
		},
		{
			name:  "non-host-relation subject",
			entry: Correlation{Key: invalidKindKey, Result: validResult},
		},
		{
			name:  "zero correlation",
			entry: Correlation{Key: validKey},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewBatch(BatchSpec{Correlations: []Correlation{test.entry}}); err == nil {
				t.Fatal("NewBatch returned nil error")
			}
		})
	}
}

func TestNewBatchRejectsDuplicateLockedSubject(t *testing.T) {
	subject := mustRelationObservationSubject(t, topology.SubjectHostRelation, "duplicate")
	result := mustUnsupportedCorrelation(t)
	key := mustRelationObservationKey(t, subject)

	if _, err := NewBatch(BatchSpec{Correlations: []Correlation{
		{Key: key, Result: result},
		{Key: key, Result: result},
	}}); err == nil {
		t.Fatal("NewBatch returned nil error")
	}
}

func TestBatchDistinguishesReplacementExpectationsWithSameSubject(t *testing.T) {
	subject := mustRelationObservationSubject(t, topology.SubjectHostRelation, "replacement")
	left := mustRelationObservationKey(t, subject)
	rightManaged, err := hostrelation.NewManagedInstanceKey("replacement-managed-key")
	if err != nil {
		t.Fatalf("hostrelation.NewManagedInstanceKey returned error: %v", err)
	}
	rightExpected, err := hostrelation.NewExpectedRelation(
		left.ExpectedRelation().SubjectKey(),
		rightManaged,
	)
	if err != nil {
		t.Fatalf("hostrelation.NewExpectedRelation returned error: %v", err)
	}
	right, err := NewCorrelationKey(subject, rightExpected)
	if err != nil {
		t.Fatalf("NewCorrelationKey returned error: %v", err)
	}
	result := mustUnsupportedCorrelation(t)

	batch, err := NewBatch(BatchSpec{Correlations: []Correlation{
		{Key: left, Result: result},
		{Key: right, Result: result},
	}})
	if err != nil {
		t.Fatalf("NewBatch returned error: %v", err)
	}
	if _, ok := batch.Correlation(left); !ok {
		t.Fatal("left replacement correlation is missing")
	}
	if _, ok := batch.Correlation(right); !ok {
		t.Fatal("right replacement correlation is missing")
	}
}

func TestBatchDeduplicatesAndDefensivelyCopiesAuthorityPaths(t *testing.T) {
	authorityPath, err := NewAuthorityPath("/tmp/host-inventory.json", target.TargetClaudeCode, target.ScopeGlobal)
	if err != nil {
		t.Fatalf("NewAuthorityPath returned error: %v", err)
	}
	batch, err := NewBatch(BatchSpec{AuthorityPaths: []AuthorityPath{authorityPath, authorityPath}})
	if err != nil {
		t.Fatalf("NewBatch returned error: %v", err)
	}

	paths := batch.AuthorityPaths()
	if len(paths) != 1 {
		t.Fatalf("authority path count = %d, want 1", len(paths))
	}
	paths[0] = AuthorityPath{}
	if got := batch.AuthorityPaths()[0].Path(); got != "/tmp/host-inventory.json" {
		t.Fatalf("batch authority path = %q after caller mutation", got)
	}

	clone := batch.Clone()
	clonePaths := clone.AuthorityPaths()
	clonePaths[0] = AuthorityPath{}
	if got := clone.AuthorityPaths()[0].Path(); got != "/tmp/host-inventory.json" {
		t.Fatalf("clone authority path = %q after caller mutation", got)
	}
}

func TestNewAuthorityPathRejectsNonCanonicalPaths(t *testing.T) {
	tests := []string{
		"relative/inventory.json",
		"/tmp/../tmp/inventory.json",
		"/tmp/inventory.json\x00suffix",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if _, err := NewAuthorityPath(path, target.TargetClaudeCode, target.ScopeGlobal); err == nil {
				t.Fatal("NewAuthorityPath returned nil error")
			}
		})
	}
}

func mustRelationObservationSubject(
	t *testing.T,
	kind topology.SubjectKind,
	key string,
) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(kind, "test", key)
	if err != nil {
		t.Fatalf("topology.NewSubjectID returned error: %v", err)
	}
	return subject
}

func mustUnsupportedCorrelation(t *testing.T) CorrelationResult {
	t.Helper()
	subjectKey, err := hostrelation.NewSubjectKey("plugin@example")
	if err != nil {
		t.Fatalf("hostrelation.NewSubjectKey returned error: %v", err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("managed-key")
	if err != nil {
		t.Fatalf("hostrelation.NewManagedInstanceKey returned error: %v", err)
	}
	subject, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatalf("hostrelation.NewExpectedRelation returned error: %v", err)
	}
	return Correlate(subject, UnsupportedInventory())
}

func mustRelationObservationKey(t *testing.T, subject topology.SubjectID) CorrelationKey {
	t.Helper()
	subjectKey, err := hostrelation.NewSubjectKey("plugin@example")
	if err != nil {
		t.Fatalf("hostrelation.NewSubjectKey returned error: %v", err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("managed-key")
	if err != nil {
		t.Fatalf("hostrelation.NewManagedInstanceKey returned error: %v", err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatalf("hostrelation.NewExpectedRelation returned error: %v", err)
	}
	key, err := NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatalf("NewCorrelationKey returned error: %v", err)
	}
	return key
}
