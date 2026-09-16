package clipresent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

func TestStateMigrationPresentationPreservesPathsWithoutTerminalControls(t *testing.T) {
	plan := migrate.StateDisclosure{
		Action: "migrate", ManifestPath: "/manifest\n\x1b[31m", SourceStatefile: "/old\tstate",
		DestinationStatefile: "/new", OutputRegistryPath: "/outputs", CarrierRegistryPath: "/carriers",
	}
	var human, encoded bytes.Buffer
	PrintStateMigration(&human, "dry-run", plan)
	if strings.Contains(human.String(), "\x1b") || strings.Contains(human.String(), "/manifest\n") || strings.Contains(human.String(), "/old\t") {
		t.Fatalf("terminal controls escaped disclosure: %q", human.String())
	}
	if err := PrintStateMigrationJSON(&encoded, "dry-run", plan, nil); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SchemaVersion int    `json:"schema_version"`
		Manifest      string `json:"manifest_path"`
		Source        string `json:"source_statefile"`
		OutputClaims  *int   `json:"output_claims"`
	}
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != contractversion.StateMigrationJSON || decoded.Manifest != plan.ManifestPath || decoded.Source != plan.SourceStatefile || decoded.OutputClaims == nil || *decoded.OutputClaims != 0 {
		t.Fatalf("JSON lost identity or known-empty counts: %s", encoded.String())
	}

	encoded.Reset()
	plan.Action = "rollback"
	if err := PrintStateMigrationJSON(&encoded, "dry-run", plan, nil); err != nil {
		t.Fatal(err)
	}
	decoded.OutputClaims = nil
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OutputClaims != nil {
		t.Fatal("recovery invented a known-empty claim count")
	}
}
