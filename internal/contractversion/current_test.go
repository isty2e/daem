package contractversion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicReferencesUseCurrentContractVersions(t *testing.T) {
	t.Parallel()

	manifestReference := readReference(t, "manifest.md")
	for _, want := range []string{
		fmt.Sprintf("The current schema version is `%d`.", ManifestSchema),
		fmt.Sprintf("| `version` | integer | yes | Must be `%d`. |", ManifestSchema),
		fmt.Sprintf("Lockfile readers accept only schema version %d", LockfileSchema),
	} {
		if !strings.Contains(manifestReference, want) {
			t.Errorf("manifest reference is missing current contract text %q", want)
		}
	}

	troubleshooting := readReference(t, "troubleshooting.md")
	transactionVersion := fmt.Sprintf(
		"Metadata transaction marker version %d enforces bounded declaration recovery.",
		MetadataTransaction,
	)
	if !strings.Contains(troubleshooting, transactionVersion) {
		t.Errorf("troubleshooting reference is missing current contract text %q", transactionVersion)
	}

	cliReference := readReference(t, "cli.md")
	rows := []struct {
		surface string
		owner   string
		version int
	}{
		{surface: "`version`", owner: "Executable identity", version: VersionJSON},
		{surface: "`init`", owner: "Manifest initialization", version: InitJSON},
		{surface: "`add`, `remove`, `import`, `unmanage extension`", owner: "Manifest authoring", version: ManifestAuthoringJSON},
		{surface: "`lock`, `outdated`", owner: "Lock comparison", version: LockComparisonJSON},
		{surface: "`list resources`", owner: "Resource inventory", version: ResourceInventoryJSON},
		{surface: "`list outputs`", owner: "Output inventory", version: OutputInventoryJSON},
		{surface: "`list paths`", owner: "Agent location inventory", version: PathInventoryJSON},
		{surface: "`status`, `apply --dry-run`", owner: "Reconciliation plan", version: ReconciliationPlanJSON},
		{surface: "confirmed `apply`", owner: "Apply result", version: ApplyResultJSON},
		{surface: "`recover`", owner: "Recovery plan/result", version: RecoveryJSON},
		{surface: "`doctor`", owner: "Passive diagnostics", version: DoctorJSON},
		{surface: "`probe mcp-server`", owner: "Runtime probe", version: MCPProbeJSON},
		{surface: "`refresh extension`", owner: "Extension refresh", version: ExtensionRefreshJSON},
	}
	for _, row := range rows {
		want := fmt.Sprintf("| %s | %s | `%d` |", row.surface, row.owner, row.version)
		if !strings.Contains(cliReference, want) {
			t.Errorf("CLI reference is missing current contract row %q", want)
		}
	}
}

func readReference(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
