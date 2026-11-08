package apply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

var ErrReplacementPlanChanged = errors.New("replacement plan changed; request a fresh dry-run and approval")

type NativeReplacementInput struct {
	ManifestPath string
	ID           string
	Keys         []string
}

// NativeReplacementPlan owns one prepared, not-yet-approved direct operation.
// The underlying generic prepared write is deliberately not exposed.
type NativeReplacementPlan struct {
	prepared   *PreparedWrite
	disclosure NativeReplacementDisclosure
}

type NativeReplacementDisclosure struct {
	Fingerprint string
	Declaration string
	Keys        []NativeReplacementKey
	Result      CommandResult
}

type NativeReplacementKey struct {
	Path           output.KeyPath
	File           string
	Action         string
	CurrentPresent bool
	Current        string
	Desired        string
}

func PlanNativeReplacement(ctx context.Context, input NativeReplacementInput) (*NativeReplacementPlan, error) {
	selection, err := normalizeNativeReplacementInput(input)
	if err != nil {
		return nil, err
	}
	prepared, err := PlanWrite(ctx, CommandInput{ManifestPath: input.ManifestPath, nativeReplacement: &selection})
	if err != nil {
		return nil, errors.Join(err, prepared.Close())
	}
	plan := &NativeReplacementPlan{prepared: prepared}
	if prepared.Reconciliation.HasErrors() {
		return nil, errors.Join(fmt.Errorf("replacement is blocked by selected key or ownership constraints"), prepared.Close())
	}
	planned := prepared.lifecycle.planned
	payload, err := nativeReplacementApprovalPayload(planned)
	if err != nil {
		return nil, errors.Join(err, prepared.Close())
	}
	digest := sha256.Sum256(payload)
	plan.disclosure = NativeReplacementDisclosure{
		Fingerprint: "v1:" + hex.EncodeToString(digest[:]), Declaration: selection.Declaration().String(),
		Result: cloneCommandResult(prepared.CommandResult),
	}
	for _, decision := range planned.assessment.Reconciliation.Aggregates() {
		file, err := destinationResolver(planned.context.Paths).Resolve(decision.DocumentAddress().AggregateRoot(), planned.context.physicalBudget)
		if err != nil {
			return nil, errors.Join(err, prepared.Close())
		}
		for _, projection := range decision.Projections() {
			before, after := projection.Before(), projection.Expected()
			row := NativeReplacementKey{Path: projection.Contract().Address().KeyPath(), File: file, CurrentPresent: before.Present()}
			nativeSyntax, _ := aggregate.HostConfigSyntaxForCodec(decision.CodecContractID())
			syntax, err := configdocument.ParseSyntax(nativeSyntax)
			if err != nil {
				return nil, errors.Join(err, prepared.Close())
			}
			if before.Present() {
				row.Current, err = configdocument.DisplayValue(ctx, syntax, before.CanonicalProjection(), nativeconfig.DocumentLimits())
				if err != nil {
					return nil, errors.Join(err, prepared.Close())
				}
			}
			row.Desired, err = configdocument.DisplayValue(ctx, syntax, after.CanonicalProjection(), nativeconfig.DocumentLimits())
			if err != nil {
				return nil, errors.Join(err, prepared.Close())
			}
			switch {
			case len(projection.PreviousStates()) == 0 && !before.Present():
				row.Action = "create"
			case len(projection.PreviousStates()) == 0 && before.CanonicalProjection() == after.CanonicalProjection():
				row.Action = "adopt"
			case len(projection.PreviousStates()) == 0:
				row.Action = "takeover"
			case !before.Present():
				row.Action = "recreate"
			case before.CanonicalProjection() != after.CanonicalProjection():
				row.Action = "replace"
			case projection.Kind() == reconcile.AggregateRecord:
				row.Action = "record"
			default:
				row.Action = "current"
			}
			plan.disclosure.Keys = append(plan.disclosure.Keys, row)
		}
	}
	return plan, nil
}

func (plan *NativeReplacementPlan) Disclosure() NativeReplacementDisclosure {
	result := plan.disclosure
	result.Keys = append([]NativeReplacementKey(nil), result.Keys...)
	result.Result = cloneCommandResult(result.Result)
	return result
}

func (plan *NativeReplacementPlan) Close() error {
	if plan == nil {
		return nil
	}
	return plan.prepared.Close()
}

// ExecuteNativeReplacement consumes consent for the exact semantic plan. The
// ordinary executor reacquires leases, reobserves and compares it before effects.
func ExecuteNativeReplacement(ctx context.Context, plan *NativeReplacementPlan, expected string, options ExecuteOptions) (CommandResult, error) {
	if plan == nil || plan.prepared == nil {
		return CommandResult{}, ErrPreparedWriteUnavailable
	}
	if !strings.HasPrefix(expected, "v1:") || expected != plan.disclosure.Fingerprint {
		return cloneCommandResult(plan.disclosure.Result), ErrReplacementPlanChanged
	}
	options.PlanWasDisclosed = true
	return ExecuteWithOptions(ctx, plan.prepared, options)
}

func nativeReplacementApprovalPayload(planned commandPlan) ([]byte, error) {
	if planned.context.nativeReplacement == nil || len(planned.assessment.NativeSources) != 1 {
		return nil, fmt.Errorf("replacement requires one selected native declaration")
	}
	identity, err := applyOperationIdentity(planned, reconcile.ContextApply)
	if err != nil {
		return nil, err
	}
	type sourceFacts struct{ Subject, SourceID, ResolvedRef, ContentHash, Profile, Grammar string }
	observed := planned.assessment.NativeSources[0].SourceContract()
	supply, _ := observed.ExactSupply()
	placement, _ := observed.HostConfigPlacement()
	source := sourceFacts{observed.SubjectID().String(), string(supply.SourceID()), string(supply.ResolvedRef()), string(supply.ContentHash()), placement.ID(), string(placement.Grammar())}
	type protectionFacts struct {
		Namespace  []ownershipObservationFingerprintFacts
		Protection string
	}
	var protections []protectionFacts
	for _, evidence := range planned.assessment.NativeApproval {
		var namespaces []ownershipObservationFingerprintFacts
		for _, alias := range evidence.Aliases {
			fact := ownershipObservationFingerprintFacts{Destination: alias.AggregateRoot().String(), FootprintKind: output.WholePathFootprint().Kind().String()}
			if exact, present := evidence.Authority.Exact(); present {
				fact.AuthorityKind = "exact"
				fact.PathAuthority = pathAuthorityFingerprintFactsFor(exact)
			} else {
				missing, _ := evidence.Authority.Provisional()
				fact.AuthorityKind = "provisional"
				fact.ProvisionalCandidateKey = missing.CandidateKey()
				fact.ProvisionalWitness = missing.CandidateWitness()
				fact.ProvisionalNamespaceKey = missing.Namespace().Key()
				fact.ProvisionalNamespaceWitness = missing.Namespace().Witness()
			}
			namespaces = append(namespaces, fact)
		}
		protections = append(protections, protectionFacts{namespaces, evidence.Protection})
	}
	return json.Marshal(struct {
		Operation  string
		Identity   operationplan.ApplyIdentityInput
		Source     sourceFacts
		Protection []protectionFacts
		Limits     configdocument.Limits
	}{"replace-host-config/v1", identity, source, protections, nativeconfig.DocumentLimits()})
}

func normalizeNativeReplacementInput(input NativeReplacementInput) (topologyprojection.HostConfigKeySelection, error) {
	id, err := entity.New(entity.KindHostConfig, input.ID)
	if err != nil {
		return topologyprojection.HostConfigKeySelection{}, commandInputError{cause: err}
	}
	keys := make([]output.KeyPath, 0, len(input.Keys))
	for _, raw := range input.Keys {
		key, err := output.ParseKeyPath(raw)
		if err != nil {
			return topologyprojection.HostConfigKeySelection{}, commandInputError{cause: err}
		}
		keys = append(keys, key)
	}
	selection, err := topologyprojection.NewHostConfigKeySelection(id, keys)
	if err != nil {
		return topologyprojection.HostConfigKeySelection{}, commandInputError{cause: err}
	}
	return selection, nil
}
