package recovery

import (
	"fmt"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

const (
	// MaximumRemovalIntents bounds the complete operation-scoped removal
	// authority. A single intent can require namespace plus two slot observations.
	MaximumRemovalIntents = 4_096
	// MaximumArtifactTreeDepth bounds every recursive artifact observation,
	// backup restore, and cleanup pass. Planning and execution derive local
	// limits from this operation contract.
	MaximumArtifactTreeDepth = 64
	// MaximumArtifactTreeEntries is the independent per-state descendant limit.
	MaximumArtifactTreeEntries = 100_000
	// MaximumArtifactTreeBytes is the independent per-state regular-file byte limit.
	MaximumArtifactTreeBytes int64 = 4 << 30
	// MaximumRecoveryBackupFileBytes is the largest single regular-file backup
	// admitted by planning and execution.
	MaximumRecoveryBackupFileBytes int64 = 128 << 20
	// MaximumPhysicalPathDepth bounds the root/path chain opened for one rooted
	// observation. It is deliberately well above ordinary
	// agent configuration layouts while preventing one path from monopolizing
	// descriptor work.
	MaximumPhysicalPathDepth = 256
	// maximumPhysicalPathComponentVisits is the fixed operation-wide ceiling
	// across planning, re-observation, and durable-absence confirmation. It is
	// independent of intent cardinality so a large journal cannot multiply the
	// amount of admitted path work.
	maximumPhysicalPathComponentVisits = 524_288
	forwardRemovalCapabilityPasses     = 2
	forwardRemovalNamespacePasses      = 5
	forwardRemovalCandidatePasses      = 2
	removalSlotObservationPasses       = 4
	removalSlotMutationPasses          = 3
	removalSlotAbsencePasses           = 2 + mutationfs.RootedAbsencePathObservationCount

	removalPlanningObservationsPerIntent = 3
	// Forward execution performs one preflight observation, reserves one fresh
	// candidate observation per reachable state, and may validate one namespace
	// per attempted removal. A relation has at most before and expected states.
	removalForwardObservationsPerIntent          = 1 + 2*MaximumRemovalStatesPerIntent
	removalCleanupPreflightObservationsPerIntent = 3
	removalCleanupExecutionObservationsPerIntent = 3 + 2*mutationfs.RootedAbsencePathObservationCount
	removalObservationsPerIntent                 = removalPlanningObservationsPerIntent +
		removalForwardObservationsPerIntent +
		removalCleanupPreflightObservationsPerIntent +
		removalCleanupExecutionObservationsPerIntent
	// MaximumPhysicalEntries is the whole-operation recursive entry ceiling.
	MaximumPhysicalEntries = 400_000
	// MaximumPhysicalBytes is the whole-operation regular-file byte ceiling.
	MaximumPhysicalBytes int64 = 16 << 30
)

// ArtifactWork is the bounded content work observed for one regular file or
// directory artifact. It carries no path or mutation authority.
type ArtifactWork struct {
	entries int
	bytes   int64
}

// NewArtifactWork constructs one non-negative work measurement.
func NewArtifactWork(entries int, bytes int64) (ArtifactWork, error) {
	if entries < 0 || bytes < 0 {
		return ArtifactWork{}, fmt.Errorf("artifact work must not be negative")
	}
	return ArtifactWork{entries: entries, bytes: bytes}, nil
}

// Entries returns the observed descendant-entry count.
func (work ArtifactWork) Entries() int { return work.entries }

// Bytes returns the observed regular-file byte count.
func (work ArtifactWork) Bytes() int64 { return work.bytes }

// Equal reports exact agreement of bounded descendant and byte work.
func (work ArtifactWork) Equal(other ArtifactWork) bool {
	return work == other
}

// PhysicalWorkBudget bounds physical observations and artifact traversal for
// one recovery planning or effect pass. Planning charges current-path,
// ownership, backup, and cleanup evidence to the same operation ceilings;
// execution receives only capacity reserved before its first effect.
type PhysicalWorkBudget struct {
	observations                     int
	observationLimit                 int
	reservedForwardObservations      int
	forwardExecutionBegun            bool
	reservedExecutionObservations    int
	pathComponents                   int
	pathComponentLimit               int
	reservedForwardPathComponents    int
	reservedForwardEntries           int
	reservedForwardBytes             int64
	reservedBackupPathComponents     int
	reservedCleanupObservations      int
	reservedCleanupPathComponents    int
	reservedExecutionPathComponents  int
	reservedGeneralPathComponents    int
	reservedSemanticPathComponents   int
	reservedScratchPathComponents    int
	reservedRetirementPathComponents int
	entries                          int
	bytes                            int64
	entryLimit                       int
	byteLimit                        int64
	probeBytes                       int64
	probeByteLimit                   int64
	reservedReobservationEntries     int
	reservedReobservationBytes       int64
	reservedCleanupEntries           int
	reservedCleanupBytes             int64
	reservedProbeBytes               int64
	reservedBackupEntries            int
	reservedBackupBytes              int64
	reservedGeneralEntries           int
	reservedGeneralBytes             int64
	reservedGeneralProbeBytes        int64
	reservedScratchEntries           int
	reservedScratchBytes             int64
	scratchCleanupWork               ArtifactWork
	reservedRetirementEntries        int
	reservedRetirementBytes          int64
	scratchCleanupReserved           bool
	backupExecutionBegun             bool
	cleanupExecutionBegun            bool
	generalExecutionBegun            bool
	semanticExecutionBegun           bool
	scratchCleanupDisposition        scratchCleanupDisposition
	retirementDisposition            retirementDisposition
}

// BeginGeneralExecution transfers the pre-effect host reservation and the
// remaining control-path capacity into disjoint execution capabilities.
func (budget *PhysicalWorkBudget) BeginGeneralExecution() (
	*PhysicalWorkBudget,
	*PhysicalWorkBudget,
	error,
) {
	if budget == nil {
		return nil, nil, fmt.Errorf("physical work budget is required")
	}
	if budget.generalExecutionBegun {
		return nil, nil, fmt.Errorf("general recovery execution capacity was already transferred")
	}
	if !budget.cleanupExecutionBegun {
		return nil, nil, fmt.Errorf("removal cleanup lifecycle must be transferred before general recovery execution")
	}
	if budget.scratchCleanupDisposition == scratchCleanupPending {
		return nil, nil, fmt.Errorf("recovery scratch cleanup must be transferred before general recovery execution")
	}
	if budget.retirementDisposition == retirementPending {
		return nil, nil, fmt.Errorf("journal retirement capacity must be transferred before general recovery execution")
	}
	budget.generalExecutionBegun = true
	host := &PhysicalWorkBudget{
		pathComponentLimit: budget.reservedGeneralPathComponents,
		entryLimit:         budget.reservedGeneralEntries,
		byteLimit:          budget.reservedGeneralBytes,
		probeByteLimit:     budget.reservedGeneralProbeBytes,
	}
	control := &PhysicalWorkBudget{
		pathComponentLimit: max(0, budget.pathComponentLimit-budget.pathComponents),
	}
	budget.pathComponents = budget.pathComponentLimit
	budget.entries = budget.entryLimit
	budget.bytes = budget.byteLimit
	return host, control, nil
}

// ReserveGeneralPathComponents charges path work that will be transferred to
// the host-destination execution capability.
func (budget *PhysicalWorkBudget) ReserveGeneralPathComponents(count int) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.generalExecutionBegun {
		return fmt.Errorf("general recovery execution capacity was already transferred")
	}
	before := budget.pathComponents
	if err := budget.AdmitPathComponents(count); err != nil {
		return err
	}
	budget.reservedGeneralPathComponents += budget.pathComponents - before
	return nil
}

// ReserveSemanticPathComponents charges path work that will be transferred to
// statefile and ownership semantic-witness validation. The reservation grants
// no payload or mutation authority.
func (budget *PhysicalWorkBudget) ReserveSemanticPathComponents(count int) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.semanticExecutionBegun || budget.generalExecutionBegun {
		return fmt.Errorf("recovery semantic execution capacity was already transferred")
	}
	before := budget.pathComponents
	if err := budget.AdmitPathComponents(count); err != nil {
		return err
	}
	budget.reservedSemanticPathComponents += budget.pathComponents - before
	return nil
}

// BeginReservedSemanticExecution transfers only path capacity reserved before
// effects for statefile and ownership semantic-witness validation.
func (budget *PhysicalWorkBudget) BeginReservedSemanticExecution() (*PhysicalWorkBudget, error) {
	if budget == nil {
		return nil, fmt.Errorf("physical work budget is required")
	}
	if budget.semanticExecutionBegun {
		return nil, fmt.Errorf("recovery semantic execution capacity was already transferred")
	}
	if budget.generalExecutionBegun {
		return nil, fmt.Errorf("recovery semantic capacity must be transferred before general execution")
	}
	budget.semanticExecutionBegun = true
	return &PhysicalWorkBudget{
		pathComponentLimit: budget.reservedSemanticPathComponents,
	}, nil
}

// ReserveGeneralFileObservation charges one future bounded file read. A
// one-byte proof allowance distinguishes an empty file from positive growth
// without converting that proof byte into semantic content capacity.
func (budget *PhysicalWorkBudget) ReserveGeneralFileObservation(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.generalExecutionBegun {
		return fmt.Errorf("general recovery execution capacity was already transferred")
	}
	if work.entries != 0 {
		return fmt.Errorf("general file observation cannot contain descendant entries")
	}
	if err := budget.AdmitTree(work); err != nil {
		return err
	}
	budget.reservedGeneralBytes += work.bytes
	budget.reservedGeneralProbeBytes += max(int64(1), work.bytes) - work.bytes
	return nil
}

// ReserveGeneralDirectoryObservation charges one future exact directory
// snapshot plus the overflow name needed to reject descendant growth.
func (budget *PhysicalWorkBudget) ReserveGeneralDirectoryObservation(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.generalExecutionBegun {
		return fmt.Errorf("general recovery execution capacity was already transferred")
	}
	if work.entries >= budget.RemainingEntries() {
		return fmt.Errorf(
			"general recovery entries exceed operation limit %d",
			budget.entryLimit,
		)
	}
	if err := budget.AdmitTree(work); err != nil {
		return err
	}
	budget.entries++
	budget.reservedGeneralEntries += work.entries + 1
	budget.reservedGeneralBytes += work.bytes
	return nil
}

// ReserveCleanupLifecycle charges the complete post-effect cleanup preflight
// and execution envelope for one removal intent. The reservation is purely
// structural: it performs no filesystem observation and grants no mutation
// authority.
func (budget *PhysicalWorkBudget) ReserveCleanupLifecycle(
	destinationDepth int,
	residueDepth int,
	cleanupDepth int,
	absenceObservations int,
) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.cleanupExecutionBegun || budget.generalExecutionBegun {
		return fmt.Errorf("removal cleanup lifecycle capacity was already transferred")
	}
	reserved := *budget
	startObservations := reserved.observations
	startPathComponents := reserved.pathComponents

	reserveObservation := func() error {
		if err := reserved.AdmitObservation(); err != nil {
			return err
		}
		return nil
	}
	for range 3 {
		if err := reserveObservation(); err != nil {
			return err
		}
	}
	// Cleanup preflight recaptures the namespace and observes both slots. Every
	// path charge corresponds to a retained-root validation or storage traversal.
	for range forwardRemovalNamespacePasses {
		if err := reserved.AdmitPathComponents(destinationDepth); err != nil {
			return err
		}
	}
	for _, depth := range []int{residueDepth, cleanupDepth} {
		for range removalSlotObservationPasses {
			if err := reserved.AdmitPathComponents(depth); err != nil {
				return err
			}
		}
	}
	if err := reserved.ReserveExecutionObservations(
		destinationDepth,
		residueDepth,
		cleanupDepth,
		absenceObservations,
	); err != nil {
		return err
	}
	reserved.reservedCleanupObservations += reserved.observations - startObservations
	reserved.reservedCleanupPathComponents += reserved.pathComponents - startPathComponents
	*budget = reserved
	return nil
}

// BeginReservedCleanupLifecycle transfers the pre-effect cleanup envelope and
// all remaining tree capacity to one post-effect retirement capability.
func (budget *PhysicalWorkBudget) BeginReservedCleanupLifecycle() (*PhysicalWorkBudget, error) {
	if budget == nil {
		return nil, fmt.Errorf("physical work budget is required")
	}
	if budget.cleanupExecutionBegun {
		return nil, fmt.Errorf("removal cleanup lifecycle capacity was already transferred")
	}
	if budget.generalExecutionBegun {
		return nil, fmt.Errorf("removal cleanup lifecycle must be reserved before general recovery execution")
	}
	budget.cleanupExecutionBegun = true
	child := &PhysicalWorkBudget{
		observationLimit:   budget.reservedCleanupObservations,
		pathComponentLimit: budget.reservedCleanupPathComponents,
		entryLimit:         budget.RemainingEntries(),
		byteLimit:          budget.RemainingBytes(),
		probeByteLimit:     max(int64(0), budget.probeByteLimit-budget.probeBytes),
	}
	budget.entries = budget.entryLimit
	budget.bytes = budget.byteLimit
	budget.probeBytes = budget.probeByteLimit
	return child, nil
}

// ReserveBackupPathComponents charges path capacity that will be transferred
// to bounded recovery-backup execution.
func (budget *PhysicalWorkBudget) ReserveBackupPathComponents(count int) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.backupExecutionBegun {
		return fmt.Errorf("recovery backup execution capacity was already transferred")
	}
	before := budget.pathComponents
	if err := budget.AdmitPathComponents(count); err != nil {
		return err
	}
	budget.reservedBackupPathComponents += budget.pathComponents - before
	return nil
}

// ReserveBackupDirectoryExecution charges one future exact directory snapshot
// plus the overflow name needed to reject growth before publication.
func (budget *PhysicalWorkBudget) ReserveBackupDirectoryExecution(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.backupExecutionBegun {
		return fmt.Errorf("recovery backup execution capacity was already transferred")
	}
	if work.entries > MaximumArtifactTreeEntries || work.bytes > MaximumArtifactTreeBytes {
		return fmt.Errorf(
			"recovery backup work exceeds per-tree limit entries=%d bytes=%d",
			MaximumArtifactTreeEntries,
			MaximumArtifactTreeBytes,
		)
	}
	if work.entries >= budget.RemainingEntries() {
		return fmt.Errorf(
			"recovery backup entries exceed operation limit %d",
			budget.entryLimit,
		)
	}
	if work.bytes > budget.RemainingBytes() {
		return fmt.Errorf(
			"recovery backup bytes exceed operation limit %d",
			budget.byteLimit,
		)
	}
	budget.entries += work.entries + 1
	budget.bytes += work.bytes
	budget.reservedBackupEntries += work.entries + 1
	budget.reservedBackupBytes += work.bytes
	return nil
}

// ReserveBackupFileExecution charges the exact file bytes plus one bounded byte
// of growth-detection capacity for a future verified backup read.
func (budget *PhysicalWorkBudget) ReserveBackupFileExecution(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.backupExecutionBegun {
		return fmt.Errorf("recovery backup execution capacity was already transferred")
	}
	if work.entries != 0 || work.bytes > MaximumRecoveryBackupFileBytes {
		return fmt.Errorf(
			"recovery file backup work exceeds per-file limit entries=%d bytes=%d",
			work.entries,
			MaximumRecoveryBackupFileBytes,
		)
	}
	capacity := max(int64(1), work.bytes+1)
	if capacity > budget.RemainingBytes() {
		return fmt.Errorf(
			"recovery backup bytes exceed operation limit %d",
			budget.byteLimit,
		)
	}
	budget.bytes += capacity
	budget.reservedBackupBytes += capacity
	return nil
}

// BeginReservedBackupExecution transfers only capacity reserved before recovery
// effects into the rooted backup-copy phase.
func (budget *PhysicalWorkBudget) BeginReservedBackupExecution() (*PhysicalWorkBudget, error) {
	if budget == nil {
		return nil, fmt.Errorf("physical work budget is required")
	}
	if budget.backupExecutionBegun {
		return nil, fmt.Errorf("recovery backup execution capacity was already transferred")
	}
	budget.backupExecutionBegun = true
	return &PhysicalWorkBudget{
		pathComponentLimit: budget.reservedBackupPathComponents,
		entryLimit:         budget.reservedBackupEntries,
		byteLimit:          budget.reservedBackupBytes,
	}, nil
}

// NewPhysicalWorkBudget admits the complete removal-intent cardinality before
// current-path, ownership, backup, or cleanup observation and before effects.
func NewPhysicalWorkBudget(intentCount int) (*PhysicalWorkBudget, error) {
	if intentCount < 0 || intentCount > MaximumRemovalIntents {
		return nil, fmt.Errorf(
			"removal intent count %d exceeds operation maximum %d",
			intentCount,
			MaximumRemovalIntents,
		)
	}
	return &PhysicalWorkBudget{
		observationLimit:   intentCount * removalObservationsPerIntent,
		pathComponentLimit: maximumPhysicalPathComponentVisits,
		entryLimit:         MaximumPhysicalEntries,
		byteLimit:          MaximumPhysicalBytes,
	}, nil
}

// AdmitObservation charges one bounded namespace or slot observation.
func (budget *PhysicalWorkBudget) AdmitObservation() error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.observations >= budget.observationLimit {
		return fmt.Errorf(
			"removal observation count exceeds operation limit %d",
			budget.observationLimit,
		)
	}
	budget.observations++
	return nil
}

// AdmitPathComponents charges the already-normalized component work needed to
// bind or revalidate one physical namespace path. Physical path interpretation
// remains outside this wire-neutral package.
func (budget *PhysicalWorkBudget) AdmitPathComponents(depth int) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if depth < 0 {
		return fmt.Errorf("removal path-component work must not be negative")
	}
	if depth > MaximumPhysicalPathDepth {
		return fmt.Errorf(
			"removal path depth %d exceeds maximum %d",
			depth,
			MaximumPhysicalPathDepth,
		)
	}
	return budget.admitAggregatePathComponents(depth)
}

func (budget *PhysicalWorkBudget) admitAggregatePathComponents(count int) error {
	if count < 0 {
		return fmt.Errorf("removal aggregate path-component work must not be negative")
	}
	if count > budget.pathComponentLimit-budget.pathComponents {
		return fmt.Errorf(
			"removal path-component work exceeds operation limit %d",
			budget.pathComponentLimit,
		)
	}
	budget.pathComponents += count
	return nil
}

// ReserveExecutionObservations charges the exact worst-case observations and
// physical traversals performed by one ready cleanup candidate.
func (budget *PhysicalWorkBudget) ReserveExecutionObservations(
	destinationDepth int,
	residueDepth int,
	cleanupDepth int,
	absenceObservations int,
) error {
	if absenceObservations <= 0 {
		return fmt.Errorf("durable-absence observation count must be positive")
	}
	reserveObservations := func(count int) error {
		for range count {
			if err := budget.AdmitObservation(); err != nil {
				return err
			}
			budget.reservedExecutionObservations++
		}
		return nil
	}
	if err := reserveObservations(3 + 2*absenceObservations); err != nil {
		return err
	}
	reservePath := func(depth int, count int) error {
		for range count {
			before := budget.pathComponents
			if err := budget.AdmitPathComponents(depth); err != nil {
				return err
			}
			budget.reservedExecutionPathComponents += budget.pathComponents - before
		}
		return nil
	}
	// Namespace validation may recapture both persisted ancestor authority and
	// a newly appeared parent.
	if err := reservePath(destinationDepth, forwardRemovalNamespacePasses); err != nil {
		return err
	}
	// Both slots are observed before one action is selected.
	for _, depth := range []int{residueDepth, cleanupDepth} {
		if err := reservePath(depth, removalSlotObservationPasses); err != nil {
			return err
		}
	}
	// Promotion followed by cleanup is the maximal mutation path.
	for _, depth := range []int{residueDepth, cleanupDepth} {
		if err := reservePath(depth, removalSlotMutationPasses); err != nil {
			return err
		}
	}
	// Both slots receive a durable absence proof after mutation.
	for _, depth := range []int{residueDepth, cleanupDepth} {
		if err := reservePath(depth, removalSlotAbsencePasses); err != nil {
			return err
		}
	}
	return nil
}

// RemainingEntries returns the aggregate descendant-entry capacity.
func (budget *PhysicalWorkBudget) RemainingEntries() int {
	if budget == nil || budget.entries >= budget.entryLimit {
		return 0
	}
	return budget.entryLimit - budget.entries
}

// RemainingBytes returns the aggregate regular-file byte capacity.
func (budget *PhysicalWorkBudget) RemainingBytes() int64 {
	if budget == nil || budget.bytes >= budget.byteLimit {
		return 0
	}
	return budget.byteLimit - budget.bytes
}

// RemainingTreeWork returns the aggregate work still admitted by this budget.
func (budget *PhysicalWorkBudget) RemainingTreeWork() ArtifactWork {
	return ArtifactWork{
		entries: budget.RemainingEntries(),
		bytes:   budget.RemainingBytes(),
	}
}

// AdmitTree charges one completed bounded tree or file observation.
func (budget *PhysicalWorkBudget) AdmitTree(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if work.entries > budget.RemainingEntries() {
		return fmt.Errorf(
			"removal traversal entries exceed operation limit %d",
			budget.entryLimit,
		)
	}
	if work.bytes > budget.RemainingBytes() {
		return fmt.Errorf(
			"removal traversal bytes exceed operation limit %d",
			budget.byteLimit,
		)
	}
	budget.entries += work.entries
	budget.bytes += work.bytes
	return nil
}

// AdmitPhysicalWork atomically charges storage-owned path and tree work carried
// by a bounded rooted commit capability.
func (budget *PhysicalWorkBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	work, err := NewArtifactWork(entries, bytes)
	if err != nil {
		return err
	}
	reserved := *budget
	if err := reserved.admitAggregatePathComponents(pathComponents); err != nil {
		return err
	}
	if err := reserved.AdmitTree(work); err != nil {
		return err
	}
	*budget = reserved
	return nil
}

// AdmitTreeWithin charges one observation only when it remains within the
// preflight work of that exact entry. The operation budget remains an
// independent aggregate ceiling.
func (budget *PhysicalWorkBudget) AdmitTreeWithin(
	work ArtifactWork,
	maximum ArtifactWork,
) error {
	if work.entries > maximum.entries || work.bytes > maximum.bytes {
		return fmt.Errorf(
			"removal reobservation work exceeds its preflight maximum entries=%d bytes=%d",
			maximum.entries,
			maximum.bytes,
		)
	}
	return budget.AdmitTree(work)
}

// AdmitIndeterminateTreeWork conservatively charges both the full semantic
// maximum and any separately reserved reader overhead for an observation whose
// partial resource use cannot be measured.
func (budget *PhysicalWorkBudget) AdmitIndeterminateTreeWork(
	maximum ArtifactWork,
	readerCapacity ArtifactWork,
) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if readerCapacity.entries != maximum.entries || readerCapacity.bytes < maximum.bytes {
		return fmt.Errorf("removal reader capacity must cover its semantic maximum")
	}
	probeBytes := readerCapacity.bytes - maximum.bytes
	if maximum.entries > budget.RemainingEntries() {
		return fmt.Errorf(
			"removal traversal entries exceed operation limit %d",
			budget.entryLimit,
		)
	}
	if maximum.bytes > budget.RemainingBytes() {
		return fmt.Errorf(
			"removal traversal bytes exceed operation limit %d",
			budget.byteLimit,
		)
	}
	if probeBytes > budget.probeByteLimit-budget.probeBytes {
		return fmt.Errorf("removal reader probe exceeds reserved empty-proof capacity")
	}
	budget.entries += maximum.entries
	budget.bytes += maximum.bytes
	budget.probeBytes += probeBytes
	return nil
}

// AdmitIndeterminateDirectoryWork charges the complete descriptor enumeration
// capacity after an incomplete directory observation. Unlike empty-file proof
// bytes, the N+1 directory name is an actual operation entry visit.
func (budget *PhysicalWorkBudget) AdmitIndeterminateDirectoryWork(
	maximum ArtifactWork,
	readerCapacity ArtifactWork,
) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if readerCapacity.entries < maximum.entries || readerCapacity.bytes < maximum.bytes {
		return fmt.Errorf("removal directory reader capacity must cover its semantic maximum")
	}
	if readerCapacity.entries > budget.RemainingEntries() {
		return fmt.Errorf(
			"removal traversal entries exceed operation limit %d",
			budget.entryLimit,
		)
	}
	if maximum.bytes > budget.RemainingBytes() {
		return fmt.Errorf(
			"removal traversal bytes exceed operation limit %d",
			budget.byteLimit,
		)
	}
	budget.entries += readerCapacity.entries
	budget.bytes += maximum.bytes
	return nil
}

// ReserveReobservation charges the fresh effect-time observation that follows
// planning/preflight for every ready file or directory candidate.
func (budget *PhysicalWorkBudget) ReserveReobservation(work ArtifactWork) error {
	if err := budget.AdmitTree(work); err != nil {
		return err
	}
	budget.reservedReobservationEntries += work.entries
	budget.reservedReobservationBytes += work.bytes
	budget.reservedProbeBytes += max(int64(1), work.bytes) - work.bytes
	return nil
}

// ReserveDirectoryReobservation charges one fresh directory observation plus
// the extra name read only to prove an entry-count overflow. The overflow name
// is actual enumeration work but grants no additional semantic tree capacity.
func (budget *PhysicalWorkBudget) ReserveDirectoryReobservation(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if work.entries >= budget.RemainingEntries() {
		return fmt.Errorf(
			"removal traversal entries exceed operation limit %d",
			budget.entryLimit,
		)
	}
	if err := budget.AdmitTree(work); err != nil {
		return err
	}
	budget.entries++
	budget.reservedReobservationEntries += work.entries + 1
	budget.reservedReobservationBytes += work.bytes
	return nil
}

// ReserveRootedCleanup charges the complete storage and destination-parent
// validation envelope for one ready file or directory before any candidate
// begins an effect.
func (budget *PhysicalWorkBudget) ReserveRootedCleanup(
	work ArtifactWork,
	directory bool,
	parentValidationWork int,
) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	envelope, err := rootedCleanupWorkEnvelope(work, directory)
	if err != nil {
		return err
	}
	reserved := *budget
	if err := reserved.AdmitTree(ArtifactWork{
		entries: envelope.EntryWork(),
		bytes:   envelope.ByteWork(),
	}); err != nil {
		return fmt.Errorf("reserve rooted cleanup tree work: %w", err)
	}
	reserved.reservedCleanupEntries += envelope.EntryWork()
	reserved.reservedCleanupBytes += envelope.ByteWork()
	pathWork, err := envelope.PathWork(parentValidationWork)
	if err != nil {
		return fmt.Errorf("reserve rooted cleanup namespace: %w", err)
	}
	if err := reserved.admitAggregatePathComponents(pathWork); err != nil {
		return fmt.Errorf("reserve rooted cleanup namespace: %w", err)
	}
	reserved.reservedExecutionPathComponents += pathWork
	*budget = reserved
	return nil
}

// BeginReservedExecution realizes the aggregate effect-time re-observation
// capacity proved by preflight while preserving operation-wide namespace and
// path work already consumed. The child carries the exact storage work that
// bounded rooted cleanup consumes before touching its selected entry.
func (budget *PhysicalWorkBudget) BeginReservedExecution() (*PhysicalWorkBudget, error) {
	if budget == nil {
		return nil, fmt.Errorf("physical work budget is required")
	}
	return &PhysicalWorkBudget{
		observationLimit:   budget.reservedExecutionObservations,
		pathComponentLimit: budget.reservedExecutionPathComponents,
		entryLimit:         budget.reservedReobservationEntries + budget.reservedCleanupEntries,
		byteLimit:          budget.reservedReobservationBytes + budget.reservedCleanupBytes,
		probeByteLimit:     budget.reservedProbeBytes,
	}, nil
}

func rootedCleanupWorkEnvelope(
	work ArtifactWork,
	directory bool,
) (mutationfs.RootedCleanupWorkEnvelope, error) {
	kind := mutationfs.EntryKindFile
	if directory {
		kind = mutationfs.EntryKindDirectory
	}
	limits, err := mutationfs.NewTreeTraversalLimits(
		work.entries,
		MaximumArtifactTreeDepth,
		work.bytes,
	)
	if err != nil {
		return mutationfs.RootedCleanupWorkEnvelope{}, err
	}
	return mutationfs.NewRootedCleanupWorkEnvelope(kind, limits)
}
