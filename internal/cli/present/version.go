package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/isty2e/daem/internal/buildidentity"
	"github.com/isty2e/daem/internal/contractversion"
)

type versionJSONOutput struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Revision      string `json:"revision"`
	RevisionTime  string `json:"revision_time"`
	SourceState   string `json:"source_state"`
	VCS           string `json:"vcs"`
	GoVersion     string `json:"go_version"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
}

// PrintVersion writes one stable human-readable executable identity line.
func PrintVersion(output io.Writer, identity buildidentity.Identity) error {
	_, err := fmt.Fprintf(
		output,
		"daem version=%s revision=%s source=%s go=%s target=%s\n",
		knownBuildFact(identity.Version()),
		knownBuildFact(identity.Revision()),
		identity.SourceState(),
		knownBuildFact(identity.GoVersion()),
		identity.Target(),
	)
	return err
}

// PrintVersionJSON writes the schema-versioned executable identity document.
func PrintVersionJSON(output io.Writer, identity buildidentity.Identity) error {
	target := identity.Target()
	payload := versionJSONOutput{
		SchemaVersion: contractversion.VersionJSON,
		Version:       knownBuildFact(identity.Version()),
		Revision:      knownBuildFact(identity.Revision()),
		RevisionTime:  revisionTimeText(identity),
		SourceState:   identity.SourceState().String(),
		VCS:           knownBuildFact(identity.VCS()),
		GoVersion:     knownBuildFact(identity.GoVersion()),
		GOOS:          knownBuildFact(target.OS()),
		GOARCH:        knownBuildFact(target.Arch()),
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func revisionTimeText(identity buildidentity.Identity) string {
	revisionAt, ok := identity.RevisionTime()
	if !ok {
		return "unknown"
	}
	return revisionAt.UTC().Format(time.RFC3339Nano)
}

func knownBuildFact(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
