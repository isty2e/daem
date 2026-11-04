package journal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// NativeRecoveryProfile is an explicit command-selected reader and root grant.
// Persisted selectors cannot create or enlarge these independently selected roots.
type NativeRecoveryProfile struct {
	StateCodec   durable.SnapshotCodec
	Codecs       aggregate.CodecCatalog
	Limits       configdocument.Limits
	Manifest     string
	ManifestPath string
	ResolveRoot  func(output.RootRole) (string, error)
	Budget       *physicalwork.Budget
	StateReader  durable.SnapshotReader
}

// NativeRecoveryPlan retains immutable observations, not live root capabilities.
// Execution must reopen and requalify the complete operation under its leases.
type NativeRecoveryPlan struct {
	journal       journalrecord.Native
	capture       CaptureResult
	active        ActiveJournalAuthority
	inventory     recoverableInventoryAuthority
	assessment    nativeDocumentAssessment
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
	switch plan.assessment.disposition {
	case nativeDocumentsBefore:
		return recovery.ClassificationCleanBefore
	case nativeDocumentsAfter:
		return recovery.ClassificationCleanAfter
	case nativeDocumentsRollback:
		return recovery.ClassificationNeedsRollback
	case nativeDocumentsFinalize:
		return recovery.ClassificationNeedsFinalize
	default:
		return recovery.ClassificationBlocked
	}
}

func (plan NativeRecoveryPlan) SameExecutionAuthority(other RecoverablePlan) bool {
	typed, ok := other.(NativeRecoveryPlan)
	if !ok || plan.blocked != typed.blocked || !plan.sameCleanup(typed) || plan.capture != typed.capture || !plan.active.equal(typed.active) || !plan.inventory.equal(typed.inventory) || plan.assessment.disposition != typed.assessment.disposition || !plan.assessment.state.Equal(typed.assessment.state) || plan.assessment.ordinary.observationFingerprint() != typed.assessment.ordinary.observationFingerprint() || len(plan.assessment.stages) != len(typed.assessment.stages) {
		return false
	}
	scope := plan.registryScope
	if !scope.Equal(typed.registryScope) {
		return false
	}
	if same, err := scope.Matches(plan.assessment.registry, typed.assessment.registry); err != nil || !same {
		return false
	}
	for index, stage := range plan.assessment.stages {
		candidate := typed.assessment.stages[index]
		if !stage.current.Equal(candidate.current) || !sameNativeRecoveryMode(stage.mode, candidate.mode) || (stage.identity == nil) != (candidate.identity == nil) || stage.identity != nil && !stage.identity.Equal(candidate.identity) {
			return false
		}
	}
	return true
}

func sameNativeRecoveryMode(left, right *recovery.PermissionMode) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

// ObservationFingerprint supplements the durable record revision with actual
// phase and host observations. Complete state and scoped Registry equality are
// additionally required by SameExecutionAuthority.
func (plan NativeRecoveryPlan) ObservationFingerprint() string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00", plan.capture.RecordFingerprint, plan.assessment.disposition)
	for _, stage := range plan.assessment.stages {
		mode := "absent"
		if stage.mode != nil {
			mode = fmt.Sprintf("%04o", *stage.mode)
		}
		content := sha256.Sum256(stage.current.Content())
		fmt.Fprintf(hash, "%t\x00%s\x00%x\x00", stage.current.Exists(), mode, content)
	}
	fmt.Fprintf(hash, "%s\x00", plan.assessment.ordinary.observationFingerprint())
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

func (profile NativeRecoveryProfile) decodeInventory(legacy durable.SnapshotCodec) inventoryJournalDecoder {
	return nativeRecoveryInventoryDecoder(legacy, profile.StateCodec)
}

func (profile NativeRecoveryProfile) captureRoots(ctx context.Context, journal journalrecord.Native) (roots journalnamespace.Roots, retained []*rootedpath.CapturedRoot, resultErr error) {
	defer func() {
		if resultErr != nil {
			for _, root := range retained {
				resultErr = errors.Join(resultErr, root.Close())
			}
			retained = nil
		}
	}()
	anchors, err := journal.RecordedRootAnchors()
	if err != nil {
		return roots, retained, err
	}
	for _, role := range []output.RootRole{output.RootProject, output.RootPiAgent, output.RootCodexHome} {
		if _, needed := anchors[role]; !needed {
			continue
		}
		var selected *journalnamespace.RootSelection
		var path string
		switch role {
		case output.RootProject:
			selected, path = &roots.Manifest, profile.Manifest
		case output.RootPiAgent:
			selected = &roots.PiAgent
		case output.RootCodexHome:
			selected = &roots.CodexHome
		}
		if path == "" && profile.ResolveRoot != nil {
			var err error
			path, err = profile.ResolveRoot(role)
			if err != nil {
				return roots, retained, err
			}
		}
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return roots, retained, fmt.Errorf("native recovery requires a canonical independently selected root")
		}
		var selection journalnamespace.RootSelection
		var err error
		if role == output.RootProject {
			var root *rootedpath.CapturedRoot
			root, err = rootedpath.CaptureCanonicalRootBounded(path, physicalwork.MaximumPhysicalPathDepth, profile.Budget)
			selection = journalnamespace.RootSelection{SelectedPath: path, Root: root}
		} else {
			selection, err = journalnamespace.CaptureRecordedRoot(ctx, path, anchors[role], profile.Budget)
		}
		if err != nil {
			return roots, retained, err
		}
		retained = append(retained, selection.Root)
		*selected = selection
	}
	return roots, retained, ctx.Err()
}

// NativeRecoveryOperation uses qualified roots for one revalidation/execution.
// Close releases independently captured roots, never roots borrowed from a live
// capture. Its plan projection carries neither kind of capability.
type NativeRecoveryOperation struct {
	plan                        NativeRecoveryPlan
	profile                     NativeRecoveryProfile
	roots                       journalnamespace.Roots
	retained                    []*rootedpath.CapturedRoot
	qualified                   nativeQualifiedDocumentRecords
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
	var err error
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

func loadNativeRecoveryOperation(ctx context.Context, paths Paths, options PlanLoadOptions, inventory recoveryRootInventory) (result *NativeRecoveryOperation, resultErr error) {
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
	journal, ok := inventory.active.record.Native()
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
	roots, retained, err := profile.captureRoots(ctx, journal)
	if err != nil {
		return nil, err
	}
	operation := &NativeRecoveryOperation{profile: profile, roots: roots, retained: retained, resolver: options.Resolver, inventoryProfile: RecoveryInventoryProfile{decoder: profile.decodeInventory(options.StateCodec)}}
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
	ordinaryCapabilities, ordinaryRoots, err := captureNativeOrdinaryRecoveryRoots(ctx, ordinaryViews, options.Resolver, options.RootedCapability, profile.Budget)
	if err != nil {
		return nil, err
	}
	operation.retained = append(operation.retained, ordinaryRoots...)
	operation.ordinaryCapabilities = ordinaryCapabilities
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(inventory.active.operationDir, physicalwork.MaximumPhysicalPathDepth, profile.Budget)
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
	active := inventory.active
	if err := ValidateActiveJournalRoot(ctx, filesystem, root, profile.Budget, active.physicalAuthority); err != nil {
		return nil, err
	}
	blocked := false
	assessment, err := assessDocumentRecovery(journal, ctx, nativeDocumentRecoveryReadOptions{
		documents: nativeDocumentReadOptions{artifacts: root, filesystem: filesystem, budget: profile.Budget, limits: profile.Limits, codecs: profile.Codecs, ordinaryCapabilities: ordinaryCapabilities},
		roots:     roots, state: profile.StateReader, registry: options.OwnershipRegistry,
		directory: active.operationDir, active: active.physicalAuthority, resolver: options.Resolver,
	})
	if err != nil {
		var phase *nativeSelectedPhaseError
		if !errors.As(err, &phase) || ctx.Err() != nil {
			return nil, err
		}
		blocked = true
	}
	snapshot, err := readNativeCaptureRecord(ctx, root, filesystem, profile.Budget)
	if err != nil {
		return nil, err
	}
	if journalrecord.Fingerprint(snapshot.Content()) != active.identity.JournalAuthorityFingerprint() {
		return nil, fmt.Errorf("native recovery journal changed during qualification")
	}
	if err := ValidateActiveJournalRoot(ctx, filesystem, root, profile.Budget, active.physicalAuthority); err != nil {
		return nil, err
	}
	inv, err := newRecoverableInventoryAuthority(inventory, inventory.decision.State() == retirement.StatePrepared)
	if err != nil {
		return nil, err
	}
	operation.qualified = assessment.qualified
	scope, err := ownership.NewDocumentScope(nil, nil)
	if !blocked {
		scope, err = assessment.registryScope()
	}
	if err != nil {
		return nil, err
	}
	assessment.qualified = nativeQualifiedDocumentRecords{records: assessment.qualified.records}
	operation.plan = NativeRecoveryPlan{blocked: blocked, journal: journal, capture: CaptureResult{OperationID: journal.OperationID(), Directory: active.operationDir, JournalPath: filepath.Join(active.operationDir, journalrecord.FileName), RecordFingerprint: active.identity.JournalAuthorityFingerprint()}, active: active.physicalAuthority, inventory: inv, assessment: assessment, registryScope: scope}
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
	inventoryOptions.recordDecoder = options.NativeDocuments.decodeInventory(options.StateCodec)
	inventory, err := loadRecoveryRootInventory(ctx, paths.RecoveryDir, inventoryOptions)
	if err != nil {
		return nil, err
	}
	if inventory.active == nil {
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
	result := make([]NativeRecoveryDocument, operation.qualified.records.Len())
	for index := range result {
		result[index] = NativeRecoveryDocument{operation: operation, index: index}
	}
	return result
}

func (document NativeRecoveryDocument) Namespace() journalnamespace.DocumentNamespace {
	if !document.valid() || document.index >= len(document.operation.qualified.namespaces) {
		return journalnamespace.DocumentNamespace{}
	}
	return document.operation.qualified.namespaces[document.index]
}

func (document NativeRecoveryDocument) Comparison() journaldocument.Comparison {
	if !document.valid() {
		return journaldocument.Comparison{}
	}
	loaded, present := document.operation.qualified.records.At(document.index)
	if !present {
		return journaldocument.Comparison{}
	}
	return loaded.Group().Comparison()
}

func (document NativeRecoveryDocument) Current() (aggregate.Document, *recovery.PermissionMode) {
	if !document.valid() || document.index >= len(document.operation.plan.assessment.stages) {
		return aggregate.Document{}, nil
	}
	stage := document.operation.plan.assessment.stages[document.index]
	var mode *recovery.PermissionMode
	if stage.mode != nil {
		copy := *stage.mode
		mode = &copy
	}
	return stage.current, mode
}

func (document NativeRecoveryDocument) Restoration() journaldocument.Restoration {
	if !document.valid() || document.index >= len(document.operation.plan.assessment.stages) {
		return journaldocument.Restoration{}
	}
	return document.operation.plan.assessment.stages[document.index].restoration
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
	return document.operation != nil && !document.operation.closed && document.index >= 0 && document.index < document.operation.qualified.records.Len()
}
