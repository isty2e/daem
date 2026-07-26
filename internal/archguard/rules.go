package archguard

var importRules = []importRule{
	{
		rule:    "mutation package imports forbidden phase",
		subject: isMutationPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenDeclaration(),
			forbiddenIntent(),
			forbiddenSource(),
			forbiddenArtifact(),
			forbiddenLifecycle(),
			forbiddenTarget(),
			forbiddenResource(),
			forbiddenLock(),
			forbiddenLockfile(),
			forbiddenOutput(),
			forbiddenPayload(),
			forbiddenObserve(),
			forbiddenReconciliation(),
			forbiddenJournal(),
			forbiddenExecute(),
			forbiddenAdopt(),
			forbiddenDiagnose(),
			forbiddenPresent(),
			forbiddenWorkflow(),
			forbiddenCLI(),
			forbiddenStatefile(),
			forbiddenPaths(),
		},
	},
	{
		rule:    "project root authority imports forbidden boundary",
		subject: isProjectRootAuthorityPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenSurface(),
			forbiddenCompat(),
			forbiddenStorageCommit(),
		},
	},
	{
		rule:    "resource package imports forbidden phase",
		subject: isResourcePackage,
		forbiddenImports: []forbiddenImport{
			forbiddenDeclaration(),
			forbiddenIntent(),
			forbiddenSourceBackend(),
			forbiddenSurface(),
			forbiddenCompat(),
			forbiddenLockfile(),
			forbiddenLockBuild(),
			forbiddenOutput(),
			forbiddenPayload(),
			forbiddenObserve(),
			forbiddenReconciliation(),
			forbiddenJournal(),
			forbiddenExecute(),
			forbiddenAdopt(),
			forbiddenDiagnose(),
			forbiddenPresent(),
			forbiddenWorkflow(),
			forbiddenCLI(),
		},
	},
	{
		rule:    "declared resource package imports forbidden phase",
		subject: isDeclaredResourcePackage,
		forbiddenImports: []forbiddenImport{
			forbiddenDeclaration(),
			forbiddenIntent(),
			forbiddenSourceBackend(),
			forbiddenSurface(),
			forbiddenCompat(),
			forbiddenLockfile(),
			forbiddenLockBuild(),
			forbiddenOutput(),
			forbiddenPayload(),
			forbiddenObserve(),
			forbiddenReconciliation(),
			forbiddenJournal(),
			forbiddenExecute(),
			forbiddenAdopt(),
			forbiddenDiagnose(),
			forbiddenPresent(),
			forbiddenWorkflow(),
			forbiddenCLI(),
		},
	},
	{
		rule:    "declaration package imports forbidden phase",
		subject: isDeclarationPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenResourceFamily(),
			forbiddenSourceBackend(),
			forbiddenSurface(),
			forbiddenLock(),
			forbiddenOutput(),
			forbiddenPayload(),
			forbiddenObserve(),
			forbiddenReconciliation(),
			forbiddenJournal(),
			forbiddenExecute(),
			forbiddenAdopt(),
			forbiddenDiagnose(),
			forbiddenPresent(),
			forbiddenWorkflow(),
			forbiddenCLI(),
		},
	},
	{
		rule:    "target selection package imports forbidden phase",
		subject: isTargetSelectionPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenIntent(),
			forbiddenDeclaredResource(),
			forbiddenResourceFamily(),
			forbiddenSurface(),
			forbiddenDiagnose(),
			forbiddenPresent(),
			forbiddenWorkflow(),
			forbiddenCLI(),
		},
	},
	{
		rule:    "output package imports forbidden phase",
		subject: isOutputPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenSourceBackend(),
			forbiddenPayload(),
			forbiddenObserve(),
			forbiddenReconciliation(),
			forbiddenStatefile(),
			forbiddenJournal(),
			forbiddenExecute(),
			forbiddenPresent(),
			forbiddenCLI(),
		},
	},
	{
		rule:    "payload package imports forbidden phase",
		subject: isPayloadPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenReconciliation(),
			forbiddenExecute(),
			forbiddenDeclaration(),
			forbiddenIntent(),
			forbiddenOutputProject(),
			forbiddenLockBehavior(),
			forbiddenDiagnose(),
			forbiddenPresent(),
			forbiddenCLI(),
		},
	},
	{
		rule:    "reconciliation package imports forbidden phase",
		subject: isReconciliationPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenDeclaration(),
			forbiddenIntent(),
			forbiddenSource(),
			forbiddenSourceBackend(),
			forbiddenLockfile(),
			forbiddenObserveAdapters(),
			forbiddenOutputProject(),
			forbiddenPayload(),
			forbiddenJournal(),
			forbiddenExecute(),
			forbiddenPresent(),
			forbiddenWorkflow(),
			forbiddenCLI(),
		},
	},
	{
		rule:    "observe package imports forbidden phase",
		subject: isObserveRootPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenReconciliation(),
			forbiddenStatefile(),
			forbiddenLockfile(),
			forbiddenPaths(),
			forbiddenWorkflow(),
			forbiddenPresent(),
			forbiddenCLI(),
		},
	},
	{
		rule:    "observe adapter package imports forbidden phase",
		subject: isObserveAdapterPackage,
		forbiddenImports: []forbiddenImport{
			forbiddenOutputProject(),
		},
	},
	{
		rule:    "journal or execute package imports forbidden phase",
		subject: isJournalOrExecutePackage,
		forbiddenImports: []forbiddenImport{
			forbiddenDeclaration(),
			forbiddenIntent(),
			forbiddenSourceBackend(),
			forbiddenLockBuild(),
			forbiddenObserveAdapters(),
			forbiddenOutputProject(),
			forbiddenPayloadBuild(),
			forbiddenDiagnose(),
			forbiddenPresent(),
			forbiddenWorkflow(),
			forbiddenCLI(),
		},
	},
}

func forbiddenDeclaration() forbiddenImport {
	return forbiddenImport{name: "declaration", paths: []string{"internal/declaration"}}
}

func forbiddenIntent() forbiddenImport {
	return forbiddenImport{name: "intent", paths: []string{"internal/intent"}}
}

func forbiddenResourceFamily() forbiddenImport {
	return forbiddenImport{name: "resource family", paths: []string{"internal/resource/skill", "internal/resource/hook", "internal/resource/instructions"}}
}

func forbiddenResource() forbiddenImport {
	return forbiddenImport{name: "resource", paths: []string{"internal/resource"}}
}

func forbiddenLifecycle() forbiddenImport {
	return forbiddenImport{name: "lifecycle", paths: []string{"internal/lifecycle"}}
}

func forbiddenTarget() forbiddenImport {
	return forbiddenImport{name: "target", paths: []string{"internal/target"}}
}

func forbiddenDeclaredResource() forbiddenImport {
	return forbiddenImport{name: "declared resource", paths: []string{"internal/resource/declared"}}
}

func forbiddenSource() forbiddenImport {
	return forbiddenImport{name: "source", paths: []string{"internal/supply/source"}}
}

func forbiddenArtifact() forbiddenImport {
	return forbiddenImport{name: "artifact", paths: []string{"internal/supply/artifact"}}
}

func forbiddenSourceBackend() forbiddenImport {
	return forbiddenImport{name: "source backend", paths: []string{
		"internal/supply/source/backend",
		"internal/supply/source/backend/gitcli",
		"internal/supply/source/backend/localfs",
		"internal/supply/source/resolution",
		"internal/supply/source/backend/s3object",
	}}
}

func forbiddenSurface() forbiddenImport {
	return forbiddenImport{name: "surface", paths: []string{"internal/realization", "internal/target/surface"}}
}

func forbiddenCompat() forbiddenImport {
	return forbiddenImport{name: "compat", paths: []string{"internal/supply/compat/skill", "internal/resource/skill/compat"}}
}

func forbiddenLock() forbiddenImport {
	return forbiddenImport{name: "lock", paths: []string{"internal/realization/lock"}}
}

func forbiddenLockfile() forbiddenImport {
	return forbiddenImport{name: "lockfile", paths: []string{"internal/realization/lockfile"}}
}

func forbiddenLockBuild() forbiddenImport {
	return forbiddenImport{
		name: "lock build",
		paths: []string{
			"internal/realization/lock/build",
			"internal/workflow/lock/generate",
		},
	}
}

func forbiddenLockBehavior() forbiddenImport {
	return forbiddenImport{name: "lock behavior", paths: []string{"internal/realization/lock/build"}}
}

func forbiddenOutput() forbiddenImport {
	return forbiddenImport{name: "output", paths: []string{"internal/output", "internal/render", "internal/hostoutput"}}
}

func forbiddenOutputProject() forbiddenImport {
	return forbiddenImport{name: "output project", paths: []string{"internal/output/project", "internal/render"}}
}

func forbiddenPayload() forbiddenImport {
	return forbiddenImport{name: "payload", paths: []string{"internal/effect/payload", "internal/hostoutput"}}
}

func forbiddenPayloadBuild() forbiddenImport {
	return forbiddenImport{name: "payload build", paths: []string{"internal/effect/payload/build"}}
}

func forbiddenObserve() forbiddenImport {
	return forbiddenImport{name: "observe", paths: []string{"internal/assurance/observe"}}
}

func forbiddenObserveAdapters() forbiddenImport {
	return forbiddenImport{name: "observe adapter", paths: []string{
		"internal/assurance/observe/live",
		"internal/assurance/observe/lock",
	}}
}

func forbiddenReconciliation() forbiddenImport {
	return forbiddenImport{name: "reconciliation", paths: []string{"internal/reconcile"}}
}

func forbiddenJournal() forbiddenImport {
	return forbiddenImport{name: "journal", paths: []string{"internal/effect/journal"}}
}

func forbiddenExecute() forbiddenImport {
	return forbiddenImport{name: "execute", paths: []string{"internal/effect/execute"}}
}

func forbiddenAdopt() forbiddenImport {
	return forbiddenImport{name: "adopt", paths: []string{"internal/adopt", "internal/importer"}}
}

func forbiddenDiagnose() forbiddenImport {
	return forbiddenImport{name: "diagnose", paths: []string{"internal/diagnose"}}
}

func forbiddenPresent() forbiddenImport {
	return forbiddenImport{name: "present", paths: []string{"internal/cli/present"}}
}

func forbiddenWorkflow() forbiddenImport {
	return forbiddenImport{name: "workflow", paths: []string{"internal/workflow"}}
}

func forbiddenCLI() forbiddenImport {
	return forbiddenImport{name: "cli", paths: []string{"internal/cli"}}
}

func matchesForbiddenImport(importPath string, forbidden forbiddenImport) bool {
	if forbidden.name == "cli" && matchesInternalImport(importPath, "internal/cli/present") {
		return false
	}
	return matchesAnyInternalImport(importPath, forbidden.paths)
}

func forbiddenStatefile() forbiddenImport {
	return forbiddenImport{name: "statefile", paths: []string{"internal/assurance/statefile", "internal/state"}}
}

func forbiddenStorageCommit() forbiddenImport {
	return forbiddenImport{name: "storage commit", paths: []string{"internal/effect/storage/commit"}}
}

func forbiddenPaths() forbiddenImport {
	return forbiddenImport{name: "paths", paths: []string{"internal/paths"}}
}
