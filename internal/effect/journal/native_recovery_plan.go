package journal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	"github.com/isty2e/daem/internal/effect/journal/recoveryoperation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// NativeRecoveryPlan retains immutable observations, not live root capabilities.
// Execution must reopen and requalify the complete operation under its leases.
type NativeRecoveryPlan struct {
	journal       journalrecord.Native
	capture       CaptureResult
	active        ActiveJournalAuthority
	inventory     recoverableInventoryAuthority
	assessment    journalqualification.AssessmentFacts
	registryScope ownership.DocumentScope
	blocked       bool
	cleanup       []recovery.RemovalCleanupObligation
}

func (NativeRecoveryPlan) recoverablePlan()            {}
func (plan NativeRecoveryPlan) Clone() RecoverablePlan { return plan }
func (NativeRecoveryPlan) AuthorityKind() RecoveryAuthorityKind {
	return RecoveryAuthorityActiveJournal
}

func (plan NativeRecoveryPlan) Blocked() bool {
	if plan.blocked || plan.capture.OperationID == "" {
		return true
	}
	for _, obligation := range plan.cleanup {
		if obligation.Readiness() == recovery.RemovalCleanupBlocked {
			return true
		}
	}
	return false
}

func (plan NativeRecoveryPlan) HasErrors() bool {
	if plan.Blocked() {
		return true
	}
	for _, obligation := range plan.cleanup {
		if obligation.Readiness() == recovery.RemovalCleanupRetry {
			return true
		}
	}
	return false
}
func (plan NativeRecoveryPlan) OperationID() string       { return plan.capture.OperationID }
func (plan NativeRecoveryPlan) OperationDir() string      { return plan.capture.Directory }
func (plan NativeRecoveryPlan) RecordFingerprint() string { return plan.capture.RecordFingerprint }

func (plan NativeRecoveryPlan) Classification() recovery.Classification {
	if plan.Blocked() {
		return recovery.ClassificationBlocked
	}
	return plan.assessment.Classification()
}

func (plan NativeRecoveryPlan) SameExecutionAuthority(other RecoverablePlan) bool {
	typed, ok := other.(NativeRecoveryPlan)
	if !ok || plan.blocked != typed.blocked || !plan.sameCleanup(typed) || plan.capture != typed.capture || !plan.active.Equal(typed.active) || !plan.inventory.equal(typed.inventory) {
		return false
	}
	scope := plan.registryScope
	if !scope.Equal(typed.registryScope) {
		return false
	}
	return plan.assessment.SameObservedAuthority(typed.assessment, scope)
}

// ObservationFingerprint supplements the durable record revision with actual
// phase and host observations. Complete state and scoped Registry equality are
// additionally required by SameExecutionAuthority.
func (plan NativeRecoveryPlan) ObservationFingerprint() string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00", plan.capture.RecordFingerprint, plan.assessment.ObservationFingerprint())
	for _, obligation := range plan.cleanup {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00", obligation.Readiness(), obligation.Action(), obligation.Reason(), obligation.Detail())
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func NativeActiveRecoveryPlan(selection RecoverablePlan) (NativeRecoveryPlan, bool) {
	plan, ok := selection.(NativeRecoveryPlan)
	return plan, ok
}

func (plan NativeRecoveryPlan) DocumentAliases() ([][]aggregate.DocumentAddress, error) {
	return plan.journal.DocumentAliases()
}

func (plan NativeRecoveryPlan) RemovalIntents() ([]recovery.RemovalIntent, error) {
	return plan.journal.RemovalIntents()
}

func (plan NativeRecoveryPlan) RemovalDemands() (recovery.RemovalDemandSet, error) {
	return plan.journal.RemovalDemands()
}

// NativeRecoveryOperation uses qualified roots for one revalidation/execution.
// Close releases independently captured roots, never roots borrowed from a live
// capture. Its plan projection carries neither kind of capability.
type NativeRecoveryOperation struct {
	plan                        NativeRecoveryPlan
	profile                     recoveryoperation.Profile
	roots                       journalnamespace.Roots
	ownedRoots                  *recoveryoperation.OwnedRoots
	retained                    []*rootedpath.CapturedRoot
	qualified                   journalqualification.Documents
	initial                     journalqualification.Assessment
	closed                      bool
	inventoryProfile            RecoveryInventoryProfile
	capturedRemovalReservations bool
	resolver                    func(output.Destination) (string, error)
	ordinaryCapabilities        RootedCapabilityResolver
}

func (operation *NativeRecoveryOperation) Close() error {
	if operation == nil || operation.closed {
		return nil
	}
	operation.closed = true
	err := operation.ownedRoots.Close()
	for _, root := range operation.retained {
		err = errors.Join(err, root.Close())
	}
	return err
}

func (operation *NativeRecoveryOperation) Plan() NativeRecoveryPlan { return operation.plan }
func (operation *NativeRecoveryOperation) ManifestRoot() *rootedpath.CapturedRoot {
	return operation.roots.Manifest.Root
}

func (operation *NativeRecoveryOperation) Budget() *physicalwork.Budget {
	return operation.profile.Budget
}

func loadNativeRecoveryOperation(ctx context.Context, paths Paths, options PlanLoadOptions, inventory journalinventory.RootInventory) (result *NativeRecoveryOperation, resultErr error) {
	if options.NativeDocuments == nil || options.NativeDocuments.Budget == nil || options.NativeDocuments.StateCodec == nil {
		return nil, fmt.Errorf("native recovery requires its selected profile and original physical allocation")
	}
	profile := *options.NativeDocuments
	if err := profile.Limits.Validate(); err != nil {
		return nil, err
	}
	filesystem, ok := options.Filesystem.(mutationfs.RootedReader)
	if !ok {
		return nil, fmt.Errorf("native recovery requires rooted observation")
	}
	journal, ok := inventory.Active().Record().Native()
	if !ok {
		return nil, fmt.Errorf("native recovery requires a native16 inventory record")
	}
	if profile.Budget.RequireCapturePending() == nil {
		demands, err := journal.RemovalDemands()
		if err != nil {
			return nil, err
		}
		if err := profile.Budget.AdmitRecoveryRemovalDemands(demands); err != nil {
			return nil, err
		}
	}
	if filepath.Clean(paths.ManifestRoot) != profile.Manifest || filepath.Clean(journal.Owner().ManifestPath()) != filepath.Clean(profile.ManifestPath) {
		return nil, fmt.Errorf("native recovery owner differs from the selected manifest")
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, profile.Budget)
	if err != nil || !observed.Exact().Equal(journal.Owner().StatefileAuthority()) {
		return nil, errors.Join(fmt.Errorf("native recovery state authority differs from its selected owner"), err)
	}
	ownedRoots, err := profile.OpenRoots(ctx, journal)
	if err != nil {
		return nil, err
	}
	roots := ownedRoots.Selection()
	operation := &NativeRecoveryOperation{profile: profile, roots: roots, ownedRoots: ownedRoots, resolver: options.Resolver, inventoryProfile: RecoveryInventoryProfile{decoder: nativeRecoveryInventoryDecoder(options.StateCodec, profile.StateCodec)}}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, operation.Close())
			result = nil
		}
	}()
	ordinaryViews, err := journal.RecordedEntries()
	if err != nil {
		return nil, err
	}
	ordinaryCapabilities, ordinaryRoots, err := recoveryoperation.OpenOrdinaryRoots(ctx, ordinaryViews, options.Resolver, options.RootedCapability, profile.Budget)
	if err != nil {
		return nil, err
	}
	operation.retained = append(operation.retained, ordinaryRoots...)
	operation.ordinaryCapabilities = ordinaryCapabilities
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(inventory.Active().OperationDir(), physicalwork.MaximumPhysicalPathDepth, profile.Budget)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close(), ctx.Err())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, operation.Close())
			result = nil
		}
	}()
	active := inventory.Active()
	if err := journalinventory.ValidateActiveJournalRoot(ctx, filesystem, root, profile.Budget, active.PhysicalAuthority()); err != nil {
		return nil, err
	}
	blocked := false
	assessment, err := journalqualification.AssessNativeDocuments(journal, ctx, journalqualification.NativeReadOptions{
		Documents: journalqualification.ReadOptions{Artifacts: root, Filesystem: filesystem, Budget: profile.Budget, Limits: profile.Limits, Codecs: profile.Codecs, OrdinaryCapabilities: ordinaryCapabilities},
		Roots:     roots, State: profile.StateReader, Registry: options.OwnershipRegistry,
		Directory: active.OperationDir(), Active: active.PhysicalAuthority(), Resolver: options.Resolver,
	})
	if err != nil {
		var phase *journalqualification.SelectedPhaseError
		if !errors.As(err, &phase) || ctx.Err() != nil {
			return nil, err
		}
		blocked = true
	}
	snapshot, err := journalqualification.ReadJournalSnapshot(ctx, root, filesystem, profile.Budget)
	if err != nil {
		return nil, err
	}
	if journalrecord.Fingerprint(snapshot.Content()) != active.Identity().JournalAuthorityFingerprint() {
		return nil, fmt.Errorf("native recovery journal changed during qualification")
	}
	if err := journalinventory.ValidateActiveJournalRoot(ctx, filesystem, root, profile.Budget, active.PhysicalAuthority()); err != nil {
		return nil, err
	}
	inv, err := newRecoverableInventoryAuthority(inventory, inventory.Decision().State() == retirement.StatePrepared)
	if err != nil {
		return nil, err
	}
	operation.qualified = assessment.Documents()
	operation.initial = assessment
	scope, err := ownership.NewDocumentScope(nil, nil)
	if !blocked {
		scope, err = assessment.RegistryScope()
	}
	if err != nil {
		return nil, err
	}
	operation.plan = NativeRecoveryPlan{blocked: blocked, journal: journal, capture: CaptureResult{OperationID: journal.OperationID(), Directory: active.OperationDir(), JournalPath: filepath.Join(active.OperationDir(), journalrecord.FileName), RecordFingerprint: active.Identity().JournalAuthorityFingerprint()}, active: active.PhysicalAuthority(), inventory: inv, assessment: assessment.Facts(), registryScope: scope}
	operation.plan, err = operation.plan.assessCleanup(ctx, options, profile.Budget)
	if err != nil {
		return nil, err
	}
	return operation, nil
}

// OpenNativeRecovery requalifies the exact disclosed operation. No source or
// manifest declarations are reinterpreted as recovery authority.
func OpenNativeRecovery(ctx context.Context, paths Paths, selected NativeRecoveryPlan, options PlanLoadOptions) (*NativeRecoveryOperation, error) {
	if options.NativeDocuments == nil || selected.Blocked() {
		return nil, fmt.Errorf("native recovery profile is required")
	}
	inventoryOptions := inventoryOptionsFromPlan(options)
	inventoryOptions.Decoder = nativeRecoveryInventoryDecoder(options.StateCodec, options.NativeDocuments.StateCodec)
	inventory, err := journalinventory.LoadRoot(ctx, paths.RecoveryDir, inventoryOptions)
	if err != nil {
		return nil, err
	}
	if inventory.Active() == nil {
		return nil, fmt.Errorf("native recovery active authority disappeared")
	}
	operation, err := loadNativeRecoveryOperation(ctx, paths, options, inventory)
	if err != nil {
		return nil, err
	}
	if !selected.SameExecutionAuthority(operation.plan) {
		return nil, errors.Join(fmt.Errorf("native recovery observation changed"), operation.Close())
	}
	return operation, nil
}

// NativeRecoveryDocument borrows an open operation. Current's absent zero and
// Restoration's unchanged zero are not validity evidence: qualify Namespace and
// Removal before using content, and validate Restoration before publication.
type NativeRecoveryDocument struct {
	operation *NativeRecoveryOperation
	index     int
}

func (operation *NativeRecoveryOperation) Documents() []NativeRecoveryDocument {
	if operation == nil || operation.closed {
		return nil
	}
	result := make([]NativeRecoveryDocument, operation.qualified.Records().Len())
	for index := range result {
		result[index] = NativeRecoveryDocument{operation: operation, index: index}
	}
	return result
}

func (document NativeRecoveryDocument) Namespace() journalnamespace.DocumentNamespace {
	if !document.valid() {
		return journalnamespace.DocumentNamespace{}
	}
	namespace, present := document.operation.qualified.NamespaceAt(document.index)
	if !present {
		return journalnamespace.DocumentNamespace{}
	}
	return namespace
}

func (document NativeRecoveryDocument) Comparison() journaldocument.Comparison {
	if !document.valid() {
		return journaldocument.Comparison{}
	}
	loaded, present := document.operation.qualified.Records().At(document.index)
	if !present {
		return journaldocument.Comparison{}
	}
	return loaded.Group().Comparison()
}

func (document NativeRecoveryDocument) Current() (aggregate.Document, *recovery.PermissionMode) {
	if !document.valid() {
		return aggregate.Document{}, nil
	}
	stage, present := document.operation.plan.assessment.DocumentAt(document.index)
	if !present {
		return aggregate.Document{}, nil
	}
	current, mode, _ := stage.CurrentState()
	return current, mode
}

func (document NativeRecoveryDocument) Restoration() journaldocument.Restoration {
	if !document.valid() {
		return journaldocument.Restoration{}
	}
	stage, present := document.operation.plan.assessment.DocumentAt(document.index)
	if !present {
		return journaldocument.Restoration{}
	}
	return stage.Restoration()
}

func (document NativeRecoveryDocument) Removal() (recovery.DocumentRemoval, bool, error) {
	if !document.valid() {
		return recovery.DocumentRemoval{}, false, fmt.Errorf("live native recovery document is required")
	}
	doc, err := document.operation.plan.journal.DocumentAt(document.index)
	if err != nil {
		return recovery.DocumentRemoval{}, false, err
	}
	return doc.Removal()
}

func (plan NativeRecoveryPlan) CleanupReady(discharged []recovery.RemovalCleanupObligation) bool {
	intents, err := plan.RemovalIntents()
	if err != nil || len(intents) != len(discharged) {
		return false
	}
	for index, intent := range intents {
		expected, err := recovery.NewPendingRemovalCleanupObligation(intent)
		if err != nil || !expected.SameBasis(discharged[index]) || discharged[index].Readiness() != recovery.RemovalCleanupDischarged {
			return false
		}
	}
	return true
}

func (document NativeRecoveryDocument) valid() bool {
	return document.operation != nil && !document.operation.closed && document.index >= 0 && document.index < document.operation.qualified.Records().Len()
}
