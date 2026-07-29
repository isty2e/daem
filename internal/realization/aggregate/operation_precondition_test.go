package aggregate_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestOperationPreconditionsForContractMatchPlacementCatalog(t *testing.T) {
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		t.Run(string(placement.ID()), func(t *testing.T) {
			contract, err := placement.ProjectionContract("context7")
			if err != nil {
				t.Fatalf("ProjectionContract returned error: %v", err)
			}
			preconditions, admitted, err := aggregate.OperationPreconditionsForContract(contract)
			if err != nil {
				t.Fatalf("OperationPreconditionsForContract returned error: %v", err)
			}
			if !admitted {
				t.Fatal("implemented MCP codec has no operation-precondition profile")
			}

			conflictingPath, hasConflictingPath := placement.ConflictingConfigPath()
			if !hasConflictingPath {
				if len(preconditions) != 0 {
					t.Fatalf("preconditions = %#v, want none", preconditions)
				}
				return
			}
			if len(preconditions) != 1 {
				t.Fatalf("preconditions = %#v, want one alternate-document condition", preconditions)
			}
			precondition := preconditions[0]
			if precondition.Kind() != aggregate.OperationPreconditionDocumentAbsent {
				t.Fatalf("kind = %q, want document_absent", precondition.Kind())
			}
			document := precondition.DocumentAddress()
			if document.Target() != placement.Target() ||
				document.Scope() != placement.Scope() ||
				document.AggregateRoot() != conflictingPath {
				t.Fatalf(
					"document = (%q, %q, %q), want (%q, %q, %q)",
					document.Target(),
					document.Scope(),
					document.AggregateRoot(),
					placement.Target(),
					placement.Scope(),
					conflictingPath,
				)
			}
			if err := document.Validate(); err != nil {
				t.Fatalf("precondition document is invalid: %v", err)
			}
			if detail := precondition.UnsatisfiedDetail(); !strings.Contains(
				detail,
				"unsupported alternate config",
			) || !strings.Contains(detail, string(aggregate.OperationPreconditionDocumentAbsent)) ||
				!strings.Contains(detail, conflictingPath.String()) {
				t.Fatalf("unsatisfied detail = %q", detail)
			}
		})
	}
}

func TestOperationPreconditionsForContractAdmitsHooksAndRejectsZeroContract(t *testing.T) {
	for _, location := range []struct {
		target target.Target
		scope  target.Scope
	}{
		{target: target.TargetCodex, scope: target.ScopeProject},
		{target: target.TargetCodex, scope: target.ScopeGlobal},
		{target: target.TargetClaudeCode, scope: target.ScopeProject},
		{target: target.TargetClaudeCode, scope: target.ScopeGlobal},
	} {
		placement, present := aggregate.HookPlacementFor(location.target, location.scope)
		if !present {
			t.Fatalf("HookPlacementFor(%q, %q) is missing", location.target, location.scope)
		}
		contribution, err := placement.Contribution("format")
		if err != nil {
			t.Fatalf("Contribution(%q): %v", placement.ID(), err)
		}
		preconditions, admitted, err := aggregate.OperationPreconditionsForContract(
			contribution.Contract(),
		)
		if err != nil {
			t.Fatalf("OperationPreconditionsForContract(%q): %v", placement.CodecContractID(), err)
		}
		if !admitted || len(preconditions) != 0 {
			t.Fatalf(
				"hook codec %q = %#v, %t, want admitted without preconditions",
				placement.CodecContractID(),
				preconditions,
				admitted,
			)
		}
	}

	preconditions, admitted, err := aggregate.OperationPreconditionsForContract(
		aggregate.ProjectionContract{},
	)
	if err == nil {
		t.Fatal("zero contract returned nil error")
	}
	if admitted || preconditions != nil {
		t.Fatalf("zero contract = %#v, %t, want unadmitted", preconditions, admitted)
	}
}
