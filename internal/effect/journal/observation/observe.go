package observation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// Input supplies independently selected observation
// dependencies. Root and allocation ownership stay with the containing operation.
type Input struct {
	Owner      stateauthority.Authority
	Before     durable.Snapshot
	Aliases    []aggregate.DocumentAddress
	Roots      journalnamespace.Roots
	Filesystem mutationfs.RootedReader
	Registry   ownershipmutation.RegistryReader
	Limits     configdocument.Limits
	Budget     *physicalwork.Budget
}

type nativeDocumentOriginal struct {
	document aggregate.Document
	mode     *recovery.PermissionMode
	identity mutationfs.EntryIdentity
}

// Batch retains evidence, not a publication or execution
// grant. Root lifetime and the shared allocation remain with the containing owner.
type Batch struct {
	owner       stateauthority.Authority
	before      durable.Snapshot
	namespaces  []journalnamespace.DocumentNamespace
	registry    ownership.Registry
	originals   []nativeDocumentOriginal
	limits      configdocument.Limits
	budget      *physicalwork.Budget
	initialized bool
}

// Namespaces returns the coalesced observation groups; their roots remain borrowed.
func (batch Batch) Namespaces() []journalnamespace.DocumentNamespace {
	return slices.Clone(batch.namespaces)
}

func (batch Batch) DocumentLimits() configdocument.Limits {
	return batch.limits
}

// Observe borrows the selected roots and pending budget. The
// resulting evidence requires separate ownership and operation qualification.
func Observe(ctx context.Context, input Input) (Batch, error) {
	if ctx == nil || input.Registry == nil || input.Filesystem == nil {
		return Batch{}, fmt.Errorf("native observation requires context, filesystem and the selected registry reader")
	}
	if err := errors.Join(ctx.Err(), input.Owner.Validate(), input.Limits.Validate(), input.Budget.RequireCapturePending()); err != nil {
		return Batch{}, err
	}
	if err := requireManifest(ctx, input.Roots, input.Budget); err != nil {
		return Batch{}, err
	}
	if len(input.Aliases) != 0 {
		if _, err := journaldocument.AliasSet(ctx, input.Aliases); err != nil {
			return Batch{}, err
		}
	}
	before, err := journaldocument.IndexSnapshot(ctx, input.Before)
	if err != nil {
		return Batch{}, err
	}
	nativeBefore, err := before.NativeDestinations(ctx)
	if err != nil {
		return Batch{}, err
	}
	namespaces, err := observeNativeDocumentGroups(ctx, input)
	if err != nil {
		return Batch{}, err
	}
	var missing []pathauthority.Provisional
	for _, namespace := range namespaces {
		provisional, absent := namespace.Observation().Provisional()
		if !absent {
			continue
		}
		footprints, err := before.Footprints(ctx, namespace.Addresses())
		if err != nil {
			return Batch{}, err
		}
		hasHistory := len(footprints) != 0
		for _, alias := range namespace.Addresses() {
			_, native := nativeBefore[alias.AggregateRoot()]
			hasHistory = hasHistory || native
		}
		if hasHistory {
			missing = append(missing, provisional)
		}
	}
	current, err := readNativeCaptureRegistry(ctx, input.Registry, input.Owner, missing, input.Budget)
	if err != nil {
		return Batch{}, err
	}
	originals := make([]nativeDocumentOriginal, 0, len(namespaces))
	for _, namespace := range namespaces {
		document, mode, identity, err := namespace.ReadReferent(ctx, min(input.Limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.Budget)
		if err != nil {
			return Batch{}, err
		}
		if document.Exists() && (identity == nil || identity.Kind() != mutationfs.EntryKindFile) {
			return Batch{}, fmt.Errorf("native original requires regular-file identity")
		}
		originals = append(originals, nativeDocumentOriginal{document: document, mode: mode, identity: identity})
	}
	if err := errors.Join(ctx.Err(), input.Budget.RequireCapturePending()); err != nil {
		return Batch{}, err
	}
	return Batch{
		owner: input.Owner, before: input.Before, namespaces: namespaces, registry: current,
		originals: originals, limits: input.Limits, budget: input.Budget, initialized: true,
	}, nil
}

func observeNativeDocumentGroups(ctx context.Context, input Input) ([]journalnamespace.DocumentNamespace, error) {
	aliases := slices.Clone(input.Aliases)
	slices.SortFunc(aliases, journaldocument.CompareAliases)
	indices := make(map[string]int)
	var representatives []journalnamespace.DocumentNamespace
	var groups [][]aggregate.DocumentAddress
	for _, alias := range aliases {
		namespace, err := journalnamespace.Observe(ctx, []aggregate.DocumentAddress{alias}, input.Roots, input.Filesystem, input.Budget)
		if err != nil {
			return nil, err
		}
		key := ""
		if exact, known := namespace.Observation().Exact(); known {
			key = "exact:" + exact.Key()
		} else {
			provisional, _ := namespace.Observation().Provisional()
			key = "provisional:" + provisional.CandidateKey()
		}
		index, present := indices[key]
		if !present {
			indices[key] = len(groups)
			representatives = append(representatives, namespace)
			groups = append(groups, []aggregate.DocumentAddress{alias})
			continue
		}
		if !representatives[index].Observation().Equal(namespace.Observation()) {
			return nil, fmt.Errorf("native aliases disagree on the same comparison identity")
		}
		groups[index] = append(groups[index], alias)
	}
	result := make([]journalnamespace.DocumentNamespace, 0, len(groups))
	for index, aliases := range groups {
		namespace, err := journalnamespace.Observe(ctx, aliases, input.Roots, input.Filesystem, input.Budget)
		if err != nil {
			return nil, err
		}
		if !representatives[index].Observation().Equal(namespace.Observation()) {
			return nil, fmt.Errorf("native namespace changed while coalescing aliases")
		}
		result = append(result, namespace)
	}
	return result, nil
}

func readNativeCaptureRegistry(ctx context.Context, reader ownershipmutation.RegistryReader, owner stateauthority.Authority, missing []pathauthority.Provisional, budget *physicalwork.Budget) (ownership.Registry, error) {
	if reader == nil || budget == nil {
		return ownership.Registry{}, fmt.Errorf("native capture requires its selected registry reader and shared budget")
	}
	if len(missing) == 0 {
		return reader.LoadForClaimRemovals(ctx, nil, physicalwork.MaximumPhysicalPathDepth, budget)
	}
	qualified, supported := reader.(ownershipmutation.DocumentRegistryReader)
	if !supported {
		return ownership.Registry{}, fmt.Errorf("missing native history requires a document-qualified registry reader")
	}
	return qualified.LoadForDocuments(ctx, owner, missing, nil, physicalwork.MaximumPhysicalPathDepth, budget)
}

// Document returns the retained original; false means unobserved, not absent.
func (batch Batch) Document(alias aggregate.DocumentAddress) (aggregate.Document, *recovery.PermissionMode, bool) {
	if !batch.initialized {
		return aggregate.Document{}, nil, false
	}
	for index, namespace := range batch.namespaces {
		if slices.Contains(namespace.Addresses(), alias) {
			original := batch.originals[index]
			if original.mode == nil {
				return original.document, nil, true
			}
			mode := *original.mode
			return original.document, &mode, true
		}
	}
	return aggregate.Document{}, nil, false
}

func (batch Batch) PrepareOwnership(ctx context.Context, roots journalnamespace.Roots, after durable.Snapshot, operationID string) ([]CaptureInput, error) {
	if !batch.initialized || ctx == nil {
		return nil, fmt.Errorf("native observation batch and context are required")
	}
	if err := errors.Join(ctx.Err(), batch.budget.RequireCapturePending()); err != nil {
		return nil, err
	}
	if err := batch.ValidateOriginals(ctx, roots); err != nil {
		return nil, err
	}
	if err := errors.Join(ctx.Err(), batch.budget.RequireCapturePending()); err != nil {
		return nil, err
	}
	input := nativeCaptureOwnershipInput{
		owner: batch.owner, operationID: operationID, before: batch.before, after: after,
		namespaces: batch.namespaces, limits: batch.limits, budget: batch.budget,
	}
	return input.prepareFromRegistry(ctx, batch.registry)
}

// ValidateOriginals checks retained document facts against the supplied root
// gate. Success is not qualification of the containing operation's root custody.
func (batch Batch) ValidateOriginals(ctx context.Context, roots journalnamespace.Roots) error {
	if err := batch.Validate(); err != nil {
		return err
	}
	if err := requireManifest(ctx, roots, batch.budget); err != nil {
		return err
	}
	for index, namespace := range batch.namespaces {
		document, mode, identity, err := namespace.ReadReferent(ctx, min(batch.limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), batch.budget)
		if err != nil {
			return err
		}
		original := batch.originals[index]
		sameMode := mode == nil && original.mode == nil || mode != nil && original.mode != nil && *mode == *original.mode
		sameIdentity := identity == nil && original.identity == nil || identity != nil && original.identity != nil && identity.Equal(original.identity)
		if !document.Equal(original.document) || !sameMode || !sameIdentity {
			return fmt.Errorf("native original changed after its decision observation")
		}
	}
	return ctx.Err()
}

func requireManifest(ctx context.Context, selected journalnamespace.Roots, budget *physicalwork.Budget) error {
	roots, err := selected.Qualify(ctx, budget)
	if err != nil {
		return err
	}
	if _, present := roots.Manifest(); !present {
		return fmt.Errorf("native observation requires its selected manifest root")
	}
	return nil
}

func (batch Batch) Validate() error {
	if !batch.initialized {
		return fmt.Errorf("native observation is unavailable")
	}
	return nil
}

func (batch Batch) MatchesOriginal(index int, document aggregate.Document, mode *recovery.PermissionMode) bool {
	if !batch.initialized || index < 0 || index >= len(batch.originals) {
		return false
	}
	original := batch.originals[index]
	sameMode := mode == nil && original.mode == nil || mode != nil && original.mode != nil && *mode == *original.mode
	return document.Equal(original.document) && sameMode
}

// ReadRegistry reuses the selected document-history allowances in one fresh
// read. It does not infer allowances from the Registry being inspected.
func (batch Batch) ReadRegistry(ctx context.Context, reader ownershipmutation.RegistryReader) (ownership.Registry, error) {
	input := nativeCaptureOwnershipInput{
		owner: batch.owner, operationID: "native-registry-read", before: batch.before, after: batch.before,
		namespaces: batch.namespaces, limits: batch.limits, budget: batch.budget,
	}
	membership, err := input.membership(ctx)
	if err != nil {
		return ownership.Registry{}, err
	}
	return readNativeCaptureRegistry(ctx, reader, batch.owner, membership.missingHistory, batch.budget)
}
