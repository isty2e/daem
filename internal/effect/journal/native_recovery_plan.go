package journal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
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
	Budget       *recovery.PhysicalWorkBudget
	StateReader  durable.SnapshotReader
}

// NativeRecoveryPlan retains immutable observations, not live root capabilities.
// Execution must reopen and requalify the complete operation under its leases.
type NativeRecoveryPlan struct {
	journal    nativeJournalSnapshots
	capture    CaptureResult
	active     ActiveJournalAuthority
	inventory  recoverableInventoryAuthority
	assessment nativeDocumentAssessment
	blocked    bool
}

func (NativeRecoveryPlan) recoverablePlan()            {}
func (plan NativeRecoveryPlan) Clone() RecoverablePlan { return plan }
func (NativeRecoveryPlan) AuthorityKind() RecoveryAuthorityKind {
	return RecoveryAuthorityActiveJournal
}

func (plan NativeRecoveryPlan) Blocked() bool             { return plan.blocked || plan.capture.OperationID == "" }
func (plan NativeRecoveryPlan) HasErrors() bool           { return plan.Blocked() }
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
	if !ok || plan.capture != typed.capture || !plan.active.equal(typed.active) || !plan.inventory.equal(typed.inventory) || plan.assessment.disposition != typed.assessment.disposition || !plan.assessment.state.Equal(typed.assessment.state) || !plan.assessment.registry.Equal(typed.assessment.registry) || len(plan.assessment.stages) != len(typed.assessment.stages) {
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
// phase and host observations. Full state and Registry equality is also required.
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
	return hex.EncodeToString(hash.Sum(nil))
}

func NativeActiveRecoveryPlan(selection RecoverablePlan) (NativeRecoveryPlan, bool) {
	plan, ok := selection.(NativeRecoveryPlan)
	return plan, ok
}

func (plan NativeRecoveryPlan) DocumentAliases() [][]aggregate.DocumentAddress {
	result := make([][]aggregate.DocumentAddress, len(plan.journal.record.DocumentGroups))
	for index, group := range plan.journal.record.DocumentGroups {
		for _, alias := range group.Aliases {
			address, err := alias.Address.canonical()
			if err == nil {
				result[index] = append(result[index], address)
			}
		}
	}
	return result
}

func (plan NativeRecoveryPlan) RemovalIntents() ([]recovery.RemovalIntent, error) {
	var intents []recovery.RemovalIntent
	for _, group := range plan.journal.record.DocumentGroups {
		if group.Removal == nil {
			continue
		}
		removal, err := group.Removal.canonical()
		if err != nil {
			return nil, err
		}
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	return intents, nil
}

func (plan NativeRecoveryPlan) RemovalDemands() (recovery.RemovalDemandSet, error) {
	var demands []recovery.RemovalDemand
	for _, group := range plan.journal.record.DocumentGroups {
		if group.Removal == nil {
			continue
		}
		removal, err := group.Removal.canonical()
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		demands = append(demands, removal.Demands().Demands()...)
	}
	return recovery.NewRemovalDemandSet(demands)
}

func (profile NativeRecoveryProfile) decodeInventory(legacy durable.SnapshotCodec) inventoryJournalDecoder {
	return nativeRecoveryInventoryDecoder(legacy, profile.StateCodec)
}

func (profile NativeRecoveryProfile) captureRoots(ctx context.Context, journal nativeJournalSnapshots) (roots NativeDocumentRoots, retained []*rootedpath.CapturedRoot, resultErr error) {
	defer func() {
		if resultErr != nil {
			for _, root := range retained {
				resultErr = errors.Join(resultErr, root.Close())
			}
			retained = nil
		}
	}()
	roles := map[output.RootRole]bool{output.RootProject: true}
	anchors := map[output.RootRole]recoveryRootProvenance{output.RootProject: journal.record.ManifestRootProvenance}
	for _, group := range journal.record.DocumentGroups {
		for _, alias := range group.Aliases {
			address, err := alias.Address.canonical()
			if err != nil {
				return roots, retained, err
			}
			role := address.AggregateRoot().RootRole()
			if previous, known := anchors[role]; known && previous != alias.LogicalRoot.Provenance {
				return roots, retained, fmt.Errorf("native role has inconsistent recorded root anchors")
			}
			anchors[role] = alias.LogicalRoot.Provenance
			roles[role] = true
		}
	}
	for _, role := range []output.RootRole{output.RootProject, output.RootPiAgent, output.RootCodexHome} {
		if !roles[role] {
			continue
		}
		var selected *NativeDocumentRoot
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
		var selection NativeDocumentRoot
		var err error
		if role == output.RootProject {
			var root *rootedpath.CapturedRoot
			root, err = rootedpath.CaptureCanonicalRootBounded(path, recovery.MaximumPhysicalPathDepth, profile.Budget)
			selection = NativeDocumentRoot{SelectedPath: path, Root: root}
		} else {
			selection, err = captureRecordedNativeRoot(ctx, path, anchors[role], profile.Budget)
		}
		if err != nil {
			return roots, retained, err
		}
		retained = append(retained, selection.Root)
		*selected = selection
	}
	return roots, retained, ctx.Err()
}

// NativeRecoveryOperation owns independently captured roots through one live
// revalidation/execution. Its plan projection never carries those capabilities.
type NativeRecoveryOperation struct {
	plan                        NativeRecoveryPlan
	profile                     NativeRecoveryProfile
	roots                       NativeDocumentRoots
	retained                    []*rootedpath.CapturedRoot
	qualified                   nativeQualifiedDocumentRecords
	closed                      bool
	inventoryProfile            RecoveryInventoryProfile
	capturedRemovalReservations bool
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

func (operation *NativeRecoveryOperation) Budget() *recovery.PhysicalWorkBudget {
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
	journal, ok := inventory.active.record.(nativeJournalSnapshots)
	if !ok {
		return nil, fmt.Errorf("native recovery requires a native16 inventory record")
	}
	if len(journal.record.Entries)+len(journal.record.ClaimTransitions)+len(journal.record.ProvisionalAcquires)+len(journal.record.RemovalIntents) != 0 {
		return nil, fmt.Errorf("native recovery requires the complete selected-document execution envelope")
	}
	for _, group := range journal.record.DocumentGroups {
		if group.Extent != "selected" || group.PiOrder != nil {
			return nil, fmt.Errorf("native recovery variant requires its owning execution integration")
		}
	}
	if filepath.Clean(paths.ManifestRoot) != profile.Manifest || filepath.Clean(journal.owner.ManifestPath()) != filepath.Clean(profile.ManifestPath) {
		return nil, fmt.Errorf("native recovery owner differs from the selected manifest")
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, recovery.MaximumPhysicalPathDepth, profile.Budget)
	if err != nil || !observed.Exact().Equal(journal.owner.StatefileAuthority()) {
		return nil, errors.Join(fmt.Errorf("native recovery state authority differs from its selected owner"), err)
	}
	roots, retained, err := profile.captureRoots(ctx, journal)
	if err != nil {
		return nil, err
	}
	operation := &NativeRecoveryOperation{profile: profile, roots: roots, retained: retained, inventoryProfile: RecoveryInventoryProfile{decoder: profile.decodeInventory(options.StateCodec)}}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, operation.Close())
			result = nil
		}
	}()
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(inventory.active.operationDir, recovery.MaximumPhysicalPathDepth, profile.Budget)
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
	assessment, err := journal.assessDocumentRecovery(ctx, nativeDocumentRecoveryReadOptions{
		documents: nativeDocumentReadOptions{artifacts: root, filesystem: filesystem, budget: profile.Budget, limits: profile.Limits, codecs: profile.Codecs},
		roots:     roots, state: profile.StateReader, registry: options.OwnershipRegistry,
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
	if recoveryJournalRecordFingerprint(snapshot.Content()) != active.identity.JournalAuthorityFingerprint() {
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
	assessment.qualified = nativeQualifiedDocumentRecords{records: assessment.qualified.records}
	operation.plan = NativeRecoveryPlan{blocked: blocked, journal: journal, capture: CaptureResult{OperationID: journal.record.OperationID, Directory: active.operationDir, JournalPath: filepath.Join(active.operationDir, recoveryJournalFileName), RecordFingerprint: active.identity.JournalAuthorityFingerprint()}, active: active.physicalAuthority, inventory: inv, assessment: assessment}
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

type NativeRecoveryDocument struct {
	operation *NativeRecoveryOperation
	index     int
}

func (operation *NativeRecoveryOperation) Documents() []NativeRecoveryDocument {
	if operation == nil || operation.closed {
		return nil
	}
	result := make([]NativeRecoveryDocument, len(operation.qualified.records.groups))
	for index := range result {
		result[index] = NativeRecoveryDocument{operation: operation, index: index}
	}
	return result
}

func (document NativeRecoveryDocument) Namespace() NativeDocumentNamespace {
	return document.operation.qualified.namespaces[document.index]
}

func (document NativeRecoveryDocument) Comparison() DocumentComparison {
	return document.operation.qualified.records.groups[document.index].comparison
}

func (document NativeRecoveryDocument) Current() (aggregate.Document, *recovery.PermissionMode) {
	stage := document.operation.plan.assessment.stages[document.index]
	return stage.current, clonePermissionMode(stage.mode)
}

func (document NativeRecoveryDocument) Restoration() DocumentRestoration {
	return document.operation.plan.assessment.stages[document.index].restoration
}

func (document NativeRecoveryDocument) Removal() (recovery.DocumentRemoval, bool, error) {
	record := document.operation.plan.journal.record.DocumentGroups[document.index].Removal
	if record == nil {
		return recovery.DocumentRemoval{}, false, nil
	}
	value, err := record.canonical()
	return value, true, err
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
