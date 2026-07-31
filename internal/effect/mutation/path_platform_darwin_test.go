//go:build darwin

package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCanonicalDarwinPathAppliesMixedComponentSemanticsAndMissingAnchorMode(t *testing.T) {
	selection := pathSelection{
		anchorPath:        "/input/anchor",
		missingComponents: []string{"Future", "Leaf"},
	}
	caseByDirectory := map[string]pathCaseSemantics{
		"/":                 pathCaseInsensitive,
		"/Stored":           pathCaseSensitive,
		"/Stored/Sensitive": pathCaseInsensitive,
	}
	identity, err := canonicalDarwinPath(selection, PathEffectReferent, darwinPathObservation{
		descriptorPath: func(path string, noFollow bool) (string, error) {
			if path != selection.anchorPath || noFollow {
				t.Fatalf("descriptorPath(%q, %v)", path, noFollow)
			}
			return "/Stored/Sensitive", nil
		},
		directoryCase: func(path string) (pathCaseSemantics, error) {
			mode, ok := caseByDirectory[path]
			if !ok {
				t.Fatalf("unexpected case query for %q", path)
			}
			return mode, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if identity.accessPath != "/Stored/Sensitive/Future/Leaf" {
		t.Fatalf("access path = %q", identity.accessPath)
	}
	if identity.keyPath != "/stored/Sensitive/future/leaf" {
		t.Fatalf("key path = %q", identity.keyPath)
	}
	if identity.witness != "darwin-case-v1:isii" {
		t.Fatalf("witness = %q", identity.witness)
	}
}

func TestCanonicalDarwinPathUsesStoredFinalEntrySpellingWithoutFollowing(t *testing.T) {
	selection := pathSelection{
		anchorPath: "/input/parent", missingComponents: []string{"alias"}, finalEntryMayExist: true,
	}
	identity, err := canonicalDarwinPath(selection, PathEffectDirectoryEntry, darwinPathObservation{
		descriptorPath: func(path string, noFollow bool) (string, error) {
			switch {
			case path == selection.anchorPath && !noFollow:
				return "/Stored/Parent", nil
			case path == "/Stored/Parent/alias" && noFollow:
				return "/Stored/Parent/ActualLink", nil
			default:
				t.Fatalf("descriptorPath(%q, %v)", path, noFollow)
				return "", nil
			}
		},
		directoryCase: func(string) (pathCaseSemantics, error) {
			return pathCaseInsensitive, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if identity.accessPath != "/Stored/Parent/ActualLink" ||
		identity.keyPath != "/stored/parent/actuallink" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestCanonicalDarwinPathFailsClosedOnCaseObservationErrors(t *testing.T) {
	selection := pathSelection{anchorPath: "/input"}
	for _, test := range []struct {
		name string
		mode pathCaseSemantics
		err  error
	}{
		{name: "capability error", err: errors.New("unavailable")},
		{name: "unknown capability", mode: 99},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := canonicalDarwinPath(selection, PathEffectReferent, darwinPathObservation{
				descriptorPath: func(string, bool) (string, error) { return "/Existing", nil },
				directoryCase:  func(string) (pathCaseSemantics, error) { return test.mode, test.err },
			})
			if err == nil {
				t.Fatal("expected fail-closed observation error")
			}
		})
	}
}

func TestDarwinNativeDirectoryEntryAndReferentUseDescriptorStoredSpelling(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "StoredLink")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	entry, err := canonicalPathIdentity(link, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	referent, err := canonicalPathIdentity(link, PathEffectReferent)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(entry.accessPath) != "StoredLink" {
		t.Fatalf("entry access path = %q, want stored link", entry.accessPath)
	}
	if filepath.Base(referent.accessPath) != "Target" {
		t.Fatalf("referent access path = %q, want target", referent.accessPath)
	}
	if entry.keyPath == referent.keyPath {
		t.Fatalf("entry and referent keys both %q", entry.keyPath)
	}
}

func TestDarwinNativeDirectoryEntryRetainsDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "DanglingLink")
	if err := os.Symlink(filepath.Join(root, "missing-target"), link); err != nil {
		t.Fatal(err)
	}
	entry, err := canonicalPathIdentity(link, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatalf("directory-entry identity: %v", err)
	}
	if filepath.Base(entry.accessPath) != "DanglingLink" {
		t.Fatalf("entry access path = %q, want dangling symlink spelling", entry.accessPath)
	}
	if _, err := canonicalPathIdentity(link, PathEffectReferent); err == nil {
		t.Fatal("referent identity accepted a dangling symlink")
	}
}

func TestDarwinNativeDirectoryEntryObservesFIFONonblocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "EventPipe")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatalf("FIFO directory-entry identity: %v", err)
	}
	if filepath.Base(identity.accessPath) != "EventPipe" {
		t.Fatalf("FIFO access path = %q", identity.accessPath)
	}
}

func TestDarwinNativeCaseOnlyRenamePreservesOnlyInsensitiveAuthority(t *testing.T) {
	root := t.TempDir()
	beforePath := filepath.Join(root, "BeforeName")
	afterPath := filepath.Join(root, "beforename")
	if err := os.WriteFile(beforePath, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := canonicalPathIdentity(beforePath, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(beforePath, afterPath); err != nil {
		t.Fatal(err)
	}
	after, err := canonicalPathIdentity(afterPath, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := darwinDirectoryCaseSemantics(root)
	if err != nil {
		t.Fatal(err)
	}
	if mode == pathCaseInsensitive &&
		(before.keyPath != after.keyPath || before.witness != after.witness) {
		t.Fatalf("insensitive rename changed authority: before=%#v after=%#v", before, after)
	}
	if mode == pathCaseSensitive && before.keyPath == after.keyPath {
		t.Fatalf("sensitive rename retained authority key %q", before.keyPath)
	}
}

func TestDarwinNativeRootHasCompleteIdentityWithoutComponents(t *testing.T) {
	identity, err := canonicalPathIdentity(string(filepath.Separator), PathEffectReferent)
	if err != nil {
		t.Fatal(err)
	}
	if identity.accessPath != string(filepath.Separator) ||
		identity.keyPath != string(filepath.Separator) ||
		identity.witness != "darwin-case-v1:" {
		t.Fatalf("root identity = %#v", identity)
	}
}

func TestDarwinNativeCaseAliasesFollowObservedDirectoryCapability(t *testing.T) {
	root := t.TempDir()
	stored := filepath.Join(root, "StoredName")
	if err := os.WriteFile(stored, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	mode, err := darwinDirectoryCaseSemantics(root)
	if err != nil {
		t.Fatal(err)
	}
	storedIdentity, err := canonicalPathIdentity(stored, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "storedname")
	aliasIdentity, err := canonicalPathIdentity(alias, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	switch mode {
	case pathCaseInsensitive:
		if storedIdentity.keyPath != aliasIdentity.keyPath {
			t.Fatalf("case aliases did not coalesce: %q != %q", storedIdentity.keyPath, aliasIdentity.keyPath)
		}
		if filepath.Base(aliasIdentity.accessPath) != "StoredName" {
			t.Fatalf("alias access path = %q, want stored spelling", aliasIdentity.accessPath)
		}
	case pathCaseSensitive:
		if storedIdentity.keyPath == aliasIdentity.keyPath {
			t.Fatalf("case-distinct paths coalesced as %q", storedIdentity.keyPath)
		}
		if err := os.WriteFile(alias, []byte("other"), 0o600); err != nil {
			t.Fatal(err)
		}
		second, err := canonicalPathIdentity(alias, PathEffectDirectoryEntry)
		if err != nil {
			t.Fatal(err)
		}
		if storedIdentity.keyPath == second.keyPath {
			t.Fatalf("existing case-distinct entries coalesced as %q", second.keyPath)
		}
	default:
		t.Fatalf("unexpected case mode %d", mode)
	}
}

func TestDarwinNativeExistingNormalizationAliasUsesStoredSpelling(t *testing.T) {
	root := t.TempDir()
	stored := filepath.Join(root, "Caf\u00e9")
	alias := filepath.Join(root, "Cafe\u0301")
	if err := os.WriteFile(stored, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(alias); err != nil {
		if os.IsNotExist(err) {
			t.Skip("temporary filesystem does not resolve the tested normalization alias")
		}
		t.Fatal(err)
	}

	storedIdentity, err := canonicalPathIdentity(stored, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	aliasIdentity, err := canonicalPathIdentity(alias, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	if aliasIdentity != storedIdentity {
		t.Fatalf("normalization alias identity = %#v, want stored identity %#v", aliasIdentity, storedIdentity)
	}
}

func TestDarwinNativeLegacyNormalizationAliasKeyIsRejected(t *testing.T) {
	root := t.TempDir()
	composed := filepath.Join(root, "Caf\u00e9")
	decomposed := filepath.Join(root, "Cafe\u0301")
	if err := os.WriteFile(composed, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(decomposed); err != nil {
		if os.IsNotExist(err) {
			t.Skip("temporary filesystem does not resolve the tested normalization alias")
		}
		t.Fatal(err)
	}

	for _, candidate := range []string{composed, decomposed} {
		authority, err := ObservePersistedDirectoryEntryAuthority(candidate)
		if err != nil {
			t.Fatal(err)
		}
		selection, err := selectPath(candidate, PathEffectDirectoryEntry)
		if err != nil {
			t.Fatal(err)
		}
		legacy := strings.ToLower(selectedAccessPath(selection))
		if legacy == authority.CurrentKey() {
			continue
		}
		err = authority.ValidatePersistedKey(legacy)
		if err == nil || !strings.Contains(err.Error(), "legacy Darwin-wide case fold") ||
			!strings.Contains(err.Error(), "docs/troubleshooting.md#legacy-darwin-path-authority") {
			t.Fatalf("legacy normalization alias error = %v", err)
		}
		return
	}
	t.Skip("temporary filesystem reports the same bytes for both tested normalization spellings")
}

func TestPlatformLegacyDirectoryEntryKeyClassifiesSensitiveFold(t *testing.T) {
	selection := pathSelection{anchorPath: "/Volumes/CaseSensitive/Project/State.json"}
	current := selectedAccessPath(selection)
	legacy := strings.ToLower(current)
	classified := platformLegacyDirectoryEntryKey(selection, current)
	if classified != legacy {
		t.Fatalf("legacy key = %q, want %q against %q", classified, legacy, current)
	}
}

func TestDarwinNativeCaseSensitiveDirectoryKeepsDistinctEntriesWhenAvailable(t *testing.T) {
	root := t.TempDir()
	mode, err := darwinDirectoryCaseSemantics(root)
	if err != nil {
		t.Fatal(err)
	}
	if mode != pathCaseSensitive {
		t.Skip("temporary directory is not on a case-sensitive filesystem; injected mixed-mode coverage remains active")
	}
	upper := filepath.Join(root, "Distinct")
	lower := filepath.Join(root, "distinct")
	if err := os.WriteFile(upper, []byte("upper"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lower, []byte("lower"), 0o600); err != nil {
		t.Fatal(err)
	}
	upperIdentity, err := canonicalPathIdentity(upper, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	lowerIdentity, err := canonicalPathIdentity(lower, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	if upperIdentity.keyPath == lowerIdentity.keyPath {
		t.Fatalf("case-sensitive entries coalesced as %q", upperIdentity.keyPath)
	}
}

func TestDarwinNativeMissingSuffixInheritsDeepestExistingDirectorySemantics(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Future", "Nested", "Leaf")
	identity, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := darwinDirectoryCaseSemantics(root)
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := filepath.Join("Future", "Nested", "Leaf")
	if mode == pathCaseInsensitive {
		wantSuffix = strings.ToLower(wantSuffix)
	}
	if !strings.HasSuffix(identity.keyPath, wantSuffix) {
		t.Fatalf("key = %q, want suffix %q", identity.keyPath, wantSuffix)
	}
}

func TestPersistedDirectoryEntryKeyMismatchDiagnosesLegacyDarwinFold(t *testing.T) {
	current := "/Volumes/CaseSensitive/Project/.daem/State.json"
	legacy := strings.ToLower(current)
	err := persistedDirectoryEntryKeyMismatch(current, legacy, true)
	if err == nil || !strings.Contains(err.Error(), "legacy Darwin-wide case fold") ||
		!strings.Contains(err.Error(), "docs/troubleshooting.md#legacy-darwin-path-authority") ||
		!strings.Contains(err.Error(), "do not edit or delete daem state manually") {
		t.Fatalf("legacy validation error = %v", err)
	}
}

func TestPersistedDirectoryEntryAuthorityRejectsZeroObservation(t *testing.T) {
	err := (PersistedDirectoryEntryAuthority{}).ValidatePersistedKey("/persisted")
	if err == nil || !strings.Contains(err.Error(), "current path authority key is required") {
		t.Fatalf("zero authority error = %v", err)
	}
}
