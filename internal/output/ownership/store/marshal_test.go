package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestMarshalVersion4ClaimAndBindingBytes(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "settings.json")
	statefile := filepath.Join(root, "state.json")
	manifest := filepath.Join(root, "daem.toml")
	owner, err := stateauthority.New(pathtest.Exact(statefile), manifest)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddressFromLegacy(pathtest.Exact(document), "")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	binding := storedDocumentBinding(t, claim, ".pi/settings.json")
	registry, err := ownership.NewRegistryWithProtections([]ownership.Claim{claim}, []ownership.DocumentBinding{binding}, nil)
	if err != nil {
		t.Fatal(err)
	}
	quotedDocument, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	quotedStatefile, err := json.Marshal(statefile)
	if err != nil {
		t.Fatal(err)
	}
	quotedManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf(`{
  "version": 4,
  "claims": [
    {
      "path_authority": {
        "key": %s,
        "semantics_witness": "exact-v1:"
      },
      "footprint": {
        "kind": "path"
      },
      "statefile_authority": {
        "key": %s,
        "semantics_witness": "exact-v1:"
      },
      "manifest_path": %s,
      "state": "active"
    }
  ],
  "document_bindings": [
    {
      "destination": ".pi/settings.json",
      "path_authority": {
        "key": %s,
        "semantics_witness": "exact-v1:"
      },
      "statefile_authority": {
        "key": %s,
        "semantics_witness": "exact-v1:"
      },
      "manifest_path": %s
    }
  ]
}
`, quotedDocument, quotedStatefile, quotedManifest, quotedDocument, quotedStatefile, quotedManifest)
	content, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != expected {
		t.Fatalf("version4 bytes changed:\n%s", content)
	}
}

func TestMarshalPreservesClaimsAndRejectsUnreadableSize(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat(strings.Repeat("d", 199)+string(filepath.Separator), 15))
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claims := make([]ownership.Claim, 2800)
	for index := range claims {
		address, err := ownership.NewManagedAddressFromLegacy(pathtest.Exact(filepath.Join(root, fmt.Sprintf("output-%04d", index))), "")
		if err != nil {
			t.Fatal(err)
		}
		claims[index], err = ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
	}

	one, err := ownership.NewRegistry(claims[:1])
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(one)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decode(content)
	if err != nil {
		t.Fatal(err)
	}
	if claim, found := decoded.Exact(claims[0].Address()); !found || !claim.Equal(claims[0]) {
		t.Fatal("marshal changed claim facts")
	}

	large, err := ownership.NewRegistry(claims)
	if err != nil {
		t.Fatal(err)
	}
	if content, err := Marshal(large); err == nil || content != nil {
		t.Fatal("marshal admitted a registry exceeding its loader limit")
	}
}
