package readiness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestOrdinarySourceMustAgreeWithRetainedNativeAlias(t *testing.T) {
	resolveFailure := errors.New("ordinary source unavailable")
	for _, scenario := range []string{"same source", "different source", "resolver failure", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
			canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{ServerID: "peer", Command: "npx", Args: []string{}, AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1})
			if err != nil {
				t.Fatal(err)
			}
			contract := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{PlacementID: aggregate.MCPPlacementClaudeProject, ServerID: "peer", LauncherCommand: "npx", CanonicalProjection: string(canonical)})
			item, present, err := contract.ManagedAggregateContribution()
			if err != nil || !present {
				t.Fatal("MCP projection fixture is invalid", err)
			}
			address := item.Contribution().Contract().Address().Document()
			aliasPath := filepath.Join(namespacetest.LegacyRoot(t, fixture.observation.Roots, output.RootProject).SelectedPath, filepath.FromSlash(address.AggregateRoot().RelativePath()))
			if err := os.MkdirAll(filepath.Dir(aliasPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(fixture.hostPath, aliasPath); err != nil {
				t.Fatal(err)
			}
			fixture.observation.Aliases = append(fixture.observation.Aliases, address)
			fixture.observe(t)
			codecs, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{item.Contribution().CodecContractID()}, narrowingLimits, narrowingLimits)
			if err != nil {
				t.Fatal(err)
			}
			selected := aliasPath
			if scenario == "different source" {
				selected = filepath.Join(t.TempDir(), "ordinary.json")
				requireNativePreparationFile(t, selected, []byte(`{"mcpServers":{"peer":{"command":"manual","args":[]}}}`))
			}
			resolver := liveobserve.DestinationResolver(func(output.Destination) (string, error) {
				if scenario == "resolver failure" {
					return "", resolveFailure
				}
				return selected, nil
			})
			native := nativeAssessmentSources{input: &NativeAssessmentInput{Documents: &fixture.input.Documents, Owner: fixture.observation.Owner, Budget: fixture.observation.Budget}}
			selection, err := targetselection.ForAvailableTargets([]target.Target{target.TargetPi, target.TargetClaudeCode}, []string{"pi", "claude-code"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			observed, err := observeAggregateDocuments(ctx, resolver, []aggregate.SubjectContribution{item}, nil, selection, codecs, native)
			if scenario != "same source" {
				if err == nil || len(observed.evidence) != 0 || len(observed.failures) != 0 || len(observed.preconditions) != 0 {
					t.Fatalf("invalid source returned ordinary evidence: %v, evidence=%d failures=%d", err, len(observed.evidence), len(observed.failures))
				}
				if scenario == "resolver failure" && !errors.Is(err, resolveFailure) || scenario == "canceled" && !errors.Is(err, context.Canceled) {
					t.Fatal("ordinary source refusal lost its cause", err)
				}
				if scenario == "different source" && !errors.Is(err, errOrdinaryNativeSourceMismatch) {
					t.Fatal("different source failed without detecting the namespace mismatch", err)
				}
				return
			}
			if err != nil || len(observed.evidence) != 1 || len(observed.failures) != 0 {
				t.Fatal("same-source control did not retain valid MCP evidence", err, len(observed.evidence), len(observed.failures))
			}
		})
	}
}
