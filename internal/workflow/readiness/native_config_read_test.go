package readiness

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/filesnapshot"

	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeMCPReaderReusesObservedAliasesWithoutIO(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
	aliasPath := filepath.Join(fixture.paths.ManifestRoot, ".pi", "alias.json")
	if err := os.Symlink(fixture.hostPath, aliasPath); err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, outputtest.Parse(t, ".pi/alias.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.observation.Aliases = append(fixture.observation.Aliases, alias)
	documents, err := journalpublication.ObserveNativeDocuments(t.Context(), fixture.observation)
	if err != nil {
		t.Fatal(err)
	}
	native := []nativeAssessmentSources{{input: &NativeAssessmentInput{Documents: &documents}}}
	reader, err := nativeConfigReader(native, liveobserve.DestinationResolver(managedhostpath.Resolver(fixture.paths).Resolve))
	if err != nil {
		t.Fatal(err)
	}
	original, _, observed := documents.Evidence().Document(alias)
	if !observed {
		t.Fatal("fixture alias was not captured")
	}
	if err := os.WriteFile(fixture.hostPath, []byte(`{"changed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	remaining := fixture.observation.Budget.RemainingBytes()
	for _, path := range []string{fixture.hostPath, aliasPath} {
		content, exists, err := reader(t.Context(), path, 1024)
		if err != nil || !exists || !bytes.Equal(content, original.Content()) {
			t.Fatal("MCP reader reread a retained alias", err)
		}
	}
	if fixture.observation.Budget.RemainingBytes() != remaining {
		t.Fatal("retained reads consumed new filesystem work")
	}
	if _, _, err := reader(t.Context(), aliasPath, 1); !errors.Is(err, filesnapshot.ErrLimitExceeded) {
		t.Fatal("retained content bypassed the observer's byte limit", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := reader(ctx, aliasPath, 1024); !errors.Is(err, context.Canceled) {
		t.Fatal("retained content bypassed cancellation", err)
	}
	unrelated := filepath.Join(t.TempDir(), "config.json")
	if _, exists, err := reader(t.Context(), unrelated, 1024); err != nil || exists {
		t.Fatal("unrelated absence was not preserved", err)
	}
	if err := os.WriteFile(unrelated, []byte(`{"unrelated":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if content, exists, err := reader(t.Context(), unrelated, 1024); err != nil || !exists || string(content) != `{"unrelated":true}` {
		t.Fatal("unrelated document did not use its normal bounded read", err)
	}
}
