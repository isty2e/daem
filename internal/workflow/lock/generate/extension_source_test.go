package generate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

func TestBuildLocksResolvedPiLocalCarrierIdentity(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["pi"]

[[extension]]
id = "tools"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "./packages/../packages/tools" }
`))
	if err != nil {
		t.Fatal(err)
	}
	environment, err = declarationmanifest.ResolveSelectedCarrierSources(paths, environment)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Build(context.Background(), Input{
		Paths:              paths,
		Environment:        environment,
		UsePersistentCache: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	subjects := result.Snapshot().Locked.Subjects()
	if len(subjects) != 1 {
		t.Fatalf("locked subjects = %d, want 1", len(subjects))
	}
	carrier, admitted, err := lock.DelegatedRelationCarrierKey(subjects[0])
	if err != nil {
		t.Fatal(err)
	}
	if !admitted {
		t.Fatal("locked Pi carrier was not admitted")
	}
	want := filepath.Join("packages", "tools")
	if got := carrier.Source().Ref(); got != want {
		t.Fatalf("locked Pi source = %q, want %q", got, want)
	}
	if !strings.Contains(string(result.Content()), want) {
		t.Fatalf("serialized lock does not contain canonical Pi source %q", want)
	}
}
