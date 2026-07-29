package generate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
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

[[extension]]
id = "review"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "./packages/review" }
`))
	if err != nil {
		t.Fatal(err)
	}
	environment, err = declarationmanifest.ResolveSelectedCarrierSources(paths, environment)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Build(context.Background(), Input{
		Paths:                  paths,
		Environment:            environment,
		UsePersistentCache:     false,
		ExtensionOrderIdentity: aggregatecodec.ExtensionOrderIdentityResolver(paths),
	})
	if err != nil {
		t.Fatal(err)
	}
	subjects := result.Snapshot().Locked.Subjects()
	if len(subjects) != 2 {
		t.Fatalf("locked subjects = %d, want 2", len(subjects))
	}
	var toolsSource string
	for _, subject := range subjects {
		carrier, admitted, err := lock.DelegatedRelationCarrierKey(subject)
		if err != nil {
			t.Fatal(err)
		}
		if !admitted {
			t.Fatal("locked Pi carrier was not admitted")
		}
		if subject.EntityID().Name() == "tools" {
			toolsSource = carrier.Source().Ref()
		}
	}
	want := filepath.Join("packages", "tools")
	if got := toolsSource; got != want {
		t.Fatalf("locked Pi source = %q, want %q", got, want)
	}
	if !strings.Contains(string(result.Content()), want) {
		t.Fatalf("serialized lock does not contain canonical Pi source %q", want)
	}
	constraints := result.Snapshot().Locked.OrderConstraints()
	if len(constraints) != 1 {
		t.Fatalf("order constraints = %#v, want one", constraints)
	}
	members := constraints[0].Members()
	wantIdentity := "local:project:" + filepath.Join(root, "packages", "tools")
	if got := string(members[0].HostLoadIdentity()); got != wantIdentity {
		t.Fatalf("Pi local host identity = %q, want %q", got, wantIdentity)
	}
}

func TestBuildLocksResolvedOpenCodeLocalLoadIdentity(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["opencode"]

[[extension]]
id = "first"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "./plugins/first.ts" }

[[extension]]
id = "second"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "./plugins/second.ts" }
`))
	if err != nil {
		t.Fatal(err)
	}
	environment, err = declarationmanifest.ResolveSelectedCarrierSources(paths, environment)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Build(context.Background(), Input{
		Paths:                  paths,
		Environment:            environment,
		UsePersistentCache:     false,
		ExtensionOrderIdentity: aggregatecodec.ExtensionOrderIdentityResolver(paths),
	})
	if err != nil {
		t.Fatal(err)
	}
	constraints := result.Snapshot().Locked.OrderConstraints()
	if len(constraints) != 1 {
		t.Fatalf("order constraints = %#v, want one", constraints)
	}
	members := constraints[0].Members()
	wantPrefix := "file://" + filepath.ToSlash(filepath.Join(root, ".opencode", "plugins"))
	for index, member := range members {
		if got := string(member.HostLoadIdentity()); !strings.HasPrefix(got, wantPrefix) {
			t.Fatalf("OpenCode member[%d] host identity = %q, want prefix %q", index, got, wantPrefix)
		}
	}
}
