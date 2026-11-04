package physicalwork

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func TestPhysicalWorkBudgetBoundsAggregateExecutionPasses(t *testing.T) {
	budget, err := NewBudget(2)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	observed, err := workmeasure.NewArtifactWork(44_444, MaximumPhysicalBytes/5)
	if err != nil {
		t.Fatalf("construct observed work: %v", err)
	}
	if err := budget.AdmitTree(observed); err != nil {
		t.Fatalf("admit observed work: %v", err)
	}
	if err := budget.ReserveDirectoryReobservation(observed); err != nil {
		t.Fatalf("reserve effect-time reobservation: %v", err)
	}
	if err := budget.ReserveRootedCleanup(observed, true, 0); err != nil {
		t.Fatalf("reserve directory cleanup: %v", err)
	}
	if budget.RemainingEntries() != 2 || budget.RemainingBytes() != MaximumPhysicalBytes%5 {
		t.Fatalf(
			"remaining budget = entries:%d bytes:%d, want entries:%d bytes:%d",
			budget.RemainingEntries(), budget.RemainingBytes(), 2, MaximumPhysicalBytes%5,
		)
	}
	if err := budget.ReserveDirectoryReobservation(observed); err == nil {
		t.Fatal("aggregate execution passes exceeded the operation budget")
	}
}

func TestPhysicalWorkBudgetReservesForwardObservationAndDirectoryPasses(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	work, err := workmeasure.NewArtifactWork(44_444, MaximumPhysicalBytes/5)
	if err != nil {
		t.Fatalf("construct forward work: %v", err)
	}
	if err := budget.AdmitTree(work); err != nil {
		t.Fatalf("charge capacity evidence: %v", err)
	}
	capacity, err := budget.ReserveForwardRemoval(work, true, 0)
	if err != nil {
		t.Fatalf("reserve forward removal: %v", err)
	}
	if budget.RemainingEntries() != 2 || budget.RemainingBytes() != MaximumPhysicalBytes%5 {
		t.Fatalf(
			"remaining budget = entries:%d bytes:%d, want 2/%d",
			budget.RemainingEntries(),
			budget.RemainingBytes(),
			MaximumPhysicalBytes%5,
		)
	}
	observation, err := capacity.BeginObservation()
	if err != nil {
		t.Fatalf("begin forward observation: %v", err)
	}
	if err := observation.AdmitObservation(); err != nil {
		t.Fatalf("admit forward observation: %v", err)
	}
	if err := observation.AdmitTree(work); err != nil {
		t.Fatalf("realize forward observation: %v", err)
	}
	if err := observation.AdmitTreeWithin(
		artifactWork(t, 1, 0),
		workmeasure.ArtifactWork{},
	); err == nil {
		t.Fatal("forward observation admitted overflow capacity as semantic work")
	}
	if err := observation.AdmitTree(work); err == nil {
		t.Fatal("forward observation exceeded reserved candidate work")
	}
}

func TestForwardRemovalCapacityKeepsZeroWorkProbeSeparate(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	empty, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatalf("construct empty work: %v", err)
	}
	capacity, err := budget.ReserveForwardRemoval(empty, true, 1)
	if err != nil {
		t.Fatalf("reserve empty forward removal: %v", err)
	}
	observation, err := capacity.BeginObservation()
	if err != nil {
		t.Fatalf("begin empty forward observation: %v", err)
	}
	if err := observation.AdmitObservation(); err != nil {
		t.Fatalf("admit empty forward observation: %v", err)
	}
	probe, err := workmeasure.NewArtifactWork(1, 0)
	if err != nil {
		t.Fatalf("construct probe work: %v", err)
	}
	if err := observation.AdmitIndeterminateDirectoryWork(empty, probe); err != nil {
		t.Fatalf("charge empty proof probe: %v", err)
	}
	if capacity.Admits(probe) {
		t.Fatal("zero-work capacity admitted positive semantic growth")
	}
}

func TestForwardRemovalCapacityRejectsPerTreeOverflowBeforeReservation(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	tooManyEntries, err := workmeasure.NewArtifactWork(recovery.MaximumArtifactTreeEntries+1, 0)
	if err != nil {
		t.Fatalf("construct excessive tree work: %v", err)
	}
	if _, err := budget.ReserveForwardRemoval(tooManyEntries, true, 1); err == nil {
		t.Fatal("forward removal admitted work beyond the per-tree entry limit")
	}
}

func TestForwardRemovalCapacityRejectsDescendantWorkForFile(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	work, err := workmeasure.NewArtifactWork(1, 0)
	if err != nil {
		t.Fatalf("construct invalid file work: %v", err)
	}
	if _, err := budget.ReserveForwardRemoval(work, false, 1); err == nil {
		t.Fatal("forward file removal admitted descendant-entry work")
	}
}

func TestForwardRemovalCapacityBoundsAggregateDirectoryPasses(t *testing.T) {
	budget, err := NewBudget(2)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	maximum, err := workmeasure.NewArtifactWork(44_444, MaximumPhysicalBytes/5)
	if err != nil {
		t.Fatalf("construct maximum directory work: %v", err)
	}
	if err := budget.AdmitTree(maximum); err != nil {
		t.Fatalf("charge maximum directory evidence: %v", err)
	}
	if _, err := budget.ReserveForwardRemoval(maximum, true, 0); err != nil {
		t.Fatalf("reserve maximum directory removal: %v", err)
	}
	positive, err := workmeasure.NewArtifactWork(0, budget.RemainingBytes()+1)
	if err != nil {
		t.Fatalf("construct positive directory work: %v", err)
	}
	if err := budget.AdmitTree(positive); err == nil {
		t.Fatal("operation budget admitted work after the complete cleanup envelope")
	}
}

func TestForwardRemovalCapacityBoundsRecursiveCleanupNamespaceWork(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	// Keep path work, rather than the independent entry limit, decisive here.
	const namespaceLimit = 524_288
	budget.pathComponentLimit = namespaceLimit
	maximum, err := workmeasure.NewArtifactWork(37_447, 0)
	if err != nil {
		t.Fatalf("construct namespace-bounded work: %v", err)
	}
	if _, err := budget.ReserveForwardRemoval(maximum, true, 1); err != nil {
		t.Fatalf("reserve exact namespace-bound cleanup: %v", err)
	}

	overflowBudget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct overflow work budget: %v", err)
	}
	overflowBudget.pathComponentLimit = namespaceLimit
	overflow, err := workmeasure.NewArtifactWork(37_448, 0)
	if err != nil {
		t.Fatalf("construct namespace overflow work: %v", err)
	}
	if _, err := overflowBudget.ReserveForwardRemoval(overflow, true, 1); err == nil {
		t.Fatal("forward cleanup admitted namespace work beyond the operation limit")
	}
}

func TestForwardRemovalCapacityEnvelopeDoesNotAuthorizeSyntheticWork(t *testing.T) {
	budget, err := NewBudget(2)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	entryWork, err := workmeasure.NewArtifactWork(3, 0)
	if err != nil {
		t.Fatalf("construct entry-heavy work: %v", err)
	}
	byteWork, err := workmeasure.NewArtifactWork(0, 5)
	if err != nil {
		t.Fatalf("construct byte-heavy work: %v", err)
	}
	entryCapacity, err := budget.ReserveForwardRemoval(entryWork, true, 1)
	if err != nil {
		t.Fatalf("reserve entry-heavy capacity: %v", err)
	}
	byteCapacity, err := budget.ReserveForwardRemoval(byteWork, false, 1)
	if err != nil {
		t.Fatalf("reserve byte-heavy capacity: %v", err)
	}
	envelope, err := entryCapacity.Envelope(byteCapacity)
	if err != nil {
		t.Fatalf("combine observation envelope: %v", err)
	}
	synthetic, err := workmeasure.NewArtifactWork(3, 5)
	if err != nil {
		t.Fatalf("construct synthetic work: %v", err)
	}
	if !envelope.Admits(synthetic) {
		t.Fatal("combined observation envelope did not cover independently reserved dimensions")
	}
	if entryCapacity.Admits(synthetic) || byteCapacity.Admits(synthetic) {
		t.Fatal("one state capacity authorized synthetic cross-state work")
	}
}

func TestPhysicalWorkBudgetCarriesReservedExecutionAndNamespaceWork(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := budget.AdmitObservation(); err != nil {
		t.Fatalf("admit preflight observation: %v", err)
	}
	if err := budget.AdmitPathComponents(2); err != nil {
		t.Fatalf("admit preflight path: %v", err)
	}
	if err := budget.ReserveExecutionObservations(2, 2, 2, mutationfs.RootedAbsencePathObservationCount); err != nil {
		t.Fatalf("reserve execution observations: %v", err)
	}
	work, err := workmeasure.NewArtifactWork(3, 7)
	if err != nil {
		t.Fatalf("construct removal work: %v", err)
	}
	if err := budget.ReserveReobservation(work); err != nil {
		t.Fatalf("reserve reobservation: %v", err)
	}
	execution, err := budget.BeginReservedExecution()
	if err != nil {
		t.Fatalf("begin reserved execution: %v", err)
	}
	if execution.RemainingEntries() != 3 || execution.RemainingBytes() != 7 {
		t.Fatalf(
			"reserved execution capacity = entries:%d bytes:%d, want 3/7",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
	for range 11 {
		if err := execution.AdmitObservation(); err != nil {
			t.Fatalf("realize reserved observation: %v", err)
		}
	}
	extraPasses := forwardRemovalNamespacePasses + 2*removalSlotObservationPasses +
		2*removalSlotMutationPasses + 2*removalSlotAbsencePasses
	for range extraPasses {
		if err := execution.AdmitPathComponents(2); err != nil {
			t.Fatalf("realize reserved cleanup path validation: %v", err)
		}
	}
	if err := execution.AdmitObservation(); err == nil {
		t.Fatal("reserved execution admitted unreserved observation")
	}
	if err := execution.AdmitPathComponents(1); err == nil {
		t.Fatal("reserved execution admitted unreserved path work")
	}
	if err := execution.AdmitTree(work); err != nil {
		t.Fatalf("realize reserved reobservation: %v", err)
	}
	if err := execution.AdmitTree(work); err == nil {
		t.Fatal("reserved execution admitted unreserved tree work")
	}
}

func TestPhysicalWorkBudgetTransfersReservedHostAndRemainingControlCapacity(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPathComponents(7); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupPathComponents(11); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveGeneralPathComponents(13); err != nil {
		t.Fatal(err)
	}
	emptyScratch, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveScratchCleanup(emptyScratch, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginReservedCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := budget.BeginReservedScratchCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	host, control, err := budget.BeginGeneralExecution()
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPathComponents(1); err == nil {
		t.Fatal("planning budget admitted general path work after transfer")
	}
	if _, _, err := budget.BeginGeneralExecution(); err == nil {
		t.Fatal("general execution capacity transferred twice")
	}
	if err := host.AdmitPathComponents(13); err != nil {
		t.Fatalf("host execution budget rejected reserved capacity: %v", err)
	}
	if err := host.AdmitPathComponents(1); err == nil {
		t.Fatal("host execution admitted unreserved path work")
	}
	if err := control.AdmitPathComponents(1); err != nil {
		t.Fatalf("control execution rejected remaining path capacity: %v", err)
	}
	backup, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatalf("begin reserved backup execution after general transfer: %v", err)
	}
	if err := backup.AdmitPathComponents(11); err != nil {
		t.Fatalf("reserved backup capacity was lost: %v", err)
	}
}

func TestPhysicalWorkBudgetRejectsRetirementPassesBeyondOperationCeiling(t *testing.T) {
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	work, err := workmeasure.NewArtifactWork(MaximumPhysicalEntries/6, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveRetirementDirectoryPasses(work, 6); err == nil {
		t.Fatal("retirement reservation exceeded the operation entry ceiling")
	}
	if _, err := budget.BeginReservedRetirementExecution(); err != nil {
		t.Fatalf("failed reservation changed retirement disposition: %v", err)
	}
}

func TestPhysicalWorkBudgetRequiresExplicitRetirementDisposition(t *testing.T) {
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginReservedCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := budget.BeginGeneralExecution(); err == nil {
		t.Fatal("general execution began with pending retirement capacity")
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveRetirementPathComponents(1); err == nil {
		t.Fatal("not-applicable retirement accepted a later reservation")
	}
	if _, _, err := budget.BeginGeneralExecution(); err != nil {
		t.Fatalf("general execution rejected explicit retirement disposition: %v", err)
	}
}

func TestPhysicalWorkBudgetRequiresExplicitScratchDisposition(t *testing.T) {
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := budget.BeginReservedScratchCleanup(); err == nil {
		t.Fatal("unreserved recovery scratch cleanup was transferred")
	}
	if err := budget.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveScratchCleanup(workmeasure.ArtifactWork{}, 1); err == nil {
		t.Fatal("not-applicable recovery scratch accepted a later reservation")
	}
}

func TestPhysicalWorkBudgetTransfersOnlyReservedSemanticPathCapacity(t *testing.T) {
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveSemanticPathComponents(7); err != nil {
		t.Fatalf("reserve semantic path work: %v", err)
	}
	semantic, err := budget.BeginReservedSemanticExecution()
	if err != nil {
		t.Fatalf("begin semantic execution: %v", err)
	}
	if err := budget.ReserveSemanticPathComponents(1); err == nil {
		t.Fatal("planning budget admitted semantic work after transfer")
	}
	if _, err := budget.BeginReservedSemanticExecution(); err == nil {
		t.Fatal("semantic execution capacity transferred twice")
	}
	if err := semantic.AdmitPathComponents(7); err != nil {
		t.Fatalf("semantic execution rejected reserved path work: %v", err)
	}
	if err := semantic.AdmitPathComponents(1); err == nil {
		t.Fatal("semantic execution admitted unreserved path work")
	}
}

func TestPhysicalWorkBudgetTransfersOnlyReservedGeneralContent(t *testing.T) {
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	emptyFile, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := workmeasure.NewArtifactWork(2, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveGeneralFileObservation(emptyFile); err != nil {
		t.Fatalf("reserve empty-file observation: %v", err)
	}
	if err := budget.ReserveGeneralDirectoryObservation(directory); err != nil {
		t.Fatalf("reserve directory observation: %v", err)
	}
	if err := budget.ReserveScratchCleanup(emptyFile, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginReservedCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := budget.BeginReservedScratchCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	host, _, err := budget.BeginGeneralExecution()
	if err != nil {
		t.Fatal(err)
	}
	emptyFileReader, err := workmeasure.NewArtifactWork(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AdmitIndeterminateTreeWork(emptyFile, emptyFileReader); err != nil {
		t.Fatalf("consume reserved empty-file proof: %v", err)
	}
	directoryReader, err := workmeasure.NewArtifactWork(3, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AdmitIndeterminateDirectoryWork(directory, directoryReader); err != nil {
		t.Fatalf("consume reserved directory observation: %v", err)
	}
	positiveFile, err := workmeasure.NewArtifactWork(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AdmitTree(positiveFile); err == nil {
		t.Fatal("host execution admitted unreserved content work")
	}
}

func TestPhysicalWorkBudgetTransfersOnlyReservedForwardPathWork(t *testing.T) {
	budget, err := NewBudget(2)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := budget.ReserveForwardExecutionPathWork([]int{3, 5}); err != nil {
		t.Fatalf("reserve forward path work: %v", err)
	}
	execution, err := budget.BeginReservedForwardExecution()
	if err != nil {
		t.Fatalf("begin reserved forward execution: %v", err)
	}
	passCount := forwardRemovalCapabilityPasses +
		forwardRemovalNamespacePasses +
		forwardRemovalCandidatePasses
	depths := make([]int, 0, passCount*2)
	for _, depth := range []int{3, 5} {
		for range passCount {
			depths = append(depths, depth)
		}
	}
	for index, depth := range depths {
		if index%passCount == 0 {
			if err := execution.AdmitObservation(); err != nil {
				t.Fatalf("consume forward observation %d: %v", index/passCount, err)
			}
		}
		if err := execution.AdmitPathComponents(depth); err != nil {
			t.Fatalf("consume forward path %d: %v", index, err)
		}
	}
	if err := execution.AdmitObservation(); err == nil {
		t.Fatal("forward execution admitted an unreserved observation")
	}
	if err := execution.AdmitPathComponents(1); err == nil {
		t.Fatal("forward execution admitted unreserved path work")
	}
	if execution.RemainingEntries() != 0 || execution.RemainingBytes() != 0 {
		t.Fatalf(
			"forward execution inherited content capacity entries:%d bytes:%d, want 0/0",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
	if _, err := budget.BeginReservedForwardExecution(); err == nil {
		t.Fatal("forward execution capacity was transferred more than once")
	}
	if err := budget.ReserveForwardExecutionPathWork([]int{1}); err == nil {
		t.Fatal("forward path capacity changed after execution transfer")
	}
}

func TestPhysicalWorkBudgetTransfersOnlyReservedBackupWork(t *testing.T) {
	budget, err := NewBudget(2)
	if err != nil {
		t.Fatal(err)
	}
	fileWork, err := workmeasure.NewArtifactWork(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	directoryWork, err := workmeasure.NewArtifactWork(2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupPathComponents(7); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupFileExecution(fileWork); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupDirectoryExecution(directoryWork); err != nil {
		t.Fatal(err)
	}

	execution, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatal(err)
	}
	if execution.pathComponentLimit != 7 || execution.entryLimit != 3 || execution.byteLimit != 9 {
		t.Fatalf(
			"backup execution limits = path:%d entries:%d bytes:%d, want 7/3/9",
			execution.pathComponentLimit,
			execution.entryLimit,
			execution.byteLimit,
		)
	}
	if err := execution.AdmitPathComponents(7); err != nil {
		t.Fatalf("consume exact backup path capacity: %v", err)
	}
	if err := execution.AdmitTree(fileWork); err != nil {
		t.Fatalf("consume file backup work: %v", err)
	}
	if err := execution.AdmitTree(directoryWork); err != nil {
		t.Fatalf("consume directory backup work: %v", err)
	}
	if execution.RemainingEntries() != 1 || execution.RemainingBytes() != 1 {
		t.Fatalf(
			"backup probe capacity = entries:%d bytes:%d, want 1/1",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
	if _, err := budget.BeginReservedBackupExecution(); err == nil {
		t.Fatal("backup execution capacity transferred twice")
	}
	if err := budget.ReserveBackupPathComponents(1); err == nil {
		t.Fatal("backup path capacity reserved after transfer")
	}
}

func TestPhysicalWorkBudgetReservesEmptyBackupProbeCapacity(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupFileExecution(zero); err != nil {
		t.Fatal(err)
	}
	execution, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatal(err)
	}
	if execution.entryLimit != 0 || execution.byteLimit != 1 {
		t.Fatalf(
			"empty backup execution limits = entries:%d bytes:%d, want 0/1",
			execution.entryLimit,
			execution.byteLimit,
		)
	}
	if err := execution.AdmitTree(zero); err != nil {
		t.Fatalf("admit empty backup semantic work: %v", err)
	}
}

func TestPhysicalWorkBudgetRejectsForwardPathReservationAtomically(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	remaining := budget.pathComponentLimit - 3
	for remaining > 0 {
		charge := min(remaining, MaximumPhysicalPathDepth)
		if err := budget.AdmitPathComponents(charge); err != nil {
			t.Fatalf("consume path capacity: %v", err)
		}
		remaining -= charge
	}
	before := *budget
	if err := budget.ReserveForwardExecutionPathWork([]int{1}); err == nil {
		t.Fatal("forward reservation exceeded remaining aggregate path capacity")
	}
	if *budget != before {
		t.Fatal("failed forward reservation partially consumed operation capacity")
	}
}

func TestPhysicalWorkBudgetReservesObservationCapacityForEmptyEntry(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := budget.ReserveExecutionObservations(1, 1, 1, mutationfs.RootedAbsencePathObservationCount); err != nil {
		t.Fatalf("reserve execution observations: %v", err)
	}
	empty, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatalf("construct empty removal work: %v", err)
	}
	if err := budget.ReserveReobservation(empty); err != nil {
		t.Fatalf("reserve empty reobservation: %v", err)
	}
	execution, err := budget.BeginReservedExecution()
	if err != nil {
		t.Fatalf("begin reserved execution: %v", err)
	}
	if execution.RemainingEntries() != 0 || execution.RemainingBytes() != 0 {
		t.Fatalf(
			"empty-entry semantic work = entries:%d bytes:%d, want 0/0",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
}

func TestPhysicalWorkBudgetChargesIndeterminateObservationMaximum(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	maximum, err := workmeasure.NewArtifactWork(7, 11)
	if err != nil {
		t.Fatalf("construct indeterminate observation maximum: %v", err)
	}
	if err := budget.AdmitIndeterminateTreeWork(maximum, maximum); err != nil {
		t.Fatalf("charge indeterminate observation: %v", err)
	}
	if budget.RemainingEntries() != MaximumPhysicalEntries-7 ||
		budget.RemainingBytes() != MaximumPhysicalBytes-11 {
		t.Fatalf(
			"remaining work = entries:%d bytes:%d, want entries:%d bytes:%d",
			budget.RemainingEntries(),
			budget.RemainingBytes(),
			MaximumPhysicalEntries-7,
			MaximumPhysicalBytes-11,
		)
	}
}

func TestPhysicalWorkBudgetChargesDirectoryOverflowProbeAsEntryWork(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	consumed, err := workmeasure.NewArtifactWork(MaximumPhysicalEntries-2, 0)
	if err != nil {
		t.Fatalf("construct consumed work: %v", err)
	}
	if err := budget.AdmitTree(consumed); err != nil {
		t.Fatalf("consume operation entry capacity: %v", err)
	}
	maximum, err := workmeasure.NewArtifactWork(1, 1)
	if err != nil {
		t.Fatalf("construct directory semantic maximum: %v", err)
	}
	readerCapacity, err := workmeasure.NewArtifactWork(2, 1)
	if err != nil {
		t.Fatalf("construct directory reader capacity: %v", err)
	}
	if err := budget.AdmitIndeterminateDirectoryWork(maximum, readerCapacity); err != nil {
		t.Fatalf("charge N+1 directory overflow evidence: %v", err)
	}
	if budget.RemainingEntries() != 0 {
		t.Fatalf("remaining entries = %d, want 0 after N+1 probe", budget.RemainingEntries())
	}
}

func TestPhysicalWorkBudgetAccumulatesEachDirectoryOverflowEntry(t *testing.T) {
	budget, err := NewBudget(4)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	maximum, err := workmeasure.NewArtifactWork(recovery.MaximumArtifactTreeEntries, 0)
	if err != nil {
		t.Fatalf("construct directory semantic maximum: %v", err)
	}
	readerCapacity, err := workmeasure.NewArtifactWork(recovery.MaximumArtifactTreeEntries+1, 0)
	if err != nil {
		t.Fatalf("construct directory reader capacity: %v", err)
	}
	for index := range 3 {
		if err := budget.AdmitIndeterminateDirectoryWork(maximum, readerCapacity); err != nil {
			t.Fatalf("charge directory overflow %d: %v", index, err)
		}
	}
	wantRemaining := MaximumPhysicalEntries - 3*(recovery.MaximumArtifactTreeEntries+1)
	if budget.RemainingEntries() != wantRemaining {
		t.Fatalf(
			"remaining entries = %d, want %d after three N+1 probes",
			budget.RemainingEntries(),
			wantRemaining,
		)
	}
	if err := budget.AdmitIndeterminateDirectoryWork(maximum, readerCapacity); err == nil {
		t.Fatal("aggregate budget admitted a fourth N+1 directory overflow")
	}
}

func TestPhysicalWorkBudgetSeparatelyAccountsForEmptyProofFailure(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	empty, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatalf("construct empty removal work: %v", err)
	}
	if err := budget.ReserveReobservation(empty); err != nil {
		t.Fatalf("reserve empty reobservation: %v", err)
	}
	execution, err := budget.BeginReservedExecution()
	if err != nil {
		t.Fatalf("begin reserved execution: %v", err)
	}
	probe, err := workmeasure.NewArtifactWork(0, 1)
	if err != nil {
		t.Fatalf("construct empty-proof probe: %v", err)
	}
	if err := execution.AdmitIndeterminateTreeWork(empty, probe); err != nil {
		t.Fatalf("charge reserved empty-proof failure: %v", err)
	}
	if execution.RemainingEntries() != 0 || execution.RemainingBytes() != 0 {
		t.Fatalf(
			"empty-proof failure changed semantic work = entries:%d bytes:%d",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
	if err := execution.AdmitIndeterminateTreeWork(empty, probe); err == nil {
		t.Fatal("empty-proof failure exceeded separately reserved probe capacity")
	}
}

func TestPhysicalWorkBudgetDerivesObservationCapacityFromIntentCount(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	for index := range removalObservationsPerIntent {
		if err := budget.AdmitObservation(); err != nil {
			t.Fatalf("admit observation %d: %v", index, err)
		}
	}
	if err := budget.AdmitObservation(); err == nil {
		t.Fatal("one-intent budget admitted an unowned observation")
	}
	maximum, err := NewBudget(recovery.MaximumRemovalIntents)
	if err != nil {
		t.Fatalf("construct maximum removal work budget: %v", err)
	}
	if maximum.observationLimit != 90_112 {
		t.Fatalf(
			"maximum physical observations = %d, want documented complete-lifecycle ceiling 90112",
			maximum.observationLimit,
		)
	}
}

func TestPhysicalWorkBudgetPartitionsGeneralAndCleanupExecution(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveCleanupLifecycle(
		4,
		5,
		5,
		mutationfs.RootedAbsencePathObservationCount,
	); err != nil {
		t.Fatalf("reserve cleanup lifecycle: %v", err)
	}
	if err := budget.ReserveGeneralPathComponents(1); err != nil {
		t.Fatalf("reserve general host path: %v", err)
	}
	emptyScratch, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveScratchCleanup(emptyScratch, 1); err != nil {
		t.Fatal(err)
	}
	cleanup, err := budget.BeginReservedCleanupLifecycle()
	if err != nil {
		t.Fatalf("begin cleanup lifecycle: %v", err)
	}
	if _, _, err := budget.BeginReservedScratchCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	host, control, err := budget.BeginGeneralExecution()
	if err != nil {
		t.Fatalf("begin general execution: %v", err)
	}
	if err := host.AdmitObservation(); err == nil {
		t.Fatal("general execution admitted cleanup observation work")
	}
	if err := host.AdmitTree(artifactWork(t, 1, 0)); err == nil {
		t.Fatal("general execution admitted cleanup tree work")
	}
	if err := host.AdmitPathComponents(1); err != nil {
		t.Fatalf("host execution rejected reserved path capacity: %v", err)
	}
	if err := control.AdmitPathComponents(1); err != nil {
		t.Fatalf("control execution rejected remaining path capacity: %v", err)
	}
	if err := cleanup.AdmitObservation(); err != nil {
		t.Fatalf("cleanup lifecycle rejected reserved observation: %v", err)
	}
	if err := cleanup.AdmitTree(artifactWork(t, 1, 0)); err != nil {
		t.Fatalf("cleanup lifecycle rejected reserved tree capacity: %v", err)
	}
}

func TestPhysicalWorkBudgetRejectsIntentCardinalityBeforeWork(t *testing.T) {
	if _, err := NewBudget(recovery.MaximumRemovalIntents + 1); err == nil {
		t.Fatal("removal work budget accepted excessive intent cardinality")
	}
}

func TestPhysicalWorkBudgetBoundsPathDepthAndFixedAggregateComponents(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := budget.AdmitPathComponents(MaximumPhysicalPathDepth + 1); err == nil {
		t.Fatal("removal work budget accepted excessive path depth")
	}

	fullDepthAdmissions := maximumPhysicalPathComponentVisits / MaximumPhysicalPathDepth
	for index := range fullDepthAdmissions {
		if err := budget.AdmitPathComponents(MaximumPhysicalPathDepth); err != nil {
			t.Fatalf("admit path within aggregate budget at %d: %v", index, err)
		}
	}
	if err := budget.AdmitPathComponents(1); err == nil {
		t.Fatal("removal work budget accepted aggregate path-component overflow")
	}
}
