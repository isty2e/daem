package cli_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type cliRecoveryJournalForTest struct {
	Version                int               `json:"version"`
	OperationID            string            `json:"operation_id"`
	Operation              string            `json:"operation"`
	CreatedAt              string            `json:"created_at"`
	ManifestRootProvenance json.RawMessage   `json:"manifest_root_provenance"`
	Entries                []json.RawMessage `json:"entries"`
	StatefileBefore        json.RawMessage   `json:"statefile_before"`
	StatefileAfter         json.RawMessage   `json:"statefile_after"`
	ClaimTransitions       []json.RawMessage `json:"claim_transitions,omitempty"`
}

func loadCLIRecoveryJournalForTest(t *testing.T, path string) cliRecoveryJournalForTest {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile journal returned error: %v", err)
	}

	var journal cliRecoveryJournalForTest
	if err := json.Unmarshal(content, &journal); err != nil {
		t.Fatalf("Unmarshal journal returned error: %v", err)
	}

	return journal
}

func recoveryJournalPaths(paths daempaths.Paths) journal.Paths {
	return journal.Paths{
		RecoveryDir:   paths.RecoveryDir,
		StatefilePath: paths.StatefilePath,
		ManifestRoot:  paths.ManifestRoot,
		DataDir:       paths.DataDir,
	}
}

func assertCLIRecoveryJournalActive(t *testing.T, recoveryDir string) {
	t.Helper()

	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		t.Fatalf("ReadDir recovery returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("recovery entries = %#v, want one active journal", entries)
	}
}
