package ownershiptest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
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
	owner, err := stateauthority.New(observeExact(t, paths.StatefilePath), paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := PermissionRecoveryFixture{Paths: paths, Owner: owner}
	var documents []ownership.ProtectedDocument
	var bindings []ownership.DocumentBinding
	for _, name := range names {
		destination, err := output.Parse(name)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("malformed host payload\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		binding, err := ownership.NewDocumentBinding(owner, destination, observeExact(t, path))
		if err != nil {
			t.Fatal(err)
		}
		document, err := ownership.NewProtectedDocument([]ownership.DocumentBinding{binding}, uint32(os.Getuid()), uint32(os.Getgid()), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
		documents = append(documents, document)
		fixture.Documents = append(fixture.Documents, path)
	}
	registry, err := ownership.NewRegistry(nil, bindings...)
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
	fixture.Store, err = ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
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
