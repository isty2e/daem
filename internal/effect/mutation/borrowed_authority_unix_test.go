//go:build darwin || linux

package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type authorityObservationBudget struct {
	components int
	pathLimit  int
	entries    int
	limit      int
	cause      error
}

func (budget *authorityObservationBudget) AdmitPathComponents(count int) error {
	if budget.pathLimit > 0 && count > budget.pathLimit-budget.components {
		return budget.cause
	}
	budget.components += count
	return nil
}

func (budget *authorityObservationBudget) AdmitPhysicalWork(paths, entries int, _ int64) error {
	if entries > budget.limit-budget.entries {
		return budget.cause
	}
	budget.components += paths
	budget.entries += entries
	return nil
}

func TestBorrowedAuthorityRequiresExactKindsEffectsAndRouteFacts(t *testing.T) {
	for _, mode := range []string{"complete", "missing control", "shared control", "physical control", "ancestor", "missing entry", "missing referent", "wrong target", "wrong scope", "logical document"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			control, document := filepath.Join(root, "state.json"), filepath.Join(root, "config.toml")
			for _, path := range []string{control, document} {
				if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			var domains []Domain
			for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
				path, access := control, AccessExclusive
				if mode == "ancestor" {
					path = root
				}
				if mode == "shared control" {
					access = AccessShared
				}
				controlDomain, err := NewLogicalPathDomain(LogicalPathRequest{Path: path, Access: access, Effect: effect})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "physical control" {
					controlDomain, err = NewPhysicalPathDomain(PhysicalPathRequest{Path: path, Access: AccessExclusive, Effect: effect, Target: "codex", Scope: "project"})
					if err != nil {
						t.Fatal(err)
					}
				}
				if mode != "missing control" {
					domains = append(domains, controlDomain)
				}
				if (mode == "missing entry" && effect == PathEffectDirectoryEntry) || (mode == "missing referent" && effect == PathEffectReferent) {
					continue
				}
				target, scope := "codex", "project"
				if mode == "wrong target" {
					target = "pi"
				}
				if mode == "wrong scope" {
					scope = "user"
				}
				domain, err := NewPhysicalPathDomain(PhysicalPathRequest{Path: document, Access: AccessExclusive, Effect: effect, Target: target, Scope: scope})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "logical document" {
					domain, err = NewLogicalPathDomain(LogicalPathRequest{Path: document, Access: AccessExclusive, Effect: effect})
					if err != nil {
						t.Fatal(err)
					}
				}
				domains = append(domains, domain)
			}
			set, err := store.Acquire(t.Context(), domains...)
			if err != nil {
				t.Fatal(err)
			}
			defer set.Release()
			budget := &authorityObservationBudget{limit: 1_000_000, cause: errors.New("metadata budget exhausted")}
			matched, err := set.MatchCurrentExclusiveRequestsBounded(t.Context(), []string{control}, []PhysicalAuthorityRequest{{Path: document, Target: "codex", Scope: "project"}}, 256, budget)
			if err != nil || matched != (mode == "complete") {
				t.Fatalf("coverage mode=%s matched=%t err=%v", mode, matched, err)
			}
			if budget.components == 0 || budget.entries == 0 {
				t.Fatal("borrowed validation bypassed allocation")
			}
		})
	}
}

func TestBorrowedAuthorityIsFreshBoundedAndDoesNotReacquire(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var domains []Domain
	for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
		domain, err := NewPhysicalPathDomain(PhysicalPathRequest{Path: path, Access: AccessExclusive, Effect: effect, Target: "codex", Scope: "project"})
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, domain)
	}
	set, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()
	requests := []PhysicalAuthorityRequest{{Path: path, Target: "codex", Scope: "project"}}
	budget := &authorityObservationBudget{limit: 1_000_000, cause: errors.New("metadata budget exhausted")}
	check := func(ctx context.Context) (bool, error) {
		return set.MatchCurrentExclusiveRequestsBounded(ctx, nil, requests, 256, budget)
	}
	if ok, err := check(t.Context()); err != nil || !ok {
		t.Fatal("complete borrow failed", err)
	}
	before := budget.components
	replacement := filepath.Join(root, "replacement")
	if err := os.WriteFile(replacement, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if ok, err := check(t.Context()); err != nil || !ok {
		t.Fatal("same-namespace replacement was refused", err)
	}
	if budget.components <= before {
		t.Fatal("borrow reused a prior validation snapshot")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if ok, err := check(canceled); ok || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled borrow was accepted", err)
	}
	budget.pathLimit = budget.components + 1
	if ok, err := check(t.Context()); ok || !errors.Is(err, budget.cause) {
		t.Fatal("exhausted path allowance was bypassed", err)
	}
	budget.pathLimit = 0
	budget.limit = budget.entries
	if ok, err := check(t.Context()); ok || !errors.Is(err, budget.cause) {
		t.Fatal("exhausted metadata allowance was bypassed", err)
	}
	if err := set.Release(); err != nil {
		t.Fatal(err)
	}
	if ok, err := check(t.Context()); ok || err == nil {
		t.Fatal("released borrow remained valid")
	}
}

func TestBorrowedAuthorityRejectsRedirectedAncestorAndReplacedLeaseNamespace(t *testing.T) {
	for _, replaced := range []string{"document ancestor", "lease namespace"} {
		t.Run(replaced, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"one", "two"} {
				if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, name, "config.toml"), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(root, "selected")
			if err := os.Symlink(filepath.Join(root, "one"), alias); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(alias, "config.toml")
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			var domains []Domain
			for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
				domain, err := NewPhysicalPathDomain(PhysicalPathRequest{Path: path, Access: AccessExclusive, Effect: effect, Target: "codex", Scope: "project"})
				if err != nil {
					t.Fatal(err)
				}
				domains = append(domains, domain)
			}
			set, err := store.Acquire(t.Context(), domains...)
			if err != nil {
				t.Fatal(err)
			}
			defer set.Release()
			budget := &authorityObservationBudget{limit: 1_000_000, cause: errors.New("exhausted")}
			check := func() (bool, error) {
				return set.MatchCurrentExclusiveRequestsBounded(t.Context(), nil, []PhysicalAuthorityRequest{{Path: path, Target: "codex", Scope: "project"}}, 256, budget)
			}
			if matches, err := check(); err != nil || !matches {
				t.Fatal("initial borrow failed", err)
			}

			if replaced == "document ancestor" {
				if err := os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "two"), alias); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(store.root, store.root+"-retired"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(store.root, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if matches, err := check(); matches {
				t.Fatalf("stale borrow was accepted: %v", err)
			}
		})
	}
}

func TestBoundedBorrowObservationPreservesCanonicalPathFacts(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"config.toml", "é.toml", "e\u0301.toml", "line\nfeed.toml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "config.toml"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.toml", "é.toml", "e\u0301.toml", "alias", "missing.toml", "new-é.toml", "missing/sub.toml", "line\nfeed.toml", "missing\nname.toml"} {
		for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
			path := filepath.Join(root, name)
			original, err := canonicalPathIdentity(path, effect)
			if err != nil {
				t.Fatal(err)
			}
			budget := &authorityObservationBudget{limit: 1_000_000, cause: errors.New("exhausted")}
			bounded, err := newBoundedPathIdentityObserver(t.Context(), 256, budget)(path, effect)
			if err != nil {
				t.Fatal(path, effect, err)
			}
			if original.keyPath != bounded.keyPath || original.accessPath != bounded.accessPath || original.witness != bounded.witness || original.namespaceLease != bounded.namespaceLease || !sameProvisionalPathIntent(original.provisional, bounded.provisional) {
				t.Fatalf("bounded observation changed canonical facts for %s effect=%v", name, effect)
			}
		}
	}
}

func TestBorrowedAuthorityBoundsRedirectedDataRootBeforeMetadataLookup(t *testing.T) {
	selected := filepath.Join(t.TempDir(), "selected")
	data := filepath.Join(selected, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(data)
	if err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(document, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	var domains []Domain
	for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
		domain, err := NewPhysicalPathDomain(PhysicalPathRequest{Path: document, Access: AccessExclusive, Effect: effect, Target: "codex", Scope: "project"})
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, domain)
	}
	set, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()

	deep := t.TempDir()
	for range 64 {
		deep = filepath.Join(deep, "next")
	}
	if err := os.MkdirAll(filepath.Join(deep, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(selected, selected+"-retired"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(deep, selected); err != nil {
		t.Fatal(err)
	}

	exhausted := errors.New("bounded alias traversal exhausted")
	budget := &authorityObservationBudget{pathLimit: 32, limit: 1_000_000, cause: exhausted}
	matches, err := set.MatchCurrentExclusiveRequestsBounded(t.Context(), nil, []PhysicalAuthorityRequest{{Path: document, Target: "codex", Scope: "project"}}, 256, budget)
	if matches || !errors.Is(err, exhausted) {
		t.Fatal("data-root alias interpretation escaped its allocation", err)
	}
}
