package adopt

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	targetpkg "github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
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

// MergeResult is one merge decision for an imported resource. Subject identifies
// a projection-specific decision and remains zero for aggregate-level decisions.
type MergeResult struct {
	Resource string
	Subject  topology.SubjectID
	Status   MergeStatus
	Detail   string
}

// Validate rejects incomplete or unknown merge decisions.
func (result MergeResult) Validate() error {
	if result.Resource == "" || result.Detail == "" {
		return fmt.Errorf("merge result requires resource and detail")
	}
	if !result.Subject.IsZero() {
		if err := result.Subject.Validate(); err != nil {
			return fmt.Errorf("merge result subject: %w", err)
		}
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

// MCPSourceRoute separates one projection's logical content identity from the
// physical config files whose observed state authorized the import.
type MCPSourceRoute struct {
	PrimaryPath         string
	ContentPath         string
	RequiredAbsentPaths []string
}

// MCPSourceRouteInput carries one imported MCP projection source route.
type MCPSourceRouteInput struct {
	PrimaryPath         string
	ContentPath         string
	RequiredAbsentPaths []string
}

// NewMCPSourceRoute constructs one canonical MCP import source route.
func NewMCPSourceRoute(input MCPSourceRouteInput) (MCPSourceRoute, error) {
	requiredAbsentPaths := append([]string(nil), input.RequiredAbsentPaths...)
	sort.Strings(requiredAbsentPaths)
	unique := requiredAbsentPaths[:0]
	for _, candidate := range requiredAbsentPaths {
		if len(unique) == 0 || unique[len(unique)-1] != candidate {
			unique = append(unique, candidate)
		}
	}
	route := MCPSourceRoute{
		PrimaryPath:         input.PrimaryPath,
		ContentPath:         input.ContentPath,
		RequiredAbsentPaths: unique,
	}
	if err := route.validate(); err != nil {
		return MCPSourceRoute{}, err
	}
	return route, nil
}

func (route MCPSourceRoute) validate() error {
	if strings.TrimSpace(route.PrimaryPath) == "" || strings.TrimSpace(route.PrimaryPath) != route.PrimaryPath {
		return fmt.Errorf("primary config path must be non-empty and trimmed")
	}
	if filepath.Clean(route.PrimaryPath) != route.PrimaryPath {
		return fmt.Errorf("primary config path must be canonical")
	}
	if _, err := aggregate.ParseContentPath(route.ContentPath); err != nil {
		return fmt.Errorf("content path: %w", err)
	}
	for index, absentPath := range route.RequiredAbsentPaths {
		if strings.TrimSpace(absentPath) == "" || strings.TrimSpace(absentPath) != absentPath {
			return fmt.Errorf("required-absent path %d must be non-empty and trimmed", index)
		}
		if filepath.Clean(absentPath) != absentPath {
			return fmt.Errorf("required-absent path %q must be canonical", absentPath)
		}
		if absentPath == route.PrimaryPath {
			return fmt.Errorf("primary config path cannot also be required absent")
		}
		if index > 0 && route.RequiredAbsentPaths[index-1] >= absentPath {
			return fmt.Errorf("required-absent paths must be sorted and unique")
		}
	}
	return nil
}

// LivePath returns the existing boundary spelling used to disclose one MCP
// projection without treating it as a filesystem path.
func (route MCPSourceRoute) LivePath() string {
	return route.PrimaryPath + "#" + route.ContentPath
}

// MCPServer is one imported standalone MCP projection candidate. ResourceName
// names the desired server aggregate; target and scope identify this candidate's
// projection subject within that aggregate.
type MCPServer struct {
	ResourceName string
	Target       targetpkg.Target
	Scope        targetpkg.Scope
	SourceRoute  MCPSourceRoute
	Command      string
	Args         []string
	Env          map[string]string
}

// LivePath returns the projection-specific import disclosure path.
func (server MCPServer) LivePath() string { return server.SourceRoute.LivePath() }

func (server MCPServer) projectionSubject() (topology.SubjectID, error) {
	return topologymcp.ProjectionSubject(server.Target, server.Scope, server.ResourceName)
}

// Skill is one imported skill directory candidate.
type Skill struct {
	ResourceName string
	InstallName  string
	Target       targetpkg.Target
	Targets      []targetpkg.Target
	Placements   map[targetpkg.Target]string
	Scope        targetpkg.Scope
	SourceRoutes []SkillSourceRoute
	SourcePath   string
	GroupRoot    string
	ContentHash  artifact.ContentHash
}

// SkillSourceRoute is one target-specific live entry and its fully resolved
// artifact read route. Every route that contributed to a merged skill remains
// freshness authority for the resulting plan.
type SkillSourceRoute struct {
	Target   targetpkg.Target
	LivePath string
	ReadPath string
}

// PrimarySourceRoute returns the canonical route for the representative
// target used to materialize the planned artifact. All SourceRoutes remain
// freshness evidence.
func (skill Skill) PrimarySourceRoute() (SkillSourceRoute, error) {
	for _, route := range skill.SourceRoutes {
		if route.Target == skill.Target {
			return route, nil
		}
	}
	return SkillSourceRoute{}, fmt.Errorf(
		"skill representative target %q requires a source route",
		skill.Target,
	)
}

// ExpectedSourceIdentity returns the exact directory identity that execution
// must reproduce from the primary source route before publication.
func (skill Skill) ExpectedSourceIdentity() (artifact.ExactIdentity, error) {
	route, err := skill.PrimarySourceRoute()
	if err != nil {
		return artifact.ExactIdentity{}, err
	}
	if !filepath.IsAbs(route.ReadPath) || filepath.Clean(route.ReadPath) != route.ReadPath {
		return artifact.ExactIdentity{}, fmt.Errorf(
			"skill read path %q must be canonical and absolute",
			route.ReadPath,
		)
	}
	source, err := sourcepkg.NewLocalSource(route.ReadPath, sourcepkg.LocalSourceModeVendor)
	if err != nil {
		return artifact.ExactIdentity{}, fmt.Errorf("skill read source: %w", err)
	}
	sourceID, err := sourcepkg.SourceIDFor(source)
	if err != nil {
		return artifact.ExactIdentity{}, fmt.Errorf("skill read source identity: %w", err)
	}
	return artifact.NewExactIdentity(
		sourceID,
		"",
		artifact.ArtifactKindDirectory,
		skill.ContentHash,
	)
}
