package authoring

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type unmanageTestFixture struct {
	extension desiredextension.Extension
	identity  durablecarrier.ManagedCarrierIdentity
	request   realizationdelegate.Request
}

func configureUnmanageTestHomes(t *testing.T, root string) {
	t.Helper()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
}

func unmanageManifest(source string, scope target.Scope) string {
	return `version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "` + string(scope) + `"
source = { marketplace = "` + source + `" }
`
}

func newUnmanageTestFixture(
	t *testing.T,
	name string,
	sourceValue string,
	scope target.Scope,
) unmanageTestFixture {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		sourceValue,
	)
	if err != nil {
		t.Fatal(err)
	}
	extension, err := desiredextension.New(desiredextension.Spec{
		Name:    name,
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   scope,
		Source:  source,
	})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := extensiontopology.Relation(extension)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(sourceValue)
	if err != nil {
		t.Fatal(err)
	}
	relation, err := hostrelation.Derive(extension.CarrierKey(), subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(extension.CarrierKey())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, relation)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(
		extension.ID(),
		extension.CarrierKey(),
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	return unmanageTestFixture{
		extension: extension,
		identity:  identity,
		request:   request,
	}
}

func unmanageTestPaths(t *testing.T, root string) daempaths.Paths {
	t.Helper()
	manifestPath := filepath.Join(root, "project", "daem.toml")
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func unmanageTestOwner(t *testing.T, paths daempaths.Paths) stateauthority.Authority {
	t.Helper()
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, statefileKey), paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func unmanageTestClaim(
	t *testing.T,
	fixture unmanageTestFixture,
	owner stateauthority.Authority,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	return unmanageTestClaimWithProvenance(
		t,
		fixture,
		owner,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
}

func unmanageTestClaimWithProvenance(
	t *testing.T,
	fixture unmanageTestFixture,
	owner stateauthority.Authority,
	provenance durablecarrier.ClaimProvenance,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		fixture.identity,
		fixture.request,
		provenance,
	)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func writeUnmanageState(t *testing.T, path string, snapshot durable.Snapshot) {
	t.Helper()
	content, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, path, content)
}

func writeUnmanageRegistry(
	t *testing.T,
	path string,
	registry durablecarrier.GlobalCarrierClaims,
) {
	t.Helper()
	content, err := carrierclaim.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, path, content)
}

func writeUnmanageFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readUnmanageFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func mustObservedPathAuthority(t *testing.T, path string) pathauthority.Exact {
	t.Helper()
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatalf("ObservePersistedDirectoryEntryAuthority(%q): %v", path, err)
	}
	return authority.Exact()
}
