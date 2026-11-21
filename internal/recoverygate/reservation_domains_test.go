package recoverygate_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestReservationDomainAdmission(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alias")
	if err := os.WriteFile(filepath.Join(root, "target"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", path); err != nil {
		t.Fatal(err)
	}
	entry, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry})
	if err != nil {
		t.Fatal(err)
	}
	referent, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessShared, Effect: mutation.PathEffectReferent})
	if err != nil {
		t.Fatal(err)
	}
	physical, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: mutation.PathEffectReferent, Target: "codex", Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}

	b := operationplan.NewBuilder(operationplan.RevisionsFirstEffect, nil, 0)
	if err := b.AddLogicalPair(path, mutation.AccessExclusive, mutation.AccessShared); err != nil {
		t.Fatal(err)
	}
	b.AddDomains([]mutation.Domain{physical, physical})
	if err := b.AddPhysical(path, mutation.AccessExclusive, mutation.PathEffectReferent, "codex", "project"); err != nil {
		t.Fatal(err)
	}
	steps := b.Compile().DomainSteps()
	want := []mutation.Domain{entry, referent, physical, physical, physical}
	got, err := recoverygate.LowerDomainSteps(steps)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("unrestricted domains = %#v, %v; want %#v", got, err, want)
	}

	got, err = recoverygate.LowerLogicalDomainSteps(steps[:4])
	if err != nil || !reflect.DeepEqual(got, want[:4]) {
		t.Fatalf("logical requests and compiled domains = %#v, %v; want %#v", got, err, want[:4])
	}
	if got, err := recoverygate.LowerLogicalDomainSteps(steps); err == nil || got != nil {
		t.Fatalf("physical request in logical-only list returned domains %v, error %v", got, err)
	}
	if got, err := recoverygate.LowerDomainSteps(append(steps, operationplan.DomainStep{})); err == nil || got != nil {
		t.Fatalf("invalid step after valid requests returned domains %v, error %v", got, err)
	}
}
