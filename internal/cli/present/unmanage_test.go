package clipresent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/target"
	authoringworkflow "github.com/isty2e/daem/internal/workflow/authoring"
)

func TestUnmanageExtensionProjectionDisclosesHostAndManagementBoundaries(t *testing.T) {
	result := authoringworkflow.UnmanageExtensionResult{
		ManifestPath:                 "/repo/daem.toml",
		LockfilePath:                 "/repo/daem.lock.toml",
		StatefilePath:                "/state/project.json",
		RegistryPath:                 "/state/global.json",
		ResourceID:                   "context7",
		Target:                       target.TargetClaudeCode,
		Scope:                        target.ScopeGlobal,
		ManifestStatus:               authoringworkflow.UnmanageManifestStatusWouldRemove,
		LockfileStatus:               authoringworkflow.LockfileStatusWouldWrite,
		ManagementStatus:             authoringworkflow.UnmanageManagementStatusWouldRelease,
		StatefileStatus:              authoringworkflow.UnmanageStateStatusUnchanged,
		RegistryStatus:               authoringworkflow.UnmanageStateStatusWouldWrite,
		HostStateRetained:            true,
		AmbientConsumersUnobservable: true,
		DeclarationPresent:           true,
		Mode:                         authoringworkflow.UnmanageModeDryRun,
	}

	var human bytes.Buffer
	PrintUnmanageExtensionWithOptions(&human, result, HumanOptions{Verbose: true})
	for _, want := range []string{
		"unmanage: extension/context7",
		"manifest: would remove /repo/daem.toml",
		"lockfile: would write /repo/daem.lock.toml",
		"management: would release",
		"host: retained",
		"ambient consumers: unobservable",
		"statefile: unchanged /state/project.json",
		"registry: would_write /state/global.json",
		"next: rerun this unmanage command without --dry-run",
	} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("human output = %q, want %q", human.String(), want)
		}
	}

	var encoded bytes.Buffer
	if err := PrintManifestAuthoringJSON(&encoded, UnmanageExtensionJSONFrom(result)); err != nil {
		t.Fatalf("PrintManifestAuthoringJSON: %v", err)
	}
	var payload struct {
		SchemaVersion int    `json:"schema_version"`
		Command       string `json:"command"`
		Mode          string `json:"mode"`
		Operation     string `json:"operation"`
		Changes       []struct {
			Operation  string `json:"operation"`
			ChangeKind string `json:"change_kind"`
			Status     string `json:"status"`
			ResourceID string `json:"resource_id"`
			Target     string `json:"target"`
			Scope      string `json:"scope"`
		} `json:"changes"`
		Management struct {
			Status    string `json:"status"`
			Statefile struct {
				Path   string `json:"path"`
				Status string `json:"status"`
			} `json:"statefile"`
			Registry struct {
				Path   string `json:"path"`
				Status string `json:"status"`
			} `json:"registry"`
		} `json:"management"`
		Host struct {
			State            string `json:"state"`
			AmbientConsumers string `json:"ambient_consumers"`
		} `json:"host"`
	}
	if err := json.Unmarshal(encoded.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.SchemaVersion != contractversion.ManifestAuthoringJSON ||
		payload.Command != "unmanage" ||
		payload.Mode != "dry-run" ||
		payload.Operation != "unmanage" ||
		len(payload.Changes) != 1 {
		t.Fatalf("payload = %#v, want schema-v2 unmanage envelope", payload)
	}
	change := payload.Changes[0]
	if change.Operation != "unmanage" ||
		change.ChangeKind != "would_remove" ||
		change.Status != "would_release" ||
		change.ResourceID != "extension/context7" ||
		change.Target != "claude-code" ||
		change.Scope != "global" {
		t.Fatalf("change = %#v, want exact unmanage projection", change)
	}
	if payload.Management.Status != "would_release" ||
		payload.Management.Statefile.Path != "/state/project.json" ||
		payload.Management.Statefile.Status != "unchanged" ||
		payload.Management.Registry.Path != "/state/global.json" ||
		payload.Management.Registry.Status != "would_write" ||
		payload.Host.State != "retained" ||
		payload.Host.AmbientConsumers != "unobservable" {
		t.Fatalf("management/host projection = %#v %#v", payload.Management, payload.Host)
	}
}

func TestUnmanageExtensionHumanWriteDoesNotSuggestApply(t *testing.T) {
	var output bytes.Buffer
	PrintUnmanageExtensionWithOptions(
		&output,
		authoringworkflow.UnmanageExtensionResult{
			ManifestPath:      "/repo/daem.toml",
			LockfilePath:      "/repo/daem.lock.toml",
			ResourceID:        "context7",
			ManifestStatus:    authoringworkflow.UnmanageManifestStatusRemoved,
			LockfileStatus:    authoringworkflow.LockfileStatusWritten,
			ManagementStatus:  authoringworkflow.UnmanageManagementStatusNotPresent,
			HostStateRetained: true,
			Mode:              authoringworkflow.UnmanageModeWrite,
		},
		HumanOptions{},
	)
	if strings.Contains(output.String(), "daem apply") ||
		!strings.Contains(output.String(), "host: retained") ||
		!strings.Contains(output.String(), "management: not present") {
		t.Fatalf("write output = %q, want terminal unmanage result without apply suggestion", output.String())
	}
}
