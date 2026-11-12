package carrierclaim

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func TestDecodeRegistryFactsDoesNotObserveOwnerKeys(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "not-a-directory")
	claim := testGlobalClaim(t, "alpha", "alpha@official", blocked)
	registry, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("blocks path traversal"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := DecodeRegistryFacts(content)
	if err != nil || !loaded.Equal(registry) {
		t.Fatal("carrier facts depended on current owner access", err)
	}
	reencoded, err := Marshal(loaded)
	if err != nil || !bytes.Equal(content, reencoded) {
		t.Fatal("facts decoding changed carrier facts", err)
	}

	path := filepath.Join(root, "claims.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(t.Context()); err == nil {
		t.Fatal("facts decoding removed the normal Store's live owner guard")
	}
}

func TestDecodeRegistryFactsKeepsCarrierVersionBoundaryAndPendingPinFacts(t *testing.T) {
	image, _ := testCarrierRegistryImage(t)
	content, err := Marshal(image.Registry())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := DecodeRegistryFacts(content)
	if err != nil || !loaded.Equal(image.Registry()) {
		t.Fatal("default facts lost a pending pin transition", err)
	}
	reencoded, err := Marshal(loaded)
	if err != nil || !bytes.Equal(reencoded, content) {
		t.Fatal("carrier facts changed default-format bytes", err)
	}
	if registry, err := DecodeRegistryFacts([]byte(`{"version":1,"claims":[]}`)); err != nil || !registry.Equal(durablecarrier.EmptyGlobalCarrierClaims()) {
		t.Fatal("exact empty retired facts were lost", err)
	}
	located, err := MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{
		located,
		[]byte(`{"version":2,"claims":[],"claims":[]}`),
		[]byte(`{"version":2,"claims":[],"unexpected":true}`),
		[]byte(`{"version":2,"claims":[]} {}`),
		[]byte(`{"version":2,"claims":null}`),
	} {
		registry, err := DecodeRegistryFacts(content)
		if err == nil || len(registry.Claims()) != 0 {
			t.Fatal("invalid or located bytes became usable legacy facts", err)
		}
	}
}

func TestRequiredReferenceIdentitiesIncludesPendingOwnerAndReturnsACopy(t *testing.T) {
	image, references := testCarrierRegistryImage(t)
	want := make([]pathauthority.Exact, 0, len(references))
	for _, reference := range references {
		want = append(want, reference.Identity())
	}
	slices.SortFunc(want, pathauthority.Exact.Compare)
	actual := RequiredReferenceIdentities(image.Registry())
	if !slices.Equal(actual, want) {
		t.Fatal("carrier reference census lost ordering, uniqueness or a pending owner", actual, want)
	}
	actual[0] = pathauthority.Exact{}
	if !slices.Equal(RequiredReferenceIdentities(image.Registry()), want) || len(RequiredReferenceIdentities(durablecarrier.EmptyGlobalCarrierClaims())) != 0 {
		t.Fatal("carrier reference census exposed mutable facts or invented empty owners")
	}
}
