package probe

import (
	"context"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestPrepareRefusesInterruptedMetadataTransaction(t *testing.T) {
	project := newProbeWorkflowProject(t)
	writeProbeWorkflowManifest(t, project.root, "node", []string{"server.js"}, nil)
	writeProbeWorkflowLock(t, project)
	paths, err := daempaths.Resolve(project.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	_, err = Prepare(context.Background(), CommandInput{
		ServerName:   "context7",
		ManifestPath: project.manifestPath,
		LockfilePath: project.lockfilePath,
		TargetValue:  "claude-code",
		ScopeValue:   "project",
		Mode:         ModeDryRun,
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
}
