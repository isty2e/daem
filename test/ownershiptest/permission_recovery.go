package ownershiptest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type PermissionRecoveryFixture struct {
	Paths      daempaths.Paths
	Owner      stateauthority.Authority
	Store      ownershipstore.Store
	Protection ownership.PendingProtection
	Documents  []string
}

func PermissionRecovery(t *testing.T, names ...string) PermissionRecoveryFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StatefilePath, []byte("malformed state payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	return ArmPermissionRecovery(t, paths, names...)
}

// ArmPermissionRecovery adds protection to an existing workspace without
// replacing its State, journal, or existing document contents.
func ArmPermissionRecovery(t *testing.T, paths daempaths.Paths, names ...string) PermissionRecoveryFixture {
	t.Helper()
	owner, err := stateauthority.New(observeExact(t, paths.StatefilePath), paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture := PermissionRecoveryFixture{Paths: paths, Owner: owner, Store: store}
	var documents []ownership.ProtectedDocument
	bindings := current.DocumentBindings()
	resolver := hostpath.NewResolverWithManagedDataRoot(paths.ManifestRoot, paths.DataDir)
	for _, name := range names {
		destination, err := output.Parse(name)
		if err != nil {
			t.Fatal(err)
		}
		path, err := resolver.Resolve(destination)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			if err := os.WriteFile(path, []byte("malformed host payload\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		physical := observeExact(t, path)
		cohort, err := current.DocumentBindingsAt(physical)
		if err != nil {
			t.Fatal(err)
		}
		if retained, present := current.DocumentBinding(owner.Key(), destination); present {
			if !retained.PathAuthority().Equal(physical) {
				t.Fatal("fixture document differs from its retained binding")
			}
		} else {
			binding, err := ownership.NewDocumentBinding(owner, destination, physical)
			if err != nil {
				t.Fatal(err)
			}
			bindings = append(bindings, binding)
			cohort = append(cohort, binding)
		}
		document, err := ownership.NewProtectedDocument(cohort, uint32(os.Getuid()), uint32(os.Getgid()), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		documents = append(documents, document)
		fixture.Documents = append(fixture.Documents, path)
	}
	registry, err := ownership.NewRegistryWithProtections(current.Claims(), bindings, current.PendingProtections())
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.OwnershipRegistryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.OwnershipRegistryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	invocation, _ := executionidentity.NewInvocation(strings.Repeat("a", 32))
	correlation, _ := ownership.NewProtectionCorrelation("permission-recover-test", strings.Repeat("b", 64), strings.Repeat("c", 64))
	fixture.Protection, err = ownership.NewArmedProtection(owner, invocation, correlation, documents)
	if err != nil {
		t.Fatal(err)
	}
	arm, err := ownership.NewArmProtection(fixture.Protection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Store.TransitionProtection(t.Context(), owner, arm); err != nil {
		t.Fatal(err)
	}
	return fixture
}
