package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestMarshalPreservesClaimsAndRejectsUnreadableSize(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat(strings.Repeat("d", 199)+string(filepath.Separator), 15))
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claims := make([]ownership.Claim, 2800)
	for index := range claims {
		address, err := ownership.NewManagedAddress(pathtest.Exact(filepath.Join(root, fmt.Sprintf("output-%04d", index))), "")
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
