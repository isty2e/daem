//go:build darwin || linux

package mutation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWideVisibilityAcceptanceRejectsRetargetAndAllowsFreshRetry(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}

	var domains []Domain
	for index := 0; index < 128; index++ {
		path := filepath.Join(root, fmt.Sprintf("entry-%d", index))
		if index == 127 {
			path = filepath.Join(alias, "value")
		}
		domain, err := NewLogicalPathDomain(LogicalPathRequest{
			Path: path, Access: AccessExclusive, Effect: PathEffectReferent,
		})
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, domain)
	}
	store := mutationTestStore(t)
	set, err := store.Acquire(context.Background(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()
	before := append([]Domain(nil), set.domains...)

	retarget := func(target string) {
		t.Helper()
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, alias); err != nil {
			t.Fatal(err)
		}
	}
	retarget(second)
	if matches, err := set.AcceptVisibilityChanges(context.Background()); err != nil || matches {
		t.Fatalf("retarget acceptance = %t, %v; want refusal", matches, err)
	}
	if !reflect.DeepEqual(before, set.domains) {
		t.Fatal("refused observation partially rebound domains")
	}

	retarget(first)
	if matches, err := set.AcceptVisibilityChanges(context.Background()); err != nil || !matches {
		t.Fatalf("fresh retry = %t, %v; want acceptance", matches, err)
	}
	if matches, err := set.DomainsMatchCurrent(context.Background()); err != nil || !matches {
		t.Fatalf("accepted lease state = %t, %v", matches, err)
	}
}
