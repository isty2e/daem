package mutation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLeaseCurrentPhysicalRequestsMatchSeparateValidation(t *testing.T) {
	for _, mode := range []string{"exclusive", "shared", "entry-only", "logical"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.json")
			domains := mutationTestPhysicalDomains(t, path, "codex", "project")
			switch mode {
			case "shared":
				for i := range domains {
					domains[i].access = AccessShared
				}
			case "entry-only":
				domains = domains[:1]
			case "logical":
				domains = []Domain{mutationTestLogicalDomain(t, path, AccessExclusive)}
			}
			set, err := mutationTestStore(t).Acquire(context.Background(), domains...)
			if err != nil {
				t.Fatal(err)
			}
			defer set.Release()
			exact := PhysicalAuthorityRequest{Path: path, Target: "codex", Scope: "project"}
			for _, requests := range [][]PhysicalAuthorityRequest{
				nil,
				{exact},
				{exact, exact},
				{{Path: path, Target: "claude-code", Scope: "project"}},
				{{Path: path, Target: "codex", Scope: "global"}},
				{exact, {Path: filepath.Join(root, "other"), Target: "codex", Scope: "project"}},
				{{Path: path, Target: "", Scope: "project"}},
				{{Path: "\x00", Target: "codex", Scope: "project"}},
			} {
				compareCurrentPhysicalRequests(t, context.Background(), set, requests)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			compareCurrentPhysicalRequests(t, ctx, set, []PhysicalAuthorityRequest{exact})
			compareCurrentPhysicalRequests(t, nil, set, []PhysicalAuthorityRequest{exact})
			if err := set.Release(); err != nil {
				t.Fatal(err)
			}
			compareCurrentPhysicalRequests(t, context.Background(), set, []PhysicalAuthorityRequest{exact})
			compareCurrentPhysicalRequests(t, context.Background(), nil, []PhysicalAuthorityRequest{exact})
		})
	}
}

func TestLeaseCurrentPhysicalRequestsRefreshAfterVisibilityChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "Future", "Caf\u00e9")
	domains := mutationTestPhysicalDomains(t, path, "codex", "project")
	set, err := mutationTestStore(t).Acquire(ctx, domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()
	requests := []PhysicalAuthorityRequest{{Path: path, Target: "codex", Scope: "project"}}
	compareCurrentPhysicalRequests(t, ctx, set, requests)
	for _, exists := range []bool{true, false} {
		if exists {
			err = os.MkdirAll(path, 0o700)
		} else {
			err = os.RemoveAll(filepath.Join(root, "Future"))
		}
		if err != nil {
			t.Fatal(err)
		}
		compareCurrentPhysicalRequests(t, ctx, set, requests)
		compareCurrentPhysicalRequests(t, ctx, set, nil)
		if accepted, err := set.AcceptVisibilityChanges(ctx); err != nil || !accepted {
			t.Fatalf("accept own visibility change: %t, %v", accepted, err)
		}
		compareCurrentPhysicalRequests(t, ctx, set, requests)
	}
}

func compareCurrentPhysicalRequests(t *testing.T, ctx context.Context, set *LeaseSet, requests []PhysicalAuthorityRequest) {
	t.Helper()
	want, wantErr := separatePhysicalRequestValidation(ctx, set, requests)
	got, gotErr := set.MatchCurrentPhysicalRequests(ctx, requests...)
	if got != want || (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("combined=%t,%v separate=%t,%v requests=%v", got, gotErr, want, wantErr, requests)
	}
	if gotErr != nil && gotErr.Error() != wantErr.Error() {
		t.Fatalf("combined error=%v separate error=%v", gotErr, wantErr)
	}
}

func separatePhysicalRequestValidation(ctx context.Context, set *LeaseSet, requests []PhysicalAuthorityRequest) (bool, error) {
	authority, err := NewPhysicalAuthoritySet(requests...)
	if err != nil {
		return false, err
	}
	matches, err := set.DomainsMatchCurrent(ctx)
	if err != nil || !matches {
		return matches, err
	}
	return set.CoversPhysicalAuthority(authority)
}
