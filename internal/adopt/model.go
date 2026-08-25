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

// ErrNothingToImport reports that no selected live resource was importable.
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
// Target and Scope identify the selected observation route.
type Skipped struct {
	Target   targetpkg.Target
	Scope    targetpkg.Scope
	LivePath string
	Reason   SkipReason
	Detail   string
}

func UnsupportedSurfaceSkip(target targetpkg.Target, scope targetpkg.Scope, surface string) Skipped {
	return Skipped{
		Target:   target,
		Scope:    scope,
		LivePath: fmt.Sprintf("%s:%s:%s", target, scope, surface),
		Reason:   SkipReason("unsupported_" + surface + "_surface"),
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
	Evidence     ScanEvidence
}

// ScanEvidenceKind identifies the physical fact that must remain current for
// one scan-derived import decision.
type ScanEvidenceKind string

const (
	ScanEvidenceBoundedFile      ScanEvidenceKind = "bounded_file"
	ScanEvidenceDirectoryListing ScanEvidenceKind = "directory_listing"
)

// ScanEvidence defines the freshness observation required by one scanned
// source. MaximumBytes applies only to bounded regular files.
type ScanEvidence struct {
	Kind         ScanEvidenceKind
	MaximumBytes int64
}

// NewBoundedFileScanEvidence constructs freshness evidence for one absent or
// bounded regular inventory file.
func NewBoundedFileScanEvidence(maximumBytes int64) (ScanEvidence, error) {
	evidence := ScanEvidence{
		Kind:         ScanEvidenceBoundedFile,
		MaximumBytes: maximumBytes,
	}
	if err := evidence.validate(); err != nil {
		return ScanEvidence{}, err
	}
	return evidence, nil
}

// DirectoryListingScanEvidence constructs freshness evidence for one
// directory's immediate child inventory.
func DirectoryListingScanEvidence() ScanEvidence {
	return ScanEvidence{Kind: ScanEvidenceDirectoryListing}
}

func (evidence ScanEvidence) validate() error {
	switch evidence.Kind {
	case ScanEvidenceBoundedFile:
		if evidence.MaximumBytes <= 0 {
			return fmt.Errorf("bounded-file scan evidence requires a positive byte limit")
		}
	case ScanEvidenceDirectoryListing:
		if evidence.MaximumBytes != 0 {
			return fmt.Errorf("directory-listing scan evidence must not carry a byte limit")
		}
	default:
		return fmt.Errorf("scan evidence kind %q is invalid", evidence.Kind)
	}
	return nil
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
	PrimaryRevision     string
	MaximumBytes        int64
	ContentPath         string
	RequiredAbsentPaths []string
}

// MCPSourceRouteInput carries one imported MCP projection source route.
type MCPSourceRouteInput struct {
	PrimaryPath         string
	PrimaryRevision     string
	MaximumBytes        int64
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
		PrimaryRevision:     input.PrimaryRevision,
		MaximumBytes:        input.MaximumBytes,
		ContentPath:         input.ContentPath,
		RequiredAbsentPaths: unique,
	}
	if err := route.validate(); err != nil {
		return MCPSourceRoute{}, err
	}
	return route, nil
}

func (route MCPSourceRoute) validate() error {
	if err := validateMCPPhysicalSource(
		route.PrimaryPath,
		route.PrimaryRevision,
		route.MaximumBytes,
		route.RequiredAbsentPaths,
	); err != nil {
		return err
	}
	if _, err := aggregate.ParseContentPath(route.ContentPath); err != nil {
		return fmt.Errorf("content path: %w", err)
	}
	return nil
}

func validateMCPPhysicalSource(
	primaryPath string,
	primaryRevision string,
	maximumBytes int64,
	requiredAbsentPaths []string,
) error {
	if strings.TrimSpace(primaryPath) == "" || strings.TrimSpace(primaryPath) != primaryPath {
		return fmt.Errorf("primary config path must be non-empty and trimmed")
	}
	if filepath.Clean(primaryPath) != primaryPath {
		return fmt.Errorf("primary config path must be canonical")
	}
	if strings.TrimSpace(primaryRevision) == "" || strings.TrimSpace(primaryRevision) != primaryRevision {
		return fmt.Errorf("primary config revision must be non-empty and trimmed")
	}
	if maximumBytes <= 0 {
		return fmt.Errorf("primary config maximum bytes must be positive")
	}
	for index, absentPath := range requiredAbsentPaths {
		if strings.TrimSpace(absentPath) == "" || strings.TrimSpace(absentPath) != absentPath {
			return fmt.Errorf("required-absent path %d must be non-empty and trimmed", index)
		}
		if filepath.Clean(absentPath) != absentPath {
			return fmt.Errorf("required-absent path %q must be canonical", absentPath)
		}
		if absentPath == primaryPath {
			return fmt.Errorf("primary config path cannot also be required absent")
		}
		if index > 0 && requiredAbsentPaths[index-1] >= absentPath {
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

// MCPSourceAuthority is the exact physical document evidence that supports one
// MCP import decision, independently of whether any projection is writable.
type MCPSourceAuthority struct {
	Target              targetpkg.Target
	Scope               targetpkg.Scope
	PrimaryPath         string
	PrimaryRevision     string
	MaximumBytes        int64
	RequiredAbsentPaths []string
}

func (authority MCPSourceAuthority) validate() error {
	if err := validateTargetScope(authority.Target, authority.Scope); err != nil {
		return err
	}
	return validateMCPPhysicalSource(
		authority.PrimaryPath,
		authority.PrimaryRevision,
		authority.MaximumBytes,
		authority.RequiredAbsentPaths,
	)
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

func (server MCPServer) sourceAuthority() MCPSourceAuthority {
	return MCPSourceAuthority{
		Target:              server.Target,
		Scope:               server.Scope,
		PrimaryPath:         server.SourceRoute.PrimaryPath,
		PrimaryRevision:     server.SourceRoute.PrimaryRevision,
		MaximumBytes:        server.SourceRoute.MaximumBytes,
		RequiredAbsentPaths: cloneStrings(server.SourceRoute.RequiredAbsentPaths),
	}
}

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

// SkillSourceAuthority is the exact source evidence that supports one imported
// skill decision, independently of whether its artifact will be copied.
type SkillSourceAuthority struct {
	ResourceName string
	Scope        targetpkg.Scope
	ContentHash  artifact.ContentHash
	Routes       []SkillSourceRoute
}

// SkillSourceRoute is one target-specific live entry and its fully resolved
// artifact read route. Every route that contributed to a merged skill remains
// freshness authority for the resulting plan.
type SkillSourceRoute struct {
	Target   targetpkg.Target
	LivePath string
	ReadPath string
}

func (skill Skill) sourceAuthority() SkillSourceAuthority {
	return SkillSourceAuthority{
		ResourceName: skill.ResourceName,
		Scope:        skill.Scope,
		ContentHash:  skill.ContentHash,
		Routes:       append([]SkillSourceRoute(nil), skill.SourceRoutes...),
	}
}

// PrimarySourceRoute returns the canonical route for the representative
// target used to materialize the planned artifact. All SourceRoutes remain
// freshness evidence.
func (skill Skill) PrimarySourceRoute() (SkillSourceRoute, error) {
	routes, err := skill.CanonicalSourceRoutes()
	if err != nil {
		return SkillSourceRoute{}, fmt.Errorf("skill source routes: %w", err)
	}
	for _, route := range routes {
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
