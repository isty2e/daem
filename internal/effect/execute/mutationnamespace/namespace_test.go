package mutationnamespace

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func namespaceForTest(t *testing.T, project string) Namespace {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	phase, err := physicalwork.NewTraversalPhase(budget)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := New(hostpath.NewResolver(project).Resolve, phase)
	if err != nil {
		t.Fatal(err)
	}
	if project != "" {
		root, err := rootedpath.CaptureRootBounded(project, physicalwork.MaximumPhysicalPathDepth, phase)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = root.Close() })
		if err := namespace.BindProject(root, nil); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = namespace.Close() })
	return namespace
}

func TestGlobalSiblingsShareOneRetainedRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(filepath.Join(home, ".agents", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	namespace := namespaceForTest(t, "")
	const count = 300
	for index := range count {
		logical := outputtest.Parse(t, fmt.Sprintf("~/.agents/skills/skill-%03d", index))
		for range 2 {
			if err := namespace.Bind(target.ScopeGlobal, logical); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(namespace.state.globals) != count {
		t.Fatalf("global bindings = %d, want %d", len(namespace.state.globals), count)
	}
	if len(namespace.state.retained) != 1 {
		t.Fatalf("retained roots = %d, want 1 shared root", len(namespace.state.retained))
	}
	retained := namespace.state.retained[0]
	for logical, binding := range namespace.state.globals {
		if binding.root != retained {
			t.Fatalf("binding %q retained a duplicate root witness", logical)
		}
		resolved, err := namespace.Resolve(target.ScopeGlobal, logical)
		if err != nil || resolved.root != retained || !resolved.destination.Equal(binding.destination) {
			t.Fatalf("binding %q did not resolve to the retained physical binding: %+v, %v", logical, resolved, err)
		}
		capability, err := namespace.Acquire(binding)
		if err != nil {
			t.Fatalf("binding %q lost shared root authority: %v", logical, err)
		}
		if err := capability.Close(); err != nil {
			t.Fatalf("close binding %q capability: %v", logical, err)
		}
	}
	if err := namespace.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := retained.Authority(); err == nil {
		t.Fatal("Close retained the owned global root")
	}
}

func TestDestinationRejectsPartialAddressFacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	namespace := namespaceForTest(t, "")
	logical := outputtest.Parse(t, "~/.codex/config.toml")
	if err := namespace.Bind(target.ScopeGlobal, logical); err != nil {
		t.Fatal(err)
	}
	resolved, err := namespace.Resolve(target.ScopeGlobal, logical)
	if err != nil || !resolved.IsRooted() {
		t.Fatalf("bound destination = %+v, %v", resolved, err)
	}
	for _, mutate := range []func(*Destination){
		func(candidate *Destination) { candidate.scope = target.Scope("") },
		func(candidate *Destination) { candidate.logical = output.Destination{} },
		func(candidate *Destination) { candidate.hostPath = filepath.Join(home, "different") },
		func(candidate *Destination) { candidate.root = nil },
		func(candidate *Destination) { candidate.destination = rootedpath.Destination{} },
		func(candidate *Destination) { candidate.owner = nil },
	} {
		candidate := resolved
		mutate(&candidate)
		if candidate.IsRooted() {
			t.Fatalf("partially initialized mutation destination remained valid: %+v", candidate)
		}
		if capability, err := namespace.Acquire(candidate); err == nil || capability != nil {
			t.Fatalf("partially initialized destination acquired authority: %v, %v", capability, err)
		}
	}
}

func TestNamespaceCopiesAndDestinationsShareCloseLifetime(t *testing.T) {
	project := t.TempDir()
	namespace := namespaceForTest(t, project)
	bound, err := namespace.ResolveProject(outputtest.Parse(t, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	other := namespaceForTest(t, project)
	if capability, err := other.Acquire(bound); err == nil || capability != nil {
		t.Fatalf("foreign destination acquired authority: %v, %v", capability, err)
	}
	copy := namespace
	borrowed := namespace.state.project
	if err := namespace.Close(); err != nil {
		t.Fatal(err)
	}
	if bound.IsRooted() {
		t.Fatal("closed destination retained live namespace authority")
	}
	if capability, err := copy.Acquire(bound); err == nil || capability != nil {
		t.Fatalf("copied namespace escaped Close: %v, %v", capability, err)
	}
	if _, err := copy.ResolveProject(outputtest.Parse(t, "AGENTS.md")); err == nil {
		t.Fatal("copied namespace rebound after Close")
	}
	if _, err := borrowed.Authority(); err != nil {
		t.Fatalf("namespace closed its borrowed project root: %v", err)
	}
	if err := copy.Close(); err != nil {
		t.Fatalf("repeated Close = %v", err)
	}
}

func TestBorrowedDestinationKeepsSourceRootCustodyAndIdentity(t *testing.T) {
	source := namespaceForTest(t, t.TempDir())
	logical := outputtest.Parse(t, "AGENTS.md")
	selected, err := source.ResolveProject(logical)
	if err != nil {
		t.Fatal(err)
	}
	borrower := namespaceForTest(t, "")
	borrowed, err := borrower.BorrowDestination(target.ScopeProject, logical, source.state.project, selected.RootedPath())
	if err != nil {
		t.Fatal(err)
	}
	capability, err := borrower.Acquire(borrowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := capability.Close(); err != nil {
		t.Fatal(err)
	}
	other := namespaceForTest(t, t.TempDir())
	mismatched, err := borrower.BorrowDestination(target.ScopeProject, logical, other.state.project, selected.RootedPath())
	if err != nil {
		t.Fatal(err)
	}
	if capability, err := borrower.Acquire(mismatched); err == nil || capability != nil {
		t.Fatalf("mismatched borrowed root acquired authority: %v, %v", capability, err)
	}
	if err := source.state.project.Close(); err != nil {
		t.Fatal(err)
	}
	if capability, err := borrower.Acquire(borrowed); err == nil || capability != nil {
		t.Fatalf("closed source root acquired authority: %v, %v", capability, err)
	}
	if err := borrower.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := other.state.project.Authority(); err != nil {
		t.Fatalf("borrower closed its source root: %v", err)
	}
}

func TestLateProjectBindingQualifiesBorrowedIdentityBeforePublication(t *testing.T) {
	namespace := namespaceForTest(t, "")
	selected := namespaceForTest(t, t.TempDir())
	other := namespaceForTest(t, t.TempDir())
	root := selected.state.project
	if err := namespace.BindProject(root, other.state.project); err == nil {
		t.Fatal("different borrowed project root was admitted")
	}
	logical := outputtest.Parse(t, "AGENTS.md")
	if _, err := namespace.ResolveProject(logical); err == nil {
		t.Fatal("failed qualification published a project binding")
	}
	if err := namespace.BindProject(root, root); err != nil {
		t.Fatal(err)
	}
	bound, err := namespace.ResolveProject(logical)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := namespace.ProjectProvenance()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := bound.RootedPath().Root().Provenance()
	if err != nil || provenance != expected {
		t.Fatalf("late project provenance = %+v, want %+v: %v", provenance, expected, err)
	}
	if err := namespace.BindProject(other.state.project, nil); err == nil {
		t.Fatal("late project binding was replaced")
	}
}
