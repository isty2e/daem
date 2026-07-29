package adopt

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact"
	targetpkg "github.com/isty2e/daem/internal/target"
)

// ErrNothingToImport reports that all selected live roots were absent, empty, or unsupported.
var ErrNothingToImport = errors.New("nothing to import")

// MergeStatus classifies how an imported resource relates to an existing manifest.
type MergeStatus string

const (
	MergeStatusAdd          MergeStatus = "add"
	MergeStatusMergeTargets MergeStatus = "merge_targets"
	MergeStatusNoop         MergeStatus = "noop"
	MergeStatusConflict     MergeStatus = "conflict"
)

// MergeResult is one merge decision for an imported resource.
type MergeResult struct {
	Resource string
	Status   MergeStatus
	Detail   string
}

// Validate rejects incomplete or unknown merge decisions.
func (result MergeResult) Validate() error {
	if result.Resource == "" || result.Detail == "" {
		return fmt.Errorf("merge result requires resource and detail")
	}
	switch result.Status {
	case MergeStatusAdd, MergeStatusMergeTargets, MergeStatusNoop, MergeStatusConflict:
		return nil
	default:
		return fmt.Errorf("merge result status %q is invalid", result.Status)
	}
}

// SummaryRow aggregates imported resources for one target and scope.
type SummaryRow struct {
	Target       targetpkg.Target
	Scope        targetpkg.Scope
	Instructions int
	Skills       int
	Hooks        int
	MCPServers   int
	Extensions   int
}

// Source is one imported instruction source file.
type Source struct {
	ResourceName string
	Target       targetpkg.Target
	Scope        targetpkg.Scope
	LivePath     string
	SourcePath   string
	RenderTo     string
	Content      []byte
}

// Skipped records one live import candidate that was not importable.
type Skipped struct {
	LivePath string
	Reason   string
}

func UnsupportedSurfaceSkip(target targetpkg.Target, scope targetpkg.Scope, surface string) Skipped {
	return Skipped{
		LivePath: fmt.Sprintf("%s:%s:%s", target, scope, surface),
		Reason:   "unsupported_" + surface + "_surface",
	}
}

// Scan summarizes one scanned live resource root.
type Scan struct {
	ResourceKind string
	ResourceName string
	Target       targetpkg.Target
	Scope        targetpkg.Scope
	LivePath     string
	Status       string
	Entries      int
	Imported     int
	Skipped      int
}

// Hook is one imported command hook.
type Hook struct {
	ResourceName  string
	Target        targetpkg.Target
	Scope         targetpkg.Scope
	LivePath      string
	Event         string
	Matcher       string
	Command       string
	Timeout       int
	StatusMessage string
	Condition     string
}

// MCPServer is one imported standalone MCP declaration candidate.
type MCPServer struct {
	ResourceName string
	Target       targetpkg.Target
	Scope        targetpkg.Scope
	LivePath     string
	Command      string
	Args         []string
	Env          map[string]string
}

// Skill is one imported skill directory candidate.
type Skill struct {
	ResourceName string
	InstallName  string
	Target       targetpkg.Target
	Targets      []targetpkg.Target
	Placements   map[targetpkg.Target]string
	Scope        targetpkg.Scope
	LivePath     string
	ReadPath     string
	SourcePath   string
	GroupRoot    string
	ContentHash  artifact.ContentHash
}
