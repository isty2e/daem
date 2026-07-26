//go:build unix

package configrelation

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestRemoveExactSourcePreservesUnrelatedJSONCBytesAndMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.jsonc")
	content := []byte(`{
  // retained comment
  "plugin": [
    "@acme/keep",
    ["@acme/remove", {"flag": true,}],
  ],
  "unknown": {"value": 1,},
}
`)
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatal(err)
	}
	authority := bindAuthority(t, root, path)
	defer authority.Close()

	changed, err := removeOpenCodeExactSource(
		t.Context(),
		storagecommit.Adapter{},
		authority,
		"@acme/remove",
	)
	if err != nil {
		t.Fatalf("RemoveExactSource: %v", err)
	}
	if !changed {
		t.Fatal("RemoveExactSource reported no change")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{
  // retained comment
  "plugin": [
    "@acme/keep",
  ],
  "unknown": {"value": 1,},
}
`)
	if string(got) != string(want) {
		t.Fatalf("content = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
}

func TestRemoveExactSourcePreservesCRLFAndUnicodeOutsideSelectedRow(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.jsonc")
	content := []byte("{\r\n" +
		"  // 설명 유지\r\n" +
		"  \"plugin\": [\"@acme/remove\", \"@acme/유지\",],\r\n" +
		"  \"label\": \"그대로\",\r\n" +
		"}\r\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	authority := bindAuthority(t, root, path)
	defer authority.Close()

	changed, err := removeOpenCodeExactSource(
		t.Context(),
		storagecommit.Adapter{},
		authority,
		"@acme/remove",
	)
	if err != nil {
		t.Fatalf("RemoveExactSource: %v", err)
	}
	if !changed {
		t.Fatal("RemoveExactSource reported no change")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("{\r\n" +
		"  // 설명 유지\r\n" +
		"  \"plugin\": [ \"@acme/유지\",],\r\n" +
		"  \"label\": \"그대로\",\r\n" +
		"}\r\n")
	if string(got) != string(want) {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestRemoveExactSourceTreatsMissingAndAbsentRelationsAsNoOps(t *testing.T) {
	for _, test := range []struct {
		name    string
		content []byte
		present bool
	}{
		{name: "missing"},
		{name: "whitespace", content: []byte("  \n"), present: true},
		{name: "other relation", content: []byte(`{"plugin":["@acme/keep"]}`), present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "opencode.json")
			if test.present {
				if err := os.WriteFile(path, test.content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			authority := bindAuthority(t, root, path)
			defer authority.Close()

			changed, err := removeOpenCodeExactSource(
				t.Context(),
				storagecommit.Adapter{},
				authority,
				"@acme/remove",
			)
			if err != nil {
				t.Fatalf("RemoveExactSource: %v", err)
			}
			if changed {
				t.Fatal("RemoveExactSource reported a no-op as changed")
			}
			if !test.present {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("missing path stat error = %v", err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(test.content) {
				t.Fatalf("no-op content = %q, want %q", got, test.content)
			}
		})
	}
}

func TestRemoveExactSourceRejectsAmbiguousRowsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.jsonc")
	content := []byte(`{"plugin":["@acme/remove",["@acme/remove",{}],],}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	authority := bindAuthority(t, root, path)
	defer authority.Close()

	_, err := removeOpenCodeExactSource(
		t.Context(),
		storagecommit.Adapter{},
		authority,
		"@acme/remove",
	)
	if err == nil || !strings.Contains(err.Error(), "2 exact plugin rows") {
		t.Fatalf("RemoveExactSource error = %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(content) {
		t.Fatalf("ambiguous content changed to %q", got)
	}
}

func TestRemoveExactSourceRefusesConcurrentReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.json")
	if err := os.WriteFile(path, []byte(`{"plugin":["@acme/remove"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	authority := bindAuthority(t, root, path)
	defer authority.Close()
	concurrent := []byte(`{"plugin":["@acme/remove","@acme/concurrent"]}`)
	store := concurrentReplaceStore{
		RootedStore: storagecommit.Adapter{},
		beforeReplace: func() {
			if err := os.WriteFile(path, concurrent, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}

	if _, err := removeOpenCodeExactSource(
		t.Context(),
		store,
		authority,
		"@acme/remove",
	); err == nil {
		t.Fatal("RemoveExactSource accepted a concurrent replacement")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(concurrent) {
		t.Fatalf("concurrent content = %q, want %q", got, concurrent)
	}
}

type concurrentReplaceStore struct {
	mutationfs.RootedStore
	beforeReplace func()
}

func (store concurrentReplaceStore) ReplaceRootedFile(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) error {
	store.beforeReplace()
	return store.RootedStore.ReplaceRootedFile(ctx, capability, content, mode, expected)
}

func bindAuthority(
	t *testing.T,
	root string,
	path string,
) *rootedpath.EntryAuthority {
	t.Helper()
	selected, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatalf("CaptureRoot: %v", err)
	}
	t.Cleanup(func() {
		if err := selected.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	})
	authority, err := rootedpath.BindSelectedEntryAuthority(selected, root, path)
	if err != nil {
		t.Fatalf("BindSelectedEntryAuthority: %v", err)
	}
	return authority
}
