package record

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestValidateManifestRootProvenanceIsMandatoryForEveryJournalShape(t *testing.T) {
	journal := defaultRecoveryJournal()
	journal.ManifestRootProvenance = recoveryRootProvenance{}
	if err := validateManifestRootProvenance(journal); err == nil ||
		!strings.Contains(err.Error(), "physical root is required") {
		t.Fatalf("missing provenance error = %v", err)
	}

	journal = defaultRecoveryJournal()
	journal.Entries[0].Scope = string(target.ScopeGlobal)
	if err := validateManifestRootProvenance(journal); err != nil {
		t.Fatalf("global journal provenance error = %v", err)
	}

	journal = defaultRecoveryJournal()
	journal.Entries = nil
	if err := validateManifestRootProvenance(journal); err != nil {
		t.Fatalf("state-only journal provenance error = %v", err)
	}

	journal = defaultRecoveryJournal()
	journal.ManifestRootProvenance.ObjectFingerprint = "sha256:short"
	if err := validateManifestRootProvenance(journal); err == nil ||
		!strings.Contains(err.Error(), "object fingerprint is invalid") {
		t.Fatalf("invalid provenance error = %v", err)
	}
}

func TestValidateRecoveryJournalRejectsVersionThree(t *testing.T) {
	journal := defaultRecoveryJournal()
	journal.Version = 3
	if err := validateRecoveryJournal(journal, testStateCodec()); err == nil ||
		!strings.Contains(err.Error(), "unsupported recovery journal version 3") {
		t.Fatalf("version 3 error = %v", err)
	}
}
