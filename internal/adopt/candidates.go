package adopt

import (
	"fmt"
	"path/filepath"
	"strings"

	adoptextension "github.com/isty2e/daem/internal/adopt/extension"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	targetpkg "github.com/isty2e/daem/internal/target"
)

// CandidateSetInput names each independent import candidate and observation
// axis.
type CandidateSetInput struct {
	Sources    []Source
	Skills     []Skill
	Hooks      []Hook
	MCPServers []MCPServer
	Extensions adoptextension.Result
	Scans      []Scan
	Skipped    []Skipped
}

// CandidateSet is one validated, immutable collection of live import facts.
type CandidateSet struct {
	sources         []Source
	skills          []Skill
	hooks           []Hook
	mcpServers      []MCPServer
	extensionResult adoptextension.Result
	scans           []Scan
	skipped         []Skipped
}

// NewCandidateSet validates and owns all candidate and observation facts.
func NewCandidateSet(input CandidateSetInput) (CandidateSet, error) {
	for index, source := range input.Sources {
		if err := validateSource(source); err != nil {
			return CandidateSet{}, fmt.Errorf("source candidate %d: %w", index, err)
		}
	}
	for index, skill := range input.Skills {
		if err := validateSkill(skill); err != nil {
			return CandidateSet{}, fmt.Errorf("skill candidate %d: %w", index, err)
		}
	}
	if err := validateSkillSourceCorrelations(input.Skills); err != nil {
		return CandidateSet{}, err
	}
	for index, hook := range input.Hooks {
		if err := validateHook(hook); err != nil {
			return CandidateSet{}, fmt.Errorf("hook candidate %d: %w", index, err)
		}
	}
	for index, server := range input.MCPServers {
		if err := validateMCPServer(server); err != nil {
			return CandidateSet{}, fmt.Errorf("mcp server candidate %d: %w", index, err)
		}
	}
	for index, extension := range input.Extensions.Extensions() {
		if err := extension.Validate(); err != nil {
			return CandidateSet{}, fmt.Errorf(
				"extension candidate %d: %w",
				index,
				err,
			)
		}
	}
	for index, scan := range input.Scans {
		if err := validateScan(scan); err != nil {
			return CandidateSet{}, fmt.Errorf("scan observation %d: %w", index, err)
		}
	}
	for index, skip := range input.Skipped {
		if strings.TrimSpace(skip.LivePath) == "" || strings.TrimSpace(skip.Reason) == "" {
			return CandidateSet{}, fmt.Errorf("skipped observation %d requires live path and reason", index)
		}
	}

	return CandidateSet{
		sources:         cloneSources(input.Sources),
		skills:          cloneSkills(input.Skills),
		hooks:           cloneHooks(input.Hooks),
		mcpServers:      cloneMCPServers(input.MCPServers),
		extensionResult: input.Extensions,
		scans:           cloneScans(input.Scans),
		skipped:         cloneSkipped(input.Skipped),
	}, nil
}

// Validate rejects zero or internally inconsistent candidate collections.
func (candidates CandidateSet) Validate() error {
	_, err := NewCandidateSet(CandidateSetInput{
		Sources:    candidates.sources,
		Skills:     candidates.skills,
		Hooks:      candidates.hooks,
		MCPServers: candidates.mcpServers,
		Extensions: candidates.extensionResult,
		Scans:      candidates.scans,
		Skipped:    candidates.skipped,
	})
	return err
}

// Sources returns an owned copy of imported instruction sources.
func (candidates CandidateSet) Sources() []Source {
	return cloneSources(candidates.sources)
}

// Skills returns an owned copy of imported skills.
func (candidates CandidateSet) Skills() []Skill {
	return cloneSkills(candidates.skills)
}

// Hooks returns an owned copy of imported hooks.
func (candidates CandidateSet) Hooks() []Hook {
	return cloneHooks(candidates.hooks)
}

// MCPServers returns an owned copy of imported MCP servers.
func (candidates CandidateSet) MCPServers() []MCPServer {
	return cloneMCPServers(candidates.mcpServers)
}

// Extensions returns canonical exact extension declarations in proposal order.
func (candidates CandidateSet) Extensions() []desiredextension.Extension {
	return candidates.extensionResult.Extensions()
}

// OrderedExtensions returns the complete proposal including fixed existing
// declarations needed for merge placement.
func (candidates CandidateSet) OrderedExtensions() []desiredextension.Extension {
	return candidates.extensionResult.OrderedExtensions()
}

// ExtensionResult returns the immutable exact-import order and evidence proposal.
func (candidates CandidateSet) ExtensionResult() adoptextension.Result {
	return candidates.extensionResult
}

// Scans returns an owned copy of live scan observations.
func (candidates CandidateSet) Scans() []Scan {
	return cloneScans(candidates.scans)
}

// Skipped returns an owned copy of skipped live observations.
func (candidates CandidateSet) Skipped() []Skipped {
	return cloneSkipped(candidates.skipped)
}

// ResourceCount returns the number of importable resources.
func (candidates CandidateSet) ResourceCount() int {
	return len(candidates.sources) +
		len(candidates.skills) +
		len(candidates.hooks) +
		len(candidates.mcpServers) +
		len(candidates.Extensions())
}

func validateSource(source Source) error {
	if strings.TrimSpace(source.ResourceName) == "" {
		return fmt.Errorf("resource name is required")
	}
	if err := validateTargetScope(source.Target, source.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(source.LivePath) == "" || strings.TrimSpace(source.SourcePath) == "" {
		return fmt.Errorf("live path and source path are required")
	}
	if len(source.Content) == 0 {
		return fmt.Errorf("content is required")
	}
	return nil
}

func validateSkill(skill Skill) error {
	if strings.TrimSpace(skill.ResourceName) == "" || strings.TrimSpace(skill.InstallName) == "" {
		return fmt.Errorf("resource name and install name are required")
	}
	if err := validateTargetScope(skill.Target, skill.Scope); err != nil {
		return err
	}
	if len(skill.Targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	seen := make(map[targetpkg.Target]struct{}, len(skill.Targets))
	representativePresent := false
	for _, target := range skill.Targets {
		if !profile.Profile(target).HasImportableDiscovery() {
			return fmt.Errorf("target %q is not supported by import", target)
		}
		if _, duplicate := seen[target]; duplicate {
			return fmt.Errorf("target %q is duplicated", target)
		}
		seen[target] = struct{}{}
		representativePresent = representativePresent || target == skill.Target
	}
	if !representativePresent {
		return fmt.Errorf("representative target is not present in targets")
	}
	if strings.TrimSpace(skill.SourcePath) == "" {
		return fmt.Errorf("source path is required")
	}
	if len(skill.SourceRoutes) == 0 {
		return fmt.Errorf("at least one skill source route is required")
	}
	var previous SkillSourceRoute
	liveRouteReads := make(map[string]string, len(skill.SourceRoutes))
	for index, route := range skill.SourceRoutes {
		if _, present := seen[route.Target]; !present {
			return fmt.Errorf("source route target %q is not present in targets", route.Target)
		}
		if strings.TrimSpace(route.LivePath) == "" {
			return fmt.Errorf("source route %d live path is required", index)
		}
		if !filepath.IsAbs(route.ReadPath) || filepath.Clean(route.ReadPath) != route.ReadPath {
			return fmt.Errorf("source route %d read path %q must be canonical and absolute", index, route.ReadPath)
		}
		if index > 0 && compareSkillSourceRoute(previous, route) >= 0 {
			return fmt.Errorf("skill source routes must be strictly ordered")
		}
		liveKey := string(route.Target) + "\x00" + route.LivePath
		if readPath, exists := liveRouteReads[liveKey]; exists && readPath != route.ReadPath {
			return fmt.Errorf("skill live route %q resolves to conflicting read paths", route.LivePath)
		}
		liveRouteReads[liveKey] = route.ReadPath
		previous = route
	}
	if _, err := skill.ExpectedSourceIdentity(); err != nil {
		return fmt.Errorf("source identity: %w", err)
	}
	return nil
}

func validateSkillSourceCorrelations(skills []Skill) error {
	identities := make(map[string]artifact.ContentHash, len(skills))
	for _, skill := range skills {
		contentHash, exists := identities[skill.SourcePath]
		if exists && contentHash != skill.ContentHash {
			return fmt.Errorf(
				"skill source path %q carries conflicting content identities %q and %q",
				skill.SourcePath,
				contentHash,
				skill.ContentHash,
			)
		}
		identities[skill.SourcePath] = skill.ContentHash
	}
	return nil
}

func compareSkillSourceRoute(left SkillSourceRoute, right SkillSourceRoute) int {
	for _, pair := range [][2]string{
		{string(left.Target), string(right.Target)},
		{left.LivePath, right.LivePath},
		{left.ReadPath, right.ReadPath},
	} {
		if compared := strings.Compare(pair[0], pair[1]); compared != 0 {
			return compared
		}
	}
	return 0
}

func validateHook(hook Hook) error {
	if strings.TrimSpace(hook.ResourceName) == "" {
		return fmt.Errorf("resource name is required")
	}
	if err := validateTargetScope(hook.Target, hook.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(hook.LivePath) == "" ||
		strings.TrimSpace(hook.Event) == "" ||
		strings.TrimSpace(hook.Command) == "" {
		return fmt.Errorf("live path, event, and command are required")
	}
	if hook.Timeout < 0 {
		return fmt.Errorf("timeout must not be negative")
	}
	return nil
}

func validateMCPServer(server MCPServer) error {
	if strings.TrimSpace(server.ResourceName) == "" {
		return fmt.Errorf("resource name is required")
	}
	if err := validateTargetScope(server.Target, server.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(server.LivePath) == "" || strings.TrimSpace(server.Command) == "" {
		return fmt.Errorf("live path and command are required")
	}
	for key, value := range server.Env {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return fmt.Errorf("environment references require non-empty names")
		}
	}
	return nil
}

func validateScan(scan Scan) error {
	if strings.TrimSpace(scan.ResourceKind) == "" ||
		strings.TrimSpace(scan.ResourceName) == "" ||
		strings.TrimSpace(scan.LivePath) == "" ||
		strings.TrimSpace(scan.Status) == "" {
		return fmt.Errorf("resource kind, resource name, live path, and status are required")
	}
	if err := validateTargetScope(scan.Target, scan.Scope); err != nil {
		return err
	}
	if scan.Entries < 0 || scan.Imported < 0 || scan.Skipped < 0 {
		return fmt.Errorf("scan counts must not be negative")
	}
	if scan.Imported+scan.Skipped > scan.Entries {
		return fmt.Errorf("imported and skipped counts exceed entries")
	}
	return nil
}

func validateTargetScope(target targetpkg.Target, scope targetpkg.Scope) error {
	if !profile.Profile(target).HasImportableDiscovery() {
		return fmt.Errorf("target %q is not supported by import", target)
	}
	if scope != targetpkg.ScopeProject && scope != targetpkg.ScopeGlobal {
		return fmt.Errorf("scope %q is not supported by import", scope)
	}
	return nil
}

func cloneSources(values []Source) []Source {
	if values == nil {
		return nil
	}
	cloned := make([]Source, len(values))
	copy(cloned, values)
	for index := range cloned {
		cloned[index].Content = cloneBytes(cloned[index].Content)
	}
	return cloned
}

func cloneSkills(values []Skill) []Skill {
	if values == nil {
		return nil
	}
	cloned := make([]Skill, len(values))
	copy(cloned, values)
	for index := range cloned {
		cloned[index].Targets = cloneTargets(cloned[index].Targets)
		cloned[index].SourceRoutes = append([]SkillSourceRoute(nil), cloned[index].SourceRoutes...)
	}
	return cloned
}

func cloneMCPServers(values []MCPServer) []MCPServer {
	if values == nil {
		return nil
	}
	cloned := make([]MCPServer, len(values))
	copy(cloned, values)
	for index := range cloned {
		cloned[index].Args = cloneStrings(cloned[index].Args)
		if cloned[index].Env == nil {
			continue
		}
		env := make(map[string]string, len(cloned[index].Env))
		for key, value := range cloned[index].Env {
			env[key] = value
		}
		cloned[index].Env = env
	}
	return cloned
}

func cloneHooks(values []Hook) []Hook {
	if values == nil {
		return nil
	}
	cloned := make([]Hook, len(values))
	copy(cloned, values)
	return cloned
}

func cloneScans(values []Scan) []Scan {
	if values == nil {
		return nil
	}
	cloned := make([]Scan, len(values))
	copy(cloned, values)
	return cloned
}

func cloneSkipped(values []Skipped) []Skipped {
	if values == nil {
		return nil
	}
	cloned := make([]Skipped, len(values))
	copy(cloned, values)
	return cloned
}

func cloneTargets(values []targetpkg.Target) []targetpkg.Target {
	if values == nil {
		return nil
	}
	cloned := make([]targetpkg.Target, len(values))
	copy(cloned, values)
	return cloned
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}
