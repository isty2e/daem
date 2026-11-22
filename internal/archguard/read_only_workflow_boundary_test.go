package archguard

import "testing"

func TestReadOnlyWorkflowBoundariesDoNotImportEffectOwners(t *testing.T) {
	readOnlyWorkflows := map[string]struct{}{
		"internal/workflow/diagnose": {},
		"internal/workflow/list":     {},
	}
	forbiddenImports := []string{
		"internal/effect/execute",
		"internal/effect/mutation",
		"internal/assurance/statefile",
		"internal/effect/storage/commit",
		"internal/workflow/apply",
		"internal/workflow/recover",
	}

	for _, record := range loadRepoPackageRecords(t) {
		packagePath, _ := record.Module.internalPath(record.ImportPath)
		if _, isReadOnlyWorkflow := readOnlyWorkflows[packagePath]; !isReadOnlyWorkflow {
			continue
		}
		for _, imported := range record.Imports {
			internalImport, internal := record.Module.internalPath(imported)
			if internal && matchesAnyInternalImport(internalImport, forbiddenImports) {
				t.Errorf("read-only workflow %q imports effect owner %q", record.ImportPath, imported)
			}
		}
	}
}
