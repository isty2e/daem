package mutation

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOperationFingerprintOwnsCanonicalBytesOnly(t *testing.T) {
	left := NewOperationFingerprint([]byte("plan-a"))
	same := NewOperationFingerprint([]byte("plan-a"))
	different := NewOperationFingerprint([]byte("plan-b"))
	if !left.Equal(same) {
		t.Fatal("equal canonical plans produced different fingerprints")
	}
	if left.Equal(different) {
		t.Fatal("different canonical plans produced equal fingerprints")
	}
	if (OperationFingerprint{}).Equal(OperationFingerprint{}) {
		t.Fatal("zero fingerprints compared equal")
	}
}

func TestDomainConstructorsRejectInvalidFacts(t *testing.T) {
	tests := []struct {
		name  string
		build func() error
	}{
		{
			name: "empty logical path",
			build: func() error {
				_, err := NewLogicalPathDomain(LogicalPathRequest{Access: AccessExclusive, Effect: PathEffectDirectoryEntry})
				return err
			},
		},
		{
			name: "invalid access",
			build: func() error {
				_, err := NewLogicalPathDomain(LogicalPathRequest{Path: t.TempDir(), Effect: PathEffectDirectoryEntry})
				return err
			},
		},
		{
			name: "path NUL",
			build: func() error {
				_, err := NewLogicalPathDomain(LogicalPathRequest{Path: "bad\x00path", Access: AccessExclusive, Effect: PathEffectDirectoryEntry})
				return err
			},
		},
		{
			name: "missing physical target",
			build: func() error {
				_, err := NewPhysicalPathDomain(PhysicalPathRequest{Path: t.TempDir(), Access: AccessShared, Effect: PathEffectReferent, Scope: "project"})
				return err
			},
		},
		{
			name: "invalid route containment",
			build: func() error {
				_, err := NewHostRouteDomain(HostRouteRequest{Target: "codex", Scope: "global", Family: "plugin"})
				return err
			},
		},
		{
			name: "route NUL",
			build: func() error {
				_, err := NewHostRouteDomain(HostRouteRequest{Target: "codex", Scope: "global", Family: "plugin\x00install", Containment: RouteContainmentUnknown})
				return err
			},
		},
		{
			name: "route surrounding whitespace",
			build: func() error {
				_, err := NewHostRouteDomain(HostRouteRequest{Target: " codex", Scope: "global", Family: "plugin", Containment: RouteContainmentUnknown})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.build(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDirectoryEntryAndReferentCanonicalizationStayDistinct(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	writeMutationTestFile(t, target, "target", 0o600)
	link := filepath.Join(root, "link")
	if err := symlinkForMutationTest(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	entry, err := NewLogicalPathDomain(LogicalPathRequest{Path: link, Access: AccessExclusive, Effect: PathEffectDirectoryEntry})
	if err != nil {
		t.Fatalf("NewLogicalPathDomain(entry) error: %v", err)
	}
	referent, err := NewLogicalPathDomain(LogicalPathRequest{Path: link, Access: AccessExclusive, Effect: PathEffectReferent})
	if err != nil {
		t.Fatalf("NewLogicalPathDomain(referent) error: %v", err)
	}
	if entry.canonicalPath == referent.canonicalPath {
		t.Fatalf("entry and referent canonical paths both %q", entry.canonicalPath)
	}
}

func TestSymlinkedAncestorsCanonicalizeToOnePath(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := mkdirMutationTest(realParent); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(root, "alias")
	if err := symlinkForMutationTest(realParent, aliasParent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	realDomain, err := NewLogicalPathDomain(LogicalPathRequest{Path: filepath.Join(realParent, "future"), Access: AccessExclusive, Effect: PathEffectDirectoryEntry})
	if err != nil {
		t.Fatal(err)
	}
	aliasDomain, err := NewLogicalPathDomain(LogicalPathRequest{Path: filepath.Join(aliasParent, "future"), Access: AccessExclusive, Effect: PathEffectDirectoryEntry})
	if err != nil {
		t.Fatal(err)
	}
	if realDomain.canonicalPath != aliasDomain.canonicalPath {
		t.Fatalf("canonical paths differ: %q != %q", realDomain.canonicalPath, aliasDomain.canonicalPath)
	}
}

func TestCanonicalDirectoryEntryPathResolvesAncestorsButRetainsFinalSymlink(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("create real parent: %v", err)
	}
	aliasParent := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}
	finalTarget := filepath.Join(realParent, "target")
	if err := os.WriteFile(finalTarget, []byte("target"), 0o600); err != nil {
		t.Fatalf("write final target: %v", err)
	}
	finalAlias := filepath.Join(aliasParent, "entry")
	if err := os.Symlink(finalTarget, finalAlias); err != nil {
		t.Fatalf("create final symlink: %v", err)
	}

	got, err := CanonicalDirectoryEntryPath(finalAlias)
	if err != nil {
		t.Fatalf("CanonicalDirectoryEntryPath returned error: %v", err)
	}
	canonicalRealParent, err := filepath.EvalSymlinks(realParent)
	if err != nil {
		t.Fatalf("canonicalize real parent: %v", err)
	}
	want := filepath.Join(canonicalRealParent, "entry")
	if got != want {
		t.Fatalf("CanonicalDirectoryEntryPath = %q, want %q", got, want)
	}
}

func TestCanonicalDirectoryEntryKeyMatchesDomainIdentity(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.MkdirAll(realParent, 0o755); err != nil {
		t.Fatalf("create real parent: %v", err)
	}
	aliasParent := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	key, err := CanonicalDirectoryEntryKey(filepath.Join(aliasParent, "state.json"))
	if err != nil {
		t.Fatalf("CanonicalDirectoryEntryKey returned error: %v", err)
	}
	canonicalParent, err := filepath.EvalSymlinks(realParent)
	if err != nil {
		t.Fatalf("canonicalize real parent: %v", err)
	}
	want := normalizePlatformPathKey(filepath.Join(canonicalParent, "state.json"))
	if key != want {
		t.Fatalf("CanonicalDirectoryEntryKey = %q, want %q", key, want)
	}
}

func TestPlatformPathKeyConservativelyCoalescesCaseAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FutureName")
	got := normalizePlatformPathKey(path)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		if got == path {
			t.Fatalf("case-conservative platform key did not normalize %q", path)
		}
		return
	}
	if got != path {
		t.Fatalf("case-sensitive platform key = %q, want %q", got, path)
	}
}
