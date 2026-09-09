//go:build darwin

package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDarwinSharedObservationCoalescesAncestorReads(t *testing.T) {
	descriptors, cases := 0, 0
	observation := darwinPathObservation{
		descriptorPath: func(path string, _ bool) (string, error) {
			descriptors++
			return path, nil
		},
		directoryCase: func(string) (pathCaseSemantics, error) {
			cases++
			return pathCaseSensitive, nil
		},
	}.shared()
	for _, name := range []string{"first", "second", "third"} {
		identity, err := canonicalDarwinPath(pathSelection{
			anchorPath: "/stored/parent", missingComponents: []string{name},
		}, PathEffectReferent, observation)
		if err != nil || identity.keyPath != "/stored/parent/"+name || identity.witness != "darwin-case-v1:sss" {
			t.Fatalf("identity = %#v, %v", identity, err)
		}
	}
	if descriptors != 1 || cases != 3 {
		t.Fatalf("physical observations: descriptor=%d case=%d; want 1, 3", descriptors, cases)
	}
}

func TestDarwinSharedObservationKeepsFollowModeDistinct(t *testing.T) {
	calls := 0
	observation := darwinPathObservation{
		descriptorPath: func(_ string, noFollow bool) (string, error) {
			calls++
			if noFollow {
				return "/parent/link", nil
			}
			return "/target/file", nil
		},
	}.shared()
	for range 2 {
		for _, noFollow := range []bool{false, true} {
			got, err := observation.descriptorPath("/parent/link", noFollow)
			want := "/target/file"
			if noFollow {
				want = "/parent/link"
			}
			if err != nil || got != want {
				t.Fatalf("descriptor(%t) = %q, %v; want %q", noFollow, got, err, want)
			}
		}
	}
	if calls != 2 {
		t.Fatalf("descriptor observations = %d; want 2", calls)
	}
}

func TestDarwinSharedObservationDoesNotCacheFailures(t *testing.T) {
	failure := errors.New("ordinary observation failure")
	descriptors, cases := 0, 0
	observation := darwinPathObservation{
		descriptorPath: func(path string, _ bool) (string, error) {
			descriptors++
			if descriptors == 1 {
				return "", failure
			}
			return path, nil
		},
		directoryCase: func(string) (pathCaseSemantics, error) {
			cases++
			if cases == 1 {
				return 0, failure
			}
			return pathCaseSensitive, nil
		},
	}.shared()
	if _, err := observation.descriptorPath("/parent", false); !errors.Is(err, failure) {
		t.Fatalf("descriptor error = %v", err)
	}
	if _, err := observation.directoryCase("/parent"); !errors.Is(err, failure) {
		t.Fatalf("case error = %v", err)
	}
	for range 2 {
		if got, err := observation.descriptorPath("/parent", false); err != nil || got != "/parent" {
			t.Fatalf("descriptor retry = %q, %v", got, err)
		}
		if got, err := observation.directoryCase("/parent"); err != nil || got != pathCaseSensitive {
			t.Fatalf("case retry = %v, %v", got, err)
		}
	}
	if descriptors != 2 || cases != 2 {
		t.Fatalf("retry observations = %d, %d; want 2, 2", descriptors, cases)
	}
}

func TestLeaseObservationPassesTrackUnicodePublicationAndRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Caf\u00e9")
	domains := mutationTestPhysicalDomains(t, path, "codex", "project")
	store := mutationTestStore(t)
	set, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()
	if matches, err := set.DomainsMatchCurrent(t.Context()); err != nil || !matches {
		t.Fatalf("initial validation = %t, %v", matches, err)
	}
	if accepted, err := set.AcceptVisibilityChanges(t.Context()); err != nil || !accepted {
		t.Fatalf("initial acceptance = %t, %v", accepted, err)
	}
	for _, exists := range []bool{true, false} {
		if exists {
			err = os.WriteFile(path, []byte("content"), 0o600)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if matches, err := set.DomainsMatchCurrent(t.Context()); err != nil || matches {
			t.Fatalf("fresh validation after exists=%t: %t, %v; want changed", exists, matches, err)
		}
		if matches, err := set.VisibilityAuthorityMatchesCurrent(t.Context()); err != nil || !matches {
			t.Fatalf("compensation authority = %t, %v", matches, err)
		}
		if accepted, err := set.AcceptVisibilityChanges(t.Context()); err != nil || !accepted {
			t.Fatalf("fresh acceptance = %t, %v", accepted, err)
		}
		if matches, err := set.DomainsMatchCurrent(t.Context()); err != nil || !matches {
			t.Fatalf("accepted identity = %t, %v", matches, err)
		}
	}
	before := append([]Domain(nil), set.domains...)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, observe := range []func(context.Context) (bool, error){
		set.DomainsMatchCurrent, set.VisibilityAuthorityMatchesCurrent, set.AcceptVisibilityChanges,
	} {
		if accepted, err := observe(ctx); accepted || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled observation = %t, %v", accepted, err)
		}
	}
	if !reflect.DeepEqual(before, set.domains) {
		t.Fatal("canceled observation changed accepted domains")
	}
}
