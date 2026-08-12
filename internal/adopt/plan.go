package adopt

import (
	"encoding/json"
	"fmt"

	adoptextension "github.com/isty2e/daem/internal/adopt/extension"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
)

// Plan is one complete, immutable adoption decision.
type Plan struct {
	request         Request
	originalContent []byte
	manifestContent []byte
	candidates      CandidateSet
	mergeResults    []MergeResult
}

// NewPlan constructs a complete adoption plan.
func NewPlan(
	request Request,
	originalContent []byte,
	manifestContent []byte,
	candidates CandidateSet,
	mergeResults []MergeResult,
) (Plan, error) {
	if err := request.Validate(); err != nil {
		return Plan{}, err
	}
	if err := candidates.Validate(); err != nil {
		return Plan{}, err
	}
	if manifestContent == nil {
		return Plan{}, fmt.Errorf("adoption plan requires manifest content")
	}
	if candidates.ResourceCount() == 0 && len(mergeResults) == 0 {
		return Plan{}, fmt.Errorf("adoption plan requires imported resources or merge results")
	}
	if request.Merge() {
		if originalContent == nil {
			return Plan{}, fmt.Errorf("merge adoption plan requires original manifest content")
		}
		if len(mergeResults) == 0 {
			return Plan{}, fmt.Errorf("merge adoption plan requires merge results")
		}
	} else {
		if originalContent != nil {
			return Plan{}, fmt.Errorf("create adoption plan must not carry original manifest content")
		}
		if len(mergeResults) != 0 {
			return Plan{}, fmt.Errorf("create adoption plan must not carry merge results")
		}
	}
	for index, result := range mergeResults {
		if err := result.Validate(); err != nil {
			return Plan{}, fmt.Errorf("merge result %d: %w", index, err)
		}
	}
	if err := validateCandidatesAgainstRequest(request, candidates); err != nil {
		return Plan{}, err
	}

	return Plan{
		request:         request,
		originalContent: cloneBytes(originalContent),
		manifestContent: cloneBytes(manifestContent),
		candidates:      candidates,
		mergeResults:    cloneMergeResults(mergeResults),
	}, nil
}

// Validate rejects zero or internally inconsistent plans.
func (plan Plan) Validate() error {
	_, err := NewPlan(plan.request, plan.originalContent, plan.manifestContent, plan.candidates, plan.mergeResults)
	return err
}

// Output returns the destination manifest path.
func (plan Plan) Output() string {
	return plan.request.Output()
}

// SourceDirectory returns the imported-source root.
func (plan Plan) SourceDirectory() SourceDirectory {
	return plan.request.SourceDirectory()
}

// Merge reports whether this plan updates an existing manifest.
func (plan Plan) Merge() bool {
	return plan.request.Merge()
}

// OriginalContent returns an owned copy of the pre-merge manifest.
func (plan Plan) OriginalContent() []byte {
	return cloneBytes(plan.originalContent)
}

// ManifestContent returns an owned copy of the planned manifest.
func (plan Plan) ManifestContent() []byte {
	return cloneBytes(plan.manifestContent)
}

// Sources returns an owned copy of planned instruction sources.
func (plan Plan) Sources() []Source {
	return plan.candidates.Sources()
}

// Skills returns the planned skill artifacts selected for publication.
func (plan Plan) Skills() []Skill {
	return plan.candidates.Skills()
}

// SkillSourceAuthorities returns all exact source evidence supporting skill
// decisions in this plan, whether or not an artifact will be copied.
func (plan Plan) SkillSourceAuthorities() []SkillSourceAuthority {
	return plan.candidates.SkillSourceAuthorities()
}

// Hooks returns an owned copy of planned hooks.
func (plan Plan) Hooks() []Hook {
	return plan.candidates.Hooks()
}

// MCPServers returns an owned copy of planned MCP servers.
func (plan Plan) MCPServers() []MCPServer {
	return plan.candidates.MCPServers()
}

// MCPSourceAuthorities returns all physical source evidence supporting MCP
// decisions in this plan, whether or not a declaration will be written.
func (plan Plan) MCPSourceAuthorities() []MCPSourceAuthority {
	return plan.candidates.MCPSourceAuthorities()
}

// Extensions returns source-exact imported extension declarations.
func (plan Plan) Extensions() []desiredextension.Extension {
	return plan.candidates.Extensions()
}

// ExtensionResult returns the exact extension evidence and ordering proposal.
func (plan Plan) ExtensionResult() adoptextension.Result {
	return plan.candidates.ExtensionResult()
}

// Scans returns an owned copy of live scan observations.
func (plan Plan) Scans() []Scan {
	return plan.candidates.Scans()
}

// Skipped returns an owned copy of skipped live observations.
func (plan Plan) Skipped() []Skipped {
	return plan.candidates.Skipped()
}

// MergeResults returns an owned copy of merge decisions.
func (plan Plan) MergeResults() []MergeResult {
	return cloneMergeResults(plan.mergeResults)
}

func cloneMergeResults(values []MergeResult) []MergeResult {
	if values == nil {
		return nil
	}
	cloned := make([]MergeResult, len(values))
	copy(cloned, values)
	return cloned
}

// IdentityBytes returns a deterministic disclosure of all plan-owned facts.
func (plan Plan) IdentityBytes() ([]byte, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	extensionIdentity, err := plan.ExtensionResult().IdentityBytes()
	if err != nil {
		return nil, fmt.Errorf("extension identity: %w", err)
	}
	return json.Marshal(struct {
		Output           string
		SourceDir        string
		Merge            bool
		OriginalContent  []byte
		ManifestContent  []byte
		Sources          []Source
		Skills           []Skill
		SkillAuthorities []SkillSourceAuthority
		Hooks            []Hook
		MCPServers       []MCPServer
		MCPAuthorities   []MCPSourceAuthority
		Extensions       json.RawMessage
		Scans            []Scan
		Skipped          []Skipped
		MergeResults     []mergeResultIdentity
	}{
		Output:           plan.Output(),
		SourceDir:        plan.SourceDirectory().Root(),
		Merge:            plan.Merge(),
		OriginalContent:  plan.OriginalContent(),
		ManifestContent:  plan.ManifestContent(),
		Sources:          plan.Sources(),
		Skills:           plan.Skills(),
		SkillAuthorities: plan.SkillSourceAuthorities(),
		Hooks:            plan.Hooks(),
		MCPServers:       plan.MCPServers(),
		MCPAuthorities:   plan.MCPSourceAuthorities(),
		Extensions:       extensionIdentity,
		Scans:            plan.Scans(),
		Skipped:          plan.Skipped(),
		MergeResults:     mergeResultIdentities(plan.MergeResults()),
	})
}

type mergeResultIdentity struct {
	Resource string
	Subject  string
	Status   MergeStatus
	Detail   string
}

func mergeResultIdentities(results []MergeResult) []mergeResultIdentity {
	if results == nil {
		return nil
	}
	identities := make([]mergeResultIdentity, 0, len(results))
	for _, result := range results {
		identities = append(identities, mergeResultIdentity{
			Resource: result.Resource,
			Subject:  result.Subject.String(),
			Status:   result.Status,
			Detail:   result.Detail,
		})
	}
	return identities
}

func validateCandidatesAgainstRequest(request Request, candidates CandidateSet) error {
	targets := make(map[string]struct{})
	for _, target := range request.Targets() {
		targets[string(target)] = struct{}{}
	}
	scopes := make(map[string]struct{})
	for _, scope := range request.Scopes() {
		scopes[string(scope)] = struct{}{}
	}
	validate := func(label string, target string, scope string) error {
		if _, selected := targets[target]; !selected {
			return fmt.Errorf("%s target %q is outside the adoption request", label, target)
		}
		if _, selected := scopes[scope]; !selected {
			return fmt.Errorf("%s scope %q is outside the adoption request", label, scope)
		}
		return nil
	}
	for _, source := range candidates.sources {
		if err := validate("source candidate", string(source.Target), string(source.Scope)); err != nil {
			return err
		}
	}
	for _, skill := range candidates.skills {
		for _, target := range skill.Targets {
			if err := validate("skill candidate", string(target), string(skill.Scope)); err != nil {
				return err
			}
		}
	}
	for _, authority := range candidates.skillAuthorities {
		for _, route := range authority.Routes {
			if err := validate("skill source authority", string(route.Target), string(authority.Scope)); err != nil {
				return err
			}
		}
	}
	for _, hook := range candidates.hooks {
		if err := validate("hook candidate", string(hook.Target), string(hook.Scope)); err != nil {
			return err
		}
	}
	for _, server := range candidates.mcpServers {
		if err := validate("mcp server candidate", string(server.Target), string(server.Scope)); err != nil {
			return err
		}
	}
	for _, authority := range candidates.mcpAuthorities {
		if err := validate("mcp source authority", string(authority.Target), string(authority.Scope)); err != nil {
			return err
		}
	}
	for _, extension := range candidates.Extensions() {
		if err := validate(
			"extension candidate",
			string(extension.Target()),
			string(extension.Scope()),
		); err != nil {
			return err
		}
	}
	for _, scan := range candidates.scans {
		if err := validate("scan observation", string(scan.Target), string(scan.Scope)); err != nil {
			return err
		}
	}
	return nil
}
