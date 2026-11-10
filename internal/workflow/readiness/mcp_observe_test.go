package readiness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/output/hostpath"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestUnsupportedAlternateMCPProjectionConfigUsesCanonicalPlacementPath(t *testing.T) {
	root := t.TempDir()
	operations, ok := mcpcodec.ImplementedMCPPlacementOperationsForPlacement(
		aggregate.MCPPlacementOpenCodeProject,
	)
	if !ok {
		t.Fatal("OpenCode project MCP placement operations are missing")
	}
	conflictingConfigPath, hasConflictingConfigPath := operations.Placement().ConflictingConfigPath()
	if !hasConflictingConfigPath {
		t.Fatal("OpenCode project placement is missing its alternate config path")
	}
	hostPath := filepath.Join(root, filepath.FromSlash(conflictingConfigPath.RelativePath()))
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
		t.Fatalf("create conflicting config parent: %v", err)
	}
	if err := os.WriteFile(hostPath, []byte(`{"mcp":{}}`), 0o600); err != nil {
		t.Fatalf("write conflicting config: %v", err)
	}

	contract, err := operations.Placement().ProjectionContract("context7")
	if err != nil {
		t.Fatalf("ProjectionContract: %v", err)
	}
	preconditions, admitted, err := aggregate.OperationPreconditionsForContract(contract)
	if err != nil {
		t.Fatalf("OperationPreconditionsForContract: %v", err)
	}
	if !admitted || len(preconditions) != 1 {
		t.Fatalf("operation preconditions = %#v, %t, want one admitted row", preconditions, admitted)
	}
	precondition := preconditions[0]
	if precondition.DocumentAddress().AggregateRoot() != conflictingConfigPath {
		t.Fatalf(
			"precondition path = %q, want canonical placement conflict path %q",
			precondition.DocumentAddress().AggregateRoot(),
			conflictingConfigPath,
		)
	}
	present, err := observeAggregatePrecondition(
		context.Background(),
		liveobserve.DestinationResolver(hostpath.NewResolver(root).Resolve),
		precondition,
	)
	if err != nil {
		t.Fatalf("observeAggregatePrecondition: %v", err)
	}
	if present {
		t.Fatalf("conflicting config %q satisfied an absent-document precondition", conflictingConfigPath)
	}
}

func TestNativeAggregatePreconditionsUseTheirOwnObservedDocument(t *testing.T) {
	for _, primaryExists := range []bool{false, true} {
		for _, retainAlternate := range []bool{false, true} {
			t.Run(fmt.Sprintf("primary=%t/retained_alternate=%t", primaryExists, retainAlternate), func(t *testing.T) {
				fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
				subject := testLockedMCPSubjectFor(t, "context7", target.TargetOpenCode, target.ScopeProject)
				desired, present, err := subject.ManagedAggregateContribution()
				if err != nil || !present {
					t.Fatal("fixture has no MCP contribution", err)
				}
				contract := desired.Contribution().Contract()
				preconditions, _, err := aggregate.OperationPreconditionsForContract(contract)
				if err != nil || len(preconditions) != 1 {
					t.Fatal("fixture has no alternate-document precondition", err)
				}
				primary, alternate := contract.Address().Document(), preconditions[0].DocumentAddress()
				resolve := managedhostpath.Resolver(fixture.paths, nil).Resolve
				primaryPath, err := resolve(primary.AggregateRoot())
				if err != nil {
					t.Fatal(err)
				}
				alternatePath, err := resolve(alternate.AggregateRoot())
				if err != nil {
					t.Fatal(err)
				}
				if primaryExists {
					requireNativePreparationFile(t, primaryPath, []byte(`{}`))
				} else {
					requireNativePreparationFile(t, alternatePath, []byte(`{}`))
				}
				fixture.observation.Aliases = append(fixture.observation.Aliases, primary)
				if retainAlternate {
					fixture.observation.Aliases = append(fixture.observation.Aliases, alternate)
				}
				documents, err := journalpublication.ObserveNativeDocuments(t.Context(), fixture.observation)
				if err != nil {
					t.Fatal(err)
				}
				if retainAlternate {
					if primaryExists {
						requireNativePreparationFile(t, alternatePath, []byte(`{}`))
					} else if err := os.Remove(alternatePath); err != nil {
						t.Fatal(err)
					}
				}
				catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{contract.CodecContractID()}, narrowingLimits, narrowingLimits)
				if err != nil {
					t.Fatal(err)
				}
				selection, err := targetselection.ForAvailableTargets([]target.Target{target.TargetOpenCode}, nil)
				if err != nil {
					t.Fatal(err)
				}
				observations, err := observeAggregateDocuments(t.Context(), resolve, []aggregate.SubjectContribution{desired}, nil, selection, catalog,
					nativeAssessmentSources{input: &NativeAssessmentInput{Documents: &documents}})
				if err != nil || len(observations.preconditions) != 1 || observations.preconditions[0].Satisfied() != primaryExists {
					t.Fatal("alternate-document precondition used the primary or a different epoch", observations.preconditions, err)
				}
			})
		}
	}
}
