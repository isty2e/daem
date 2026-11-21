package archguard

const (
	liveObserver = "internal/assurance/observe/live"
	lockObserver = "internal/assurance/observe/lock"

	ruleReconciliationForbiddenPhase   = "reconciliation package imports forbidden phase"
	ruleJournalOrExecuteForbiddenPhase = "journal or execute package imports forbidden phase"
	ruleEffectObserverImport           = "effect-observer-import"
)

func isLiveOrLockObserver(packagePath string) bool {
	return packagePath == liveObserver || packagePath == lockObserver
}

func observerImportRuleFor(packagePath string) string {
	switch {
	case isJournalOrExecutePackage(packagePath):
		return ruleJournalOrExecuteForbiddenPhase + ": observe adapter"
	case isReconciliationPackage(packagePath):
		return ruleReconciliationForbiddenPhase + ": observe adapter"
	}

	placement, placed := packagePlacementFor(packagePath)
	if placement.affinity == affinityEffect || (!placed && isPackageOrChild(packagePath, "internal/effect")) {
		return ruleEffectObserverImport
	}
	return ""
}

func isLiveObservationImport(importPath string) bool {
	if isPackageOrChild(importPath, liveObserver) || isPackageOrChild(importPath, lockObserver) {
		return true
	}

	placement, placed := packagePlacementFor(importPath)
	return placed && placement.affinity == affinityAssurance && placement.role == roleObservationAdapter
}
