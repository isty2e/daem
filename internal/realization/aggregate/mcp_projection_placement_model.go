package aggregate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// MCPPlacementID identifies one implemented standalone MCP projection row.
type MCPPlacementID string

const (
	MCPPlacementClaudeProject     MCPPlacementID = "claude-code.project.project-config"
	MCPPlacementClaudeGlobal      MCPPlacementID = "claude-code.global.user-shared-json"
	MCPPlacementAntigravityGlobal MCPPlacementID = "antigravity-cli.global.default-config"
	MCPPlacementOpenCodeProject   MCPPlacementID = "opencode.project.project-config"
	MCPPlacementOpenCodeGlobal    MCPPlacementID = "opencode.global.default-json"
	MCPPlacementCodexProject      MCPPlacementID = "codex.project.project-config"
	MCPPlacementCodexGlobal       MCPPlacementID = "codex.global.default-config"
)

const (
	ClaudeProjectMCPConfigPath           = ".mcp.json"
	ClaudeProjectMCPStdioAdapterV1       = "claude-project-mcp-stdio-v1"
	ClaudeGlobalMCPConfigPath            = "~/.claude.json"
	ClaudeGlobalMCPStdioEnvAdapterV1     = "claude-code-user-mcp-stdio-env-v1"
	AntigravityGlobalMCPConfigPath       = "~/.gemini/config/mcp_config.json"
	AntigravityGlobalMCPCommandAdapterV1 = "antigravity-cli-global-mcp-command-v1"
	OpenCodeProjectMCPConfigPath         = "opencode.json"
	OpenCodeProjectMCPLocalCommandV1     = "opencode-project-mcp-local-command-v1"
	OpenCodeGlobalMCPConfigPath          = "~/.config/opencode/opencode.json"
	OpenCodeGlobalMCPLocalCommandV1      = "opencode-global-mcp-local-command-v1"
	CodexProjectMCPConfigPath            = ".codex/config.toml"
	CodexProjectMCPStdioCommandV1        = "codex-project-mcp-stdio-command-v1"
	CodexGlobalMCPConfigPath             = "~/.codex/config.toml"
	CodexGlobalMCPStdioEnvVarsV1         = "codex-global-mcp-stdio-env-vars-v1"

	openCodeProjectMCPConflictPath = "opencode.jsonc"
	openCodeGlobalMCPConflictPath  = "~/.config/opencode/opencode.jsonc"
)

// MCPConfigLayer identifies the host config layer that owns one MCP projection row.
type MCPConfigLayer string

const (
	MCPConfigLayerClaudeProjectFile            MCPConfigLayer = "claude-project-config"
	MCPConfigLayerClaudeUserSharedJSON         MCPConfigLayer = "claude-user-shared-json"
	MCPConfigLayerAntigravityGlobalDefaultFile MCPConfigLayer = "antigravity-cli-global-default-config"
	MCPConfigLayerOpenCodeProjectFile          MCPConfigLayer = "opencode-project-config"
	MCPConfigLayerOpenCodeGlobalDefaultJSON    MCPConfigLayer = "opencode-global-default-json"
	MCPConfigLayerCodexProjectFile             MCPConfigLayer = "codex-project-config"
	MCPConfigLayerCodexGlobalDefaultFile       MCPConfigLayer = "codex-user-default-config"
)

const (
	MCPCodecClaudeProjectStdio       CodecContractID = ClaudeProjectMCPStdioAdapterV1
	MCPCodecClaudeGlobalStdioEnv     CodecContractID = ClaudeGlobalMCPStdioEnvAdapterV1
	MCPCodecAntigravityGlobalCommand CodecContractID = AntigravityGlobalMCPCommandAdapterV1
	MCPCodecOpenCodeProjectLocal     CodecContractID = OpenCodeProjectMCPLocalCommandV1
	MCPCodecOpenCodeGlobalLocal      CodecContractID = OpenCodeGlobalMCPLocalCommandV1
	MCPCodecCodexProjectStdioCommand CodecContractID = CodexProjectMCPStdioCommandV1
	MCPCodecCodexGlobalStdioEnvVars  CodecContractID = CodexGlobalMCPStdioEnvVarsV1
)

// MCPAbsencePolicy identifies the row-local behavior when the manifest declaration disappears.
type MCPAbsencePolicy string

const (
	MCPAbsenceRemoveBinding MCPAbsencePolicy = "remove_binding"
)

// MCPPlacementInput carries the semantic facts required to construct an MCP placement row.
type MCPPlacementInput struct {
	ID                     MCPPlacementID
	Target                 target.Target
	Scope                  target.Scope
	ConfigLayer            MCPConfigLayer
	ConfigPath             string
	ConflictingConfigPath  string
	MergeUnit              MCPMergeUnit
	ContentPathPrefix      MCPContentPathPrefix
	SiblingRetention       MCPSiblingRetentionPolicy
	CodecContractID        CodecContractID
	ComparedFields         []string
	Absence                MCPAbsencePolicy
	EnvReferenceMapping    MCPEnvReferenceMapping
	EnvReferenceResolution MCPEnvReferenceResolution
}

// MCPPlacement is the canonical internal row for one standalone MCP exact projection.
type MCPPlacement struct {
	id                    MCPPlacementID
	target                target.Target
	scope                 target.Scope
	configLayer           MCPConfigLayer
	aggregateSpec         MCPConfigAggregateSpec
	conflictingConfigPath output.Destination
	hasConflictingPath    bool
	codecContractID       CodecContractID
	comparedFields        []string
	absence               MCPAbsencePolicy
	envReferences         MCPEnvReferenceContract
}

// NewMCPPlacement constructs a validated MCP placement row.
func NewMCPPlacement(input MCPPlacementInput) (MCPPlacement, error) {
	configPath, err := output.Parse(input.ConfigPath)
	if err != nil {
		return MCPPlacement{}, fmt.Errorf("MCP config path: %w", err)
	}
	aggregateSpec, err := NewMCPConfigAggregateSpec(MCPConfigAggregateSpecInput{
		Root:              configPath,
		MergeUnit:         input.MergeUnit,
		ContentPathPrefix: input.ContentPathPrefix,
		SiblingRetention:  input.SiblingRetention,
	})
	if err != nil {
		return MCPPlacement{}, err
	}
	envReferences, err := NewMCPEnvReferenceContract(
		input.EnvReferenceMapping,
		input.EnvReferenceResolution,
	)
	if err != nil {
		return MCPPlacement{}, fmt.Errorf("MCP environment-reference contract: %w", err)
	}
	var conflictingConfigPath output.Destination
	hasConflictingPath := strings.TrimSpace(input.ConflictingConfigPath) != ""
	if hasConflictingPath {
		conflictingConfigPath, err = output.Parse(input.ConflictingConfigPath)
		if err != nil {
			return MCPPlacement{}, fmt.Errorf("MCP conflicting config path: %w", err)
		}
	}
	placement := MCPPlacement{
		id:                    input.ID,
		target:                input.Target,
		scope:                 input.Scope,
		configLayer:           input.ConfigLayer,
		aggregateSpec:         aggregateSpec,
		conflictingConfigPath: conflictingConfigPath,
		hasConflictingPath:    hasConflictingPath,
		codecContractID:       input.CodecContractID,
		comparedFields:        canonicalTokenSet(input.ComparedFields),
		absence:               input.Absence,
		envReferences:         envReferences,
	}
	if err := placement.Validate(); err != nil {
		return MCPPlacement{}, err
	}
	return placement, nil
}

// ID returns the implemented placement row identity.
func (placement MCPPlacement) ID() MCPPlacementID {
	return placement.id
}

// Target returns the host target for this placement row.
func (placement MCPPlacement) Target() target.Target {
	return placement.target
}

// Scope returns the public manifest scope for this placement row.
func (placement MCPPlacement) Scope() target.Scope {
	return placement.scope
}

// ConfigLayer returns the internal host config layer for this placement row.
func (placement MCPPlacement) ConfigLayer() MCPConfigLayer {
	return placement.configLayer
}

// AggregateSpec returns the canonical aggregate ownership spec for this placement.
func (placement MCPPlacement) AggregateSpec() MCPConfigAggregateSpec {
	return placement.aggregateSpec
}

// AggregateRoot returns the host config aggregate root for this placement row.
func (placement MCPPlacement) AggregateRoot() output.Destination {
	return placement.aggregateSpec.Root()
}

// ConfigPath returns the host config path role for this placement row.
func (placement MCPPlacement) ConfigPath() output.Destination {
	return placement.aggregateSpec.Root()
}

// ConflictingConfigPath returns an alternate host config whose presence makes
// direct mutation of this placement ambiguous.
func (placement MCPPlacement) ConflictingConfigPath() (output.Destination, bool) {
	return placement.conflictingConfigPath, placement.hasConflictingPath
}

// MergeUnit returns the managed subtree unit for this placement row.
func (placement MCPPlacement) MergeUnit() MCPMergeUnit {
	return placement.aggregateSpec.MergeUnit()
}

// ContentPathPrefix returns the managed parent path for this placement row.
func (placement MCPPlacement) ContentPathPrefix() MCPContentPathPrefix {
	return placement.aggregateSpec.ContentPathPrefix()
}

// SiblingRetention returns the row-local unmanaged sibling policy for this placement.
func (placement MCPPlacement) SiblingRetention() MCPSiblingRetentionPolicy {
	return placement.aggregateSpec.SiblingRetention()
}

// ContentPath returns the managed entry path for serverID inside this placement row.
func (placement MCPPlacement) ContentPath(serverID string) (ContentPath, error) {
	return placement.aggregateSpec.ContentPath(serverID)
}

// Contribution constructs the exact standalone MCP aggregate realization for
// serverID. Placement owns every static projection-contract fact; callers
// supply only the selected canonical entry.
func (placement MCPPlacement) Contribution(
	serverID string,
	canonicalProjection string,
) (ManagedContribution, error) {
	contract, err := placement.ProjectionContract(serverID)
	if err != nil {
		return ManagedContribution{}, err
	}
	contribution := ManagedContribution{
		address:               contract.address,
		cardinality:           contract.cardinality,
		siblingRetention:      contract.siblingRetention,
		siblingPreservation:   contract.siblingPreservation,
		equivalence:           contract.equivalence,
		canonicalContribution: canonicalProjection,
		codecContractID:       contract.codecContractID,
		comparedFields:        contract.ComparedFields(),
	}
	if err := contribution.Validate(); err != nil {
		return ManagedContribution{}, err
	}
	return contribution, nil
}

// ProjectionContract constructs the static standalone MCP projection contract
// for serverID without fabricating desired contribution bytes.
func (placement MCPPlacement) ProjectionContract(serverID string) (ProjectionContract, error) {
	if err := placement.Validate(); err != nil {
		return ProjectionContract{}, err
	}
	contentPath, err := placement.ContentPath(serverID)
	if err != nil {
		return ProjectionContract{}, err
	}
	address, err := newProjectionAddress(
		string(placement.id),
		placement.target,
		placement.scope,
		placement.ConfigPath(),
		MergeUnit(placement.MergeUnit()),
		string(contentPath),
	)
	if err != nil {
		return ProjectionContract{}, err
	}
	contract := ProjectionContract{
		address:             address,
		cardinality:         ContributionExclusive,
		siblingRetention:    SiblingRetention(placement.SiblingRetention()),
		siblingPreservation: PreserveSiblingsSemantic,
		equivalence:         EquivalenceCanonicalSemantic,
		codecContractID:     placement.codecContractID,
		comparedFields:      placement.ComparedFields(),
	}
	if err := contract.Validate(); err != nil {
		return ProjectionContract{}, err
	}
	return contract, nil
}

// ServerIDFromContentPath returns the stable server id represented by contentPath in this placement row.
func (placement MCPPlacement) ServerIDFromContentPath(contentPath ContentPath) (string, bool) {
	return placement.aggregateSpec.ServerIDFromContentPath(contentPath)
}

// CodecContractID returns the exact codec contract for this placement row.
func (placement MCPPlacement) CodecContractID() CodecContractID {
	return placement.codecContractID
}

// ComparedFields returns the canonical exact-projection comparison field set.
func (placement MCPPlacement) ComparedFields() []string {
	return append([]string(nil), placement.comparedFields...)
}

// Absence returns the row-local absent-declaration policy.
func (placement MCPPlacement) Absence() MCPAbsencePolicy {
	return placement.absence
}

// EnvReferenceContract returns the exact symbolic environment-reference
// capability selected for this placement.
func (placement MCPPlacement) EnvReferenceContract() MCPEnvReferenceContract {
	return placement.envReferences
}

// AdmitEnvironmentReference validates one canonical child/source mapping
// against this placement. Desired owns environment-name syntax.
func (placement MCPPlacement) AdmitEnvironmentReference(childName string, sourceName string) error {
	if err := placement.Validate(); err != nil {
		return err
	}
	if placement.envReferences.AdmitsReference(childName, sourceName) {
		return nil
	}
	return MCPEnvReferenceAdmissionError{
		placementID: placement.id,
		mapping:     placement.envReferences.Mapping(),
		childName:   childName,
		sourceName:  sourceName,
	}
}

// Validate rejects malformed placement rows.
func (placement MCPPlacement) Validate() error {
	if err := validateToken("MCP placement id", string(placement.id)); err != nil {
		return err
	}
	if _, err := target.ParseTarget(string(placement.target)); err != nil {
		return fmt.Errorf("MCP placement %q target: %w", placement.id, err)
	}
	if _, err := target.ParseScope(string(placement.scope)); err != nil {
		return fmt.Errorf("MCP placement %q scope: %w", placement.id, err)
	}
	if err := validateToken("MCP config layer", string(placement.configLayer)); err != nil {
		return err
	}
	if err := placement.aggregateSpec.Validate(); err != nil {
		return fmt.Errorf("MCP placement %q aggregate spec: %w", placement.id, err)
	}
	if placement.hasConflictingPath && placement.conflictingConfigPath == placement.aggregateSpec.Root() {
		return fmt.Errorf("MCP placement %q conflicting config path must differ from its aggregate root", placement.id)
	}
	if placement.hasConflictingPath {
		if err := placement.conflictingConfigPath.ValidateScope(placement.scope); err != nil {
			return fmt.Errorf("MCP placement %q conflicting config path: %w", placement.id, err)
		}
	} else if placement.conflictingConfigPath.Validate() == nil {
		return fmt.Errorf("MCP placement %q has an unmarked conflicting config path", placement.id)
	}
	if err := validateToken("MCP codec contract", string(placement.codecContractID)); err != nil {
		return err
	}
	if err := validateTokenSet("MCP compared field", placement.comparedFields); err != nil {
		return err
	}
	if err := placement.envReferences.Validate(); err != nil {
		return fmt.Errorf("MCP placement %q environment references: %w", placement.id, err)
	}
	if placement.absence != MCPAbsenceRemoveBinding {
		return fmt.Errorf("MCP placement %q absence policy %q is unsupported", placement.id, placement.absence)
	}
	return nil
}

func validateToken(label string, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be a stable token", label)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' ||
			character == '_' ||
			character == '-' {
			continue
		}
		return fmt.Errorf("%s must be a stable token", label)
	}
	return nil
}

func validateTokenSet(label string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s values are required", label)
	}
	for index, value := range values {
		if err := validateToken(label, value); err != nil {
			return err
		}
		if index > 0 && values[index-1] >= value {
			return fmt.Errorf("%s values must be sorted and unique", label)
		}
	}
	return nil
}

func canonicalTokenSet(values []string) []string {
	result := append([]string(nil), values...)
	for index := range result {
		result[index] = strings.TrimSpace(result[index])
	}
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}
