package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

func TestProvisionalAcquireIntentPromotesOnlyAdmittedExactAddress(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, "skills")
	candidate := filepath.Join(namespace, "Caf\u00e9")
	intentPath, err := pathauthority.NewProvisional(
		candidate,
		pathtest.DarwinCaseSensitive(candidate).Witness(),
		namespace,
		pathtest.DarwinCaseSensitive(namespace).Witness(),
	)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("~/.agents/skills/Caf\u00e9")
	if err != nil {
		t.Fatal(err)
	}
	authority := mustAuthority(
		t,
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	intent, err := outputownership.NewProvisionalAcquireIntent(destination, "", intentPath, authority, "operation-1")
	if err != nil {
		t.Fatal(err)
	}

	exact := pathtest.DarwinCaseSensitive(filepath.Join(namespace, "Cafe\u0301"))
	address, err := outputownership.NewManagedAddress(exact, "")
	if err != nil {
		t.Fatal(err)
	}
	transition, err := NewAcquireTransitionFromIntent(intent, address)
	if err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}
	if transition.Kind() != TransitionAcquire || !transition.Address().Equal(address) {
		t.Fatalf("promoted transition = %#v", transition)
	}
}

func TestProvisionalAcquireIntentRejectsEscapedDepthAndContentPath(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, "skills")
	intentPath, err := pathauthority.NewProvisional(
		filepath.Join(namespace, "Future", "Caf\u00e9"),
		pathtest.DarwinCaseSensitive(filepath.Join(namespace, "Future", "Caf\u00e9")).Witness(),
		namespace,
		pathtest.DarwinCaseSensitive(namespace).Witness(),
	)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("~/.agents/skills/Future/Caf\u00e9")
	if err != nil {
		t.Fatal(err)
	}
	authority := mustAuthority(
		t,
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	intent, err := outputownership.NewProvisionalAcquireIntent(destination, "/alpha", intentPath, authority, "operation-1")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		path        string
		witness     string
		contentPath string
	}{
		{name: "escaped namespace", path: filepath.Join(root, "other", "Caf\u00e9"), contentPath: "/alpha"},
		{name: "changed depth", path: filepath.Join(namespace, "Caf\u00e9"), contentPath: "/alpha"},
		{name: "changed semantics", path: filepath.Join(namespace, "Future", "caf\u00e9"), contentPath: "/alpha"},
		{name: "changed content path", path: filepath.Join(namespace, "Future", "Caf\u00e9"), contentPath: "/beta"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			witness := pathtest.DarwinCaseSensitive(test.path).Witness()
			if test.name == "changed semantics" {
				witness = witness[:len(witness)-1] + "i"
			}
			exact, exactErr := pathauthority.NewExact(test.path, witness)
			if exactErr != nil {
				t.Fatal(exactErr)
			}
			address, addressErr := outputownership.NewManagedAddress(exact, test.contentPath)
			if addressErr != nil {
				t.Fatal(addressErr)
			}
			if _, promoteErr := NewAcquireTransitionFromIntent(intent, address); promoteErr == nil {
				t.Fatal("Promote accepted incompatible exact address")
			}
		})
	}
}
