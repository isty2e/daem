package archguard

import "testing"

func TestReadOnlyWorkflowBoundariesDoNotImportEffectOwners(t *testing.T) {
	const modulePath = "github.com/isty2e/daem/"

	readOnlyWorkflows := map[string]struct{}{
		modulePath + "internal/workflow/diagnose": {},
		modulePath + "internal/workflow/list":     {},
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
		if _, isReadOnlyWorkflow := readOnlyWorkflows[record.ImportPath]; !isReadOnlyWorkflow {
			continue
		}
		for _, imported := range record.Imports {
			internalImport, internal := internalPath(imported)
			if internal && matchesAnyInternalImport(internalImport, forbiddenImports) {
				t.Errorf("read-only workflow %q imports effect owner %q", record.ImportPath, imported)
			}
		}
	}
}
