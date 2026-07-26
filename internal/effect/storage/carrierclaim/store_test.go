package carrierclaim

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestStoreUpsertRoundTripsSharedClaimsAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, "carriers", "claims.json")
	store, err := New(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if current, err := store.Load(t.Context()); err != nil || len(current.Claims()) != 0 {
		t.Fatalf("missing registry = (%#v, %v)", current, err)
	}
	first := testGlobalClaim(t, "context7", "context7@official", filepath.Join(root, "first"))
	second := testGlobalClaim(t, "context7", "context7@official", filepath.Join(root, "second"))

	afterFirst, err := store.Upsert(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Upsert(t.Context(), first)
	if err != nil || !again.Equal(afterFirst) {
		t.Fatalf("idempotent upsert = (%#v, %v)", again, err)
	}
	afterSecond, err := store.Upsert(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterSecond.Claims()) != 2 {
		t.Fatalf("claims = %#v", afterSecond.Claims())
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Equal(afterSecond) {
		t.Fatalf("loaded registry = (%#v, %v)", loaded, err)
	}
	info, err := os.Stat(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("registry mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestStoreUpsertAllCommitsOneAtomicIdempotentBatch(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, "carriers", "claims.json")
	store, err := New(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	first := testGlobalClaim(t, "alpha", "alpha@official", filepath.Join(root, "first"))
	second := testGlobalClaim(t, "beta", "beta@official", filepath.Join(root, "second"))
	commits := 0
	commitFile := store.commitFile
	store.commitFile = func(ctx context.Context, request storagecommit.FileCommit) error {
		commits++
		return commitFile(ctx, request)
	}

	after, err := store.UpsertAll(
		t.Context(),
		[]durablecarrier.ManagedCarrierClaim{second, first},
	)
	if err != nil {
		t.Fatal(err)
	}
	if commits != 1 || len(after.Claims()) != 2 {
		t.Fatalf("batch result = (%#v, commits=%d)", after, commits)
	}
	again, err := store.UpsertAll(
		t.Context(),
		[]durablecarrier.ManagedCarrierClaim{first, second},
	)
	if err != nil || !again.Equal(after) || commits != 1 {
		t.Fatalf("idempotent batch = (%#v, %v, commits=%d)", again, err, commits)
	}
	if _, err := store.UpsertAll(
		t.Context(),
		[]durablecarrier.ManagedCarrierClaim{first, first},
	); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate batch error = %v", err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Equal(after) {
		t.Fatalf("failed batch changed registry = (%#v, %v)", loaded, err)
	}
}

func TestStoreUpsertAllIfCurrentRejectsStaleRegistryWithoutPartialBatch(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	retained := testGlobalClaim(t, "retained", "retained@official", filepath.Join(root, "retained"))
	concurrent := testGlobalClaim(t, "concurrent", "concurrent@official", filepath.Join(root, "concurrent"))
	adopted := testGlobalClaimWithProvenance(
		t,
		"adopted",
		"adopted@official",
		filepath.Join(root, "adopted"),
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	expected, err := store.Upsert(t.Context(), retained)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := store.Upsert(t.Context(), concurrent)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.UpsertAllIfCurrent(
		t.Context(),
		expected,
		[]durablecarrier.ManagedCarrierClaim{adopted},
	); err == nil || !strings.Contains(err.Error(), "changed since confirmed observation") {
		t.Fatalf("UpsertAllIfCurrent error = %v, want stale registry rejection", err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Equal(actual) {
		t.Fatalf("stale adoption changed registry: got %#v want %#v", loaded.Claims(), actual.Claims())
	}
}

func TestRegistryCodecRoundTripsBothClaimProvenancesWithoutVersionChange(t *testing.T) {
	root := t.TempDir()
	installed := testGlobalClaim(t, "installed", "installed@official", filepath.Join(root, "installed"))
	adopted := testGlobalClaimWithProvenance(
		t,
		"adopted",
		"adopted@official",
		filepath.Join(root, "adopted"),
		durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved,
	)
	registry, err := durablecarrier.NewGlobalCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{installed, adopted},
	)
	if err != nil {
		t.Fatal(err)
	}

	content, err := encode(registry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"version": 1`) ||
		!strings.Contains(string(content), string(durablecarrier.ClaimProvenanceInstalledObserved)) ||
		!strings.Contains(string(content), string(durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved)) {
		t.Fatalf("registry encoding omitted version or provenance:\n%s", content)
	}
	decoded, err := decode(content)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(registry) {
		t.Fatal("registry codec changed installed or adopted claim")
	}
	reencoded, err := encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != string(content) {
		t.Fatal("registry provenance encoding is not deterministic")
	}
}

func TestRegistryCodecRejectsUnknownCorruptDuplicateAndProjectClaims(t *testing.T) {
	root := t.TempDir()
	global := testGlobalClaim(t, "context7", "context7@official", root)
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{global})
	if err != nil {
		t.Fatal(err)
	}
	content, err := encode(registry)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "unknown nested field",
			content: strings.Replace(
				string(content),
				`"carrier_family": "claude-code-plugin",`,
				`"carrier_family": "claude-code-plugin", "remove_route": "invented",`,
				1,
			),
			want: "unknown field",
		},
		{
			name: "forged carrier subject",
			content: strings.Replace(
				string(content),
				`"namespace": "claude-code.plugin-carrier"`,
				`"namespace": "codex.plugin-carrier"`,
				1,
			),
			want: "carrier_subject does not match",
		},
		{
			name: "unsupported provenance",
			content: strings.Replace(
				string(content),
				string(durablecarrier.ClaimProvenanceInstalledObserved),
				"attempt_succeeded",
				1,
			),
			want: "unsupported managed carrier claim provenance",
		},
		{
			name: "duplicate key",
			content: strings.Replace(
				string(content),
				`"version": 1`,
				`"version": 1, "version": 1`,
				1,
			),
			want: "duplicate object key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.content == string(content) {
				t.Fatalf("fixture mutation %q did not change content", test.name)
			}
			if _, err := decode([]byte(test.content)); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStoreRejectsAuthorityExposingPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"claims":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(t.Context()); err == nil ||
		!strings.Contains(err.Error(), "permissions") {
		t.Fatalf("Load error = %v", err)
	}
}

func TestStoreRemoveRetainsOtherConsumers(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	first := testGlobalClaim(t, "context7", "context7@official", filepath.Join(root, "first"))
	second := testGlobalClaim(t, "context7", "context7@official", filepath.Join(root, "second"))
	if _, err := store.Upsert(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(t.Context(), second); err != nil {
		t.Fatal(err)
	}

	remaining, err := store.Remove(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	if claims := remaining.Claims(); len(claims) != 1 || !claims[0].ExactEqual(second) {
		t.Fatalf("remaining claims = %#v", claims)
	}
	again, err := store.Remove(t.Context(), first)
	if err != nil || !again.Equal(remaining) {
		t.Fatalf("idempotent Remove = (%#v, %v)", again, err)
	}
}

func TestStoreStaleLastConsumerCommitCannotEraseConcurrentClaim(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	first := testGlobalClaim(t, "context7", "context7@official", filepath.Join(root, "first"))
	second := testGlobalClaim(t, "context7", "context7@official", filepath.Join(root, "second"))
	if _, err := store.Upsert(t.Context(), first); err != nil {
		t.Fatal(err)
	}

	stale, staleIdentity, exists, err := store.loadForCommit(t.Context())
	if err != nil || !exists {
		t.Fatalf("load stale registry = (%#v, %#v, %t, %v)", stale, staleIdentity, exists, err)
	}
	staleEmpty, changed, err := stale.WithoutClaim(first)
	if err != nil || !changed {
		t.Fatalf("derive stale last-consumer transition = (%#v, %t, %v)", staleEmpty, changed, err)
	}
	if _, err := store.Upsert(t.Context(), second); err != nil {
		t.Fatal(err)
	}

	if err := store.commitRegistry(t.Context(), staleEmpty, staleIdentity, true); err == nil {
		t.Fatal("stale last-consumer commit succeeded")
	}
	current, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Claims()) != 2 {
		t.Fatalf("stale commit erased concurrent claim: %#v", current.Claims())
	}
}

func TestStoreIndeterminatePostCommitErrorIsIdempotentlyRecoverable(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	claim := testGlobalClaim(t, "context7", "context7@official", root)
	commitFile := store.commitFile
	sentinel := errors.New("injected post-commit failure")
	store.commitFile = func(ctx context.Context, request storagecommit.FileCommit) error {
		if err := commitFile(ctx, request); err != nil {
			return err
		}
		return sentinel
	}
	if _, err := store.Upsert(t.Context(), claim); !errors.Is(err, sentinel) {
		t.Fatalf("Upsert error = %v, want injected failure", err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil || len(loaded.Claims()) != 1 || !loaded.Claims()[0].ExactEqual(claim) {
		t.Fatalf("post-error registry = (%#v, %v)", loaded, err)
	}
	store.commitFile = commitFile
	retried, err := store.Upsert(t.Context(), claim)
	if err != nil || !retried.Equal(loaded) {
		t.Fatalf("idempotent retry = (%#v, %v)", retried, err)
	}
}

func TestRegistryCodecIsBoundedCanonicalAndRejectsDuplicateSemanticClaims(t *testing.T) {
	root := t.TempDir()
	first := testGlobalClaim(t, "context7", "context7@official", filepath.Join(root, "first"))
	second := testGlobalClaim(t, "context7", "context7@official", filepath.Join(root, "second"))
	forward, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{first, second})
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{second, first})
	if err != nil {
		t.Fatal(err)
	}
	forwardContent, err := encode(forward)
	if err != nil {
		t.Fatal(err)
	}
	reverseContent, err := encode(reverse)
	if err != nil {
		t.Fatal(err)
	}
	if string(forwardContent) != string(reverseContent) {
		t.Fatal("canonical registry encoding depends on insertion order")
	}
	for _, forbidden := range []string{"stdout", "stderr", "token", "password", "remove_route"} {
		if strings.Contains(string(forwardContent), forbidden) {
			t.Fatalf("registry contains forbidden field %q", forbidden)
		}
	}

	var persisted registryDTO
	if err := json.Unmarshal(forwardContent, &persisted); err != nil {
		t.Fatal(err)
	}
	persisted.Claims = append(persisted.Claims, persisted.Claims[0])
	duplicate, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(duplicate); err == nil ||
		!strings.Contains(err.Error(), "duplicates one owner relation") {
		t.Fatalf("duplicate semantic claim error = %v", err)
	}
	if _, err := decode(make([]byte, maximumRegistryBytes+1)); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized registry error = %v", err)
	}
	for _, malformed := range []string{
		`{"version":2,"claims":[]}`,
		`{"version":1}`,
		`{"version":1,"claims":[]} {}`,
	} {
		if _, err := decode([]byte(malformed)); err == nil {
			t.Fatalf("malformed registry decoded: %s", malformed)
		}
	}
}

func testGlobalClaim(
	t *testing.T,
	name string,
	sourceValue string,
	authorityRoot string,
) durablecarrier.ManagedCarrierClaim {
	return testGlobalClaimWithProvenance(
		t,
		name,
		sourceValue,
		authorityRoot,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
}

func testGlobalClaimWithProvenance(
	t *testing.T,
	name string,
	sourceValue string,
	authorityRoot string,
	provenance durablecarrier.ClaimProvenance,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		sourceValue,
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    name,
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeGlobal,
		Source:  source,
	})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(sourceValue)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(
		value.ID(),
		value.CarrierKey(),
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := durablecarrier.NewStateAuthority(
		filepath.Join(authorityRoot, ".daem", "state.json"),
		filepath.Join(authorityRoot, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		request,
		provenance,
	)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}
