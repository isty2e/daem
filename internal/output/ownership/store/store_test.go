package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestStoreRoundTripUsesPrivateDeterministicSchema(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "data", "ownership", "claims.json"))
	claim := testActiveClaim(t, root, "left", filepath.Join(root, "host", "AGENTS.md"), "")
	replacement, _ := ownership.PresentClaim(claim)

	written, err := registryStore.Apply(context.Background(), claim.Address(), ownership.NoClaim(), replacement)
	if err != nil {
		t.Fatalf("Store.Apply returned error: %v", err)
	}
	loaded, err := registryStore.Load(context.Background())
	if err != nil {
		t.Fatalf("Store.Load returned error: %v", err)
	}
	if got, ok := loaded.Exact(claim.Address()); !ok || !got.Equal(claim) {
		t.Fatal("loaded registry omitted the written claim")
	}
	if got, ok := written.Exact(claim.Address()); !ok || !got.Equal(claim) {
		t.Fatal("returned registry omitted the written claim")
	}
	info, err := os.Lstat(registryStore.Path())
	if err != nil {
		t.Fatalf("inspect registry: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode = %04o, want 0600", info.Mode().Perm())
	}
	first, _ := os.ReadFile(registryStore.Path())
	for _, fragment := range []string{`"version": 2`, `"path_authority"`, `"statefile_authority"`, `"semantics_witness"`} {
		if !strings.Contains(string(first), fragment) {
			t.Fatalf("registry missing current authority field %q:\n%s", fragment, first)
		}
	}
	for _, forbidden := range []string{`"path":`, `"statefile_key":`} {
		if strings.Contains(string(first), forbidden) {
			t.Fatalf("registry retained legacy authority field %q:\n%s", forbidden, first)
		}
	}
	if _, err := registryStore.Apply(context.Background(), claim.Address(), replacement, replacement); err != nil {
		t.Fatalf("idempotent Store.Apply returned error: %v", err)
	}
	second, _ := os.ReadFile(registryStore.Path())
	if string(first) != string(second) {
		t.Fatalf("idempotent registry bytes changed:\nfirst=%s\nsecond=%s", first, second)
	}
}

func TestStoreLoadRejectsStaleFilesystemSemanticsWitness(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "data", "ownership", "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "host", "AGENTS.md"), "")
	replacement, _ := ownership.PresentClaim(claim)
	if _, err := registryStore.Apply(
		context.Background(),
		claim.Address(),
		ownership.NoClaim(),
		replacement,
	); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(registryStore.Path())
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*claimRecord)
	}{
		{
			name: "managed path",
			mutate: func(record *claimRecord) {
				record.PathAuthority.Witness = alternateSemanticsWitness(
					record.PathAuthority.Key,
					record.PathAuthority.Witness,
				)
			},
		},
		{
			name: "owner statefile",
			mutate: func(record *claimRecord) {
				record.StatefileAuthority.Witness = alternateSemanticsWitness(
					record.StatefileAuthority.Key,
					record.StatefileAuthority.Witness,
				)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var persisted file
			if err := json.Unmarshal(content, &persisted); err != nil {
				t.Fatal(err)
			}
			test.mutate(&persisted.Claims[0])
			mutated, err := json.MarshalIndent(persisted, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(registryStore.Path(), mutated, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := registryStore.Load(context.Background()); err == nil ||
				!strings.Contains(err.Error(), "is not current") {
				t.Fatalf("Store.Load error = %v, want stale semantics refusal", err)
			}
		})
	}
}

func TestStorePreservesUnrelatedClaimsAndRejectsStaleExpected(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "claims.json"))
	left := testActiveClaim(t, root, "left", filepath.Join(root, "host", "left"), "")
	right := testActiveClaim(t, root, "right", filepath.Join(root, "host", "right"), "")
	leftValue, _ := ownership.PresentClaim(left)
	rightValue, _ := ownership.PresentClaim(right)
	if _, err := registryStore.Apply(context.Background(), left.Address(), ownership.NoClaim(), leftValue); err != nil {
		t.Fatalf("write left claim: %v", err)
	}
	if _, err := registryStore.Apply(context.Background(), right.Address(), ownership.NoClaim(), rightValue); err != nil {
		t.Fatalf("write right claim: %v", err)
	}

	foreign := testActiveClaim(t, root, "foreign", left.Address().Path(), left.Address().ContentPath())
	foreignValue, _ := ownership.PresentClaim(foreign)
	if _, err := registryStore.Apply(context.Background(), left.Address(), foreignValue, ownership.NoClaim()); err == nil {
		t.Fatal("Store.Apply accepted stale expected ownership")
	} else {
		var stale *ownership.StaleClaimError
		if !errors.As(err, &stale) {
			t.Fatalf("error = %T, want *ownership.StaleClaimError", err)
		}
	}
	loaded, err := registryStore.Load(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	for _, claim := range []ownership.Claim{left, right} {
		if got, ok := loaded.Exact(claim.Address()); !ok || !got.Equal(claim) {
			t.Fatalf("registry lost claim for %q", claim.Address().Path())
		}
	}
}

func TestStoreRejectsMalformedOrExposedRegistry(t *testing.T) {
	root := canonicalTestRoot(t)
	path := filepath.Join(root, "claims.json")
	registryStore := mustStore(t, path)
	tests := []struct {
		name    string
		content string
	}{
		{name: "unknown field", content: `{"version":2,"claims":[],"future":true}`},
		{name: "populated legacy version", content: `{"version":1,"claims":[{"path":"legacy"}]}`},
		{name: "missing claims", content: `{"version":2}`},
		{name: "multiple values", content: `{"version":2,"claims":[]} {"version":2,"claims":[]}`},
		{
			name: "missing path witness",
			content: fmt.Sprintf(
				`{"version":2,"claims":[{"path_authority":{"key":%q},"statefile_authority":{"key":%q,"semantics_witness":"exact-v1:"},"manifest_path":%q,"state":"active"}]}`,
				filepath.Join(root, "host"),
				filepath.Join(root, "state.json"),
				filepath.Join(root, "daem.toml"),
			),
		},
		{
			name: "unknown statefile witness",
			content: fmt.Sprintf(
				`{"version":2,"claims":[{"path_authority":{"key":%q,"semantics_witness":"exact-v1:"},"statefile_authority":{"key":%q,"semantics_witness":"future-v1:"},"manifest_path":%q,"state":"active"}]}`,
				filepath.Join(root, "host"),
				filepath.Join(root, "state.json"),
				filepath.Join(root, "daem.toml"),
			),
		},
		{
			name: "active operation",
			content: fmt.Sprintf(
				`{"version":2,"claims":[{"path_authority":{"key":%q,"semantics_witness":"exact-v1:"},"statefile_authority":{"key":%q,"semantics_witness":"exact-v1:"},"manifest_path":%q,"state":"active","operation_id":"op"}]}`,
				filepath.Join(root, "host"),
				filepath.Join(root, "state.json"),
				filepath.Join(root, "daem.toml"),
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if _, err := registryStore.Load(context.Background()); err == nil {
				t.Fatal("Store.Load returned nil error")
			}
		})
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"claims":[]}`), 0o644); err != nil {
		t.Fatalf("write exposed fixture: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("expose registry mode: %v", err)
	}
	if _, err := registryStore.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("Store.Load exposed mode error = %v", err)
	}
}

func TestStoreAdmitsOnlyExactEmptyRetiredV1Registry(t *testing.T) {
	root := canonicalTestRoot(t)
	path := filepath.Join(root, "claims.json")
	registryStore := mustStore(t, path)
	const retiredEmpty = `{"version":1,"claims":[]}`
	if err := os.WriteFile(path, []byte(retiredEmpty), 0o600); err != nil {
		t.Fatal(err)
	}

	registry, err := registryStore.Load(t.Context())
	if err != nil {
		t.Fatalf("Load exact empty retired v1: %v", err)
	}
	if claims := registry.Claims(); len(claims) != 0 {
		t.Fatalf("retired registry claims = %#v, want empty", claims)
	}
	retained, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(retained) != retiredEmpty {
		t.Fatalf("read-only Load rewrote retired ownership registry:\n%s", retained)
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "missing claims", content: `{"version":1}`, want: "exact empty retired ownership registry"},
		{name: "null claims", content: `{"version":1,"claims":null}`, want: "exact empty retired ownership registry"},
		{
			name:    "populated legacy claim",
			content: `{"version":1,"claims":[{"path":"/tmp/output","statefile_key":"/tmp/state.json"}]}`,
			want:    "use the daem version that wrote it",
		},
		{name: "unknown legacy field", content: `{"version":1,"claims":[],"future":true}`, want: "unknown field"},
		{name: "future version", content: `{"version":3,"claims":[],"future":true}`, want: "written by a newer daem"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := registryStore.Load(t.Context())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want %q", err, test.want)
			}
			if test.name == "populated legacy claim" && strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("populated legacy claim was misclassified as current schema: %v", err)
			}
		})
	}

	if err := os.WriteFile(path, []byte(retiredEmpty), 0o600); err != nil {
		t.Fatal(err)
	}
	claim := testActiveClaim(t, root, "current", filepath.Join(root, "AGENTS.md"), "")
	replacement, _ := ownership.PresentClaim(claim)
	if _, err := registryStore.Apply(t.Context(), claim.Address(), ownership.NoClaim(), replacement); err != nil {
		t.Fatalf("Apply after retired empty registry: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), `"version": 2`) {
		t.Fatalf("guarded write did not replace retired schema with current schema:\n%s", current)
	}
}

func TestStoreRejectsAmbiguousAndOversizedRegistry(t *testing.T) {
	root := canonicalTestRoot(t)
	path := filepath.Join(root, "claims.json")
	registryStore := mustStore(t, path)
	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{
			name:    "duplicate authority key",
			content: []byte(`{"version":2,"claims":[],"claims":[]}`),
			want:    "duplicate object key",
		},
		{
			name:    "invalid UTF-8",
			content: []byte{'{', '"', 'v', 'e', 'r', 's', 'i', 'o', 'n', '"', ':', 0xff, '}'},
			want:    "valid UTF-8",
		},
		{
			name: "excessive depth",
			content: []byte(strings.Repeat("[", maximumOwnershipRegistryJSONDepth+2) +
				"0" + strings.Repeat("]", maximumOwnershipRegistryJSONDepth+2)),
			want: "maximum depth",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatalf("secure fixture mode: %v", err)
			}
			if _, err := registryStore.Load(context.Background()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Store.Load error = %v, want %q", err, test.want)
			}
		})
	}

	oversized := make([]byte, maximumOwnershipRegistryBytes+1)
	if err := os.WriteFile(path, oversized, 0o600); err != nil {
		t.Fatalf("write oversized fixture: %v", err)
	}
	if _, err := registryStore.Load(context.Background()); err == nil {
		t.Fatal("Store.Load accepted oversized registry")
	}
}

func TestStoreCanceledContextHasNoEffect(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "claims.json"))
	claim := testActiveClaim(t, root, "left", filepath.Join(root, "host"), "")
	value, _ := ownership.PresentClaim(claim)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := registryStore.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Store.Load error = %v, want context.Canceled", err)
	}
	if _, err := registryStore.Apply(ctx, claim.Address(), ownership.NoClaim(), value); !errors.Is(err, context.Canceled) {
		t.Fatalf("Store.Apply error = %v, want context.Canceled", err)
	}
	if _, err := os.Lstat(registryStore.Path()); !os.IsNotExist(err) {
		t.Fatalf("registry exists after canceled apply: %v", err)
	}
}

func TestNewCanonicalizesSymlinkedRegistryAncestor(t *testing.T) {
	root := canonicalTestRoot(t)
	realData := filepath.Join(root, "real-data")
	if err := os.MkdirAll(realData, 0o700); err != nil {
		t.Fatalf("create real data root: %v", err)
	}
	aliasData := filepath.Join(root, "alias-data")
	if err := os.Symlink(realData, aliasData); err != nil {
		t.Skipf("create data-root symlink: %v", err)
	}
	registryStore := mustStore(t, filepath.Join(aliasData, "ownership", "claims.json"))
	want := filepath.Join(realData, "ownership", "claims.json")
	if registryStore.Path() != want {
		t.Fatalf("Store.Path = %q, want %q", registryStore.Path(), want)
	}
}

func TestStoreCrossProcessAbsentAcquisitionHasOneWinner(t *testing.T) {
	root := canonicalTestRoot(t)
	path := filepath.Join(root, "claims.json")
	start := filepath.Join(root, "start")
	commands := make([]*exec.Cmd, 0, 2)
	for _, owner := range []string{"left", "right"} {
		command := exec.Command(os.Args[0], "-test.run=TestOwnershipRegistryHelperProcess", "--", path, root, owner, start)
		command.Env = append(os.Environ(), "DAEM_OWNERSHIP_REGISTRY_HELPER=1")
		if err := command.Start(); err != nil {
			t.Fatalf("start helper: %v", err)
		}
		commands = append(commands, command)
	}
	if err := os.WriteFile(start, []byte("go"), 0o600); err != nil {
		t.Fatalf("release helpers: %v", err)
	}
	successes := 0
	for _, command := range commands {
		if err := command.Wait(); err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful acquisitions = %d, want 1", successes)
	}
	registryStore := mustStore(t, path)
	loaded, err := registryStore.Load(context.Background())
	if err != nil {
		t.Fatalf("load winner: %v", err)
	}
	if len(loaded.Claims()) != 1 {
		t.Fatalf("claim count = %d, want 1", len(loaded.Claims()))
	}
}

func TestOwnershipRegistryHelperProcess(t *testing.T) {
	if os.Getenv("DAEM_OWNERSHIP_REGISTRY_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) != separator+5 {
		os.Exit(10)
	}
	path, root, owner, start := os.Args[separator+1], os.Args[separator+2], os.Args[separator+3], os.Args[separator+4]
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(start); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(11)
		}
		time.Sleep(time.Millisecond)
	}
	registryStore, err := New(path)
	if err != nil {
		os.Exit(12)
	}
	claim := testActiveClaim(t, root, owner, filepath.Join(root, "host", "AGENTS.md"), "")
	value, _ := ownership.PresentClaim(claim)
	if _, err := registryStore.Apply(context.Background(), claim.Address(), ownership.NoClaim(), value); err != nil {
		os.Exit(13)
	}
	os.Exit(0)
}

func mustStore(t *testing.T, path string) Store {
	t.Helper()
	registryStore, err := New(filepath.Clean(path))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return registryStore
}

func canonicalTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonicalize test root: %v", err)
	}
	return root
}

func testActiveClaim(t *testing.T, root string, ownerName string, path string, contentPath string) ownership.Claim {
	t.Helper()
	pathAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Clean(path))
	if err != nil {
		t.Fatalf("observe managed address authority: %v", err)
	}
	address, err := ownership.NewManagedAddress(pathAuthority.Exact(), contentPath)
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	statefileAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(
		filepath.Join(root, ownerName, ".daem", "state.json"),
	)
	if err != nil {
		t.Fatalf("observe statefile authority: %v", err)
	}
	authority, err := stateauthority.New(
		statefileAuthority.Exact(),
		filepath.Join(root, ownerName, "daem.toml"),
	)
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	claim, err := ownership.NewActiveClaim(address, authority)
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}
	return claim
}

func alternateSemanticsWitness(key string, current string) string {
	if current != "exact-v1:" {
		return pathtest.Exact(key).Witness()
	}
	return pathtest.DarwinCaseSensitive(key).Witness()
}
