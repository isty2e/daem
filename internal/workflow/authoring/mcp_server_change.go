package authoring

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

const firstSliceMCPTransport = "stdio"

func BuildAddMCPServerChange(document ManifestDocument, request AddMCPServerRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}
	header, err := declaration.DecodeManifestHeader(document.Content)
	if err != nil {
		return Change{}, err
	}
	name, err := CleanMCPServerName(request.Name)
	if err != nil {
		return Change{}, err
	}
	request.Name = name
	server, err := MCPServerFromAddRequest(request, header, document.Paths.ManifestOrigin)
	if err != nil {
		return Change{}, err
	}

	content, changeKind, err := ApplyAddMCPServerToManifest(document.Content, server, header)
	if err != nil {
		return Change{}, err
	}
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}
	warnings, err := mcpServerAuthoringWarnings(server)
	if err != nil {
		return Change{}, err
	}

	return Change{
		ManifestPath:  document.Path,
		Original:      document.Content,
		Content:       content,
		ResourceID:    name,
		ChangeKind:    changeKind,
		ManifestBlock: strings.TrimRight(declarationcodec.RenderMCPServerBlock(server), "\n"),
		Warnings:      warnings,
	}, nil
}

func BuildRemoveMCPServerChange(document ManifestDocument, request RemoveMCPServerRequest) (Change, error) {
	if err := document.validateOriginal(); err != nil {
		return Change{}, err
	}
	name, err := CleanMCPServerName(request.Name)
	if err != nil {
		return Change{}, err
	}
	request.Name = name
	content, changeKind, err := ApplyRemoveMCPServerToManifest(document.Content, request)
	if err != nil {
		return Change{}, err
	}
	if err := document.validateResult(content); err != nil {
		return Change{}, err
	}

	return Change{
		ManifestPath: document.Path,
		Original:     document.Content,
		Content:      content,
		ResourceID:   name,
		ChangeKind:   changeKind,
	}, nil
}

func MCPServerFromAddRequest(request AddMCPServerRequest, header declaration.ManifestHeader, origin daempaths.ManifestOrigin) (declarationcodec.MCPServer, error) {
	name, err := CleanMCPServerName(request.Name)
	if err != nil {
		return declarationcodec.MCPServer{}, err
	}
	command := request.Command
	if strings.TrimSpace(command) == "" {
		return declarationcodec.MCPServer{}, fmt.Errorf("mcp-server command is required")
	}
	if command != strings.TrimSpace(command) {
		return declarationcodec.MCPServer{}, fmt.Errorf("mcp-server command must not contain leading or trailing whitespace")
	}
	if err := validatePortableMCPCommand(command); err != nil {
		return declarationcodec.MCPServer{}, err
	}
	targets, scope, err := addMCPAuthoringTargetScope(request.Targets, request.Scope, header, origin)
	if err != nil {
		return declarationcodec.MCPServer{}, err
	}
	if err := validateAddMCPAuthoringShape(targets[0], scope, request); err != nil {
		return declarationcodec.MCPServer{}, err
	}
	env, err := mcpServerEnvReferences(request.Env)
	if err != nil {
		return declarationcodec.MCPServer{}, err
	}

	server := declarationcodec.MCPServer{
		Name:      name,
		Targets:   targets,
		Scope:     scope,
		Transport: firstSliceMCPTransport,
		Command:   declaration.NewMCPAmbientCommand(command),
		Args:      append([]string(nil), request.Args...),
		Env:       env,
	}
	if err := validateCanonicalMCPServerAuthoring(server); err != nil {
		return declarationcodec.MCPServer{}, err
	}
	return server, nil
}

func ApplyAddMCPServerToManifest(original []byte, server declarationcodec.MCPServer, header declaration.ManifestHeader) ([]byte, string, error) {
	incomingKey, err := mcpServerAuthoringKeyFor(server, header, "incoming mcp_server")
	if err != nil {
		return nil, "", err
	}

	blocks, err := declarationcodec.ScanMCPServerBlocks(original)
	if err != nil {
		return nil, "", err
	}

	for _, block := range blocks {
		existing := block.Server
		if existing.Name != server.Name {
			continue
		}
		existingKey, err := mcpServerAuthoringKeyFor(existing, header, fmt.Sprintf("mcp_server %q", existing.Name))
		if err != nil {
			return nil, "", err
		}
		if existingKey != incomingKey {
			continue
		}
		if !declarationcodec.SameMCPServerProjectionPayload(existing, server) {
			id, subjectErr := entity.New(entity.KindMCPServer, incomingKey.name)
			if subjectErr != nil {
				return nil, "", subjectErr
			}
			subject, subjectErr := topologymcp.ProjectionSubject(incomingKey.target, incomingKey.scope, id.Name())
			if subjectErr != nil {
				return nil, "", subjectErr
			}
			return nil, "", fmt.Errorf(
				"duplicate mcp_server subject %q for name %q",
				subject.Namespace()+"."+incomingKey.name,
				server.Name,
			)
		}
		if len(server.Targets) == 0 {
			return nil, "", fmt.Errorf("mcp_server %q already exists", server.Name)
		}
		if len(existing.Targets) == 0 {
			return nil, "", fmt.Errorf("mcp_server %q inherits manifest targets; edit the manifest manually to change target inheritance", server.Name)
		}
		return nil, "", fmt.Errorf("mcp_server %q already has the selected targets", server.Name)
	}

	return declaration.AppendDocumentBlock(original, declarationcodec.RenderMCPServerBlock(server)), "append mcp_server resource", nil
}

func ApplyRemoveMCPServerToManifest(original []byte, request RemoveMCPServerRequest) ([]byte, string, error) {
	header, err := declaration.DecodeManifestHeader(original)
	if err != nil {
		return nil, "", err
	}
	name, err := CleanMCPServerName(request.Name)
	if err != nil {
		return nil, "", err
	}
	request.Name = name
	nameOnlySelector := len(request.Targets) == 0 && strings.TrimSpace(request.Scope) == ""
	targets, scope, err := normalizedMCPRemoveSelector(request.Targets, request.Scope)
	if err != nil {
		return nil, "", err
	}
	request.Targets = targets
	request.Scope = scope

	candidates, err := removeMCPServerCandidates(original, header)
	if err != nil {
		return nil, "", err
	}
	if nameOnlySelector && countMCPServerCandidatesByName(candidates, request.Name) > 1 {
		return nil, "", fmt.Errorf("mcp_server resource %q is ambiguous; narrow with --target/--scope", request.Name)
	}
	matches := filterRemoveMCPServerCandidates(candidates, request)
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("mcp_server resource %q not found", request.Name)
	}
	if len(matches) > 1 {
		return nil, "", fmt.Errorf("mcp_server resource %q is ambiguous; narrow with --target/--scope", request.Name)
	}

	content := declaration.RemoveDocumentRange(original, declaration.DocumentRange{Start: matches[0].start, End: matches[0].end})
	return content, "remove mcp_server resource", nil
}

type mcpServerAuthoringKey struct {
	target target.Target
	scope  target.Scope
	name   string
}

func mcpServerAuthoringKeyFor(server declarationcodec.MCPServer, header declaration.ManifestHeader, context string) (mcpServerAuthoringKey, error) {
	name, err := CleanMCPServerName(server.Name)
	if err != nil {
		return mcpServerAuthoringKey{}, err
	}
	effectiveTargets := header.EffectiveTargets(server.Targets)
	if len(effectiveTargets) != 1 {
		return mcpServerAuthoringKey{}, fmt.Errorf("%s.targets: MCP server projection supports exactly one target", context)
	}
	selectedTarget, err := target.ParseTarget(effectiveTargets[0])
	if err != nil {
		return mcpServerAuthoringKey{}, fmt.Errorf("%s.targets: %w", context, err)
	}
	selectedScope, err := target.ParseScope(header.EffectiveScope(server.Scope))
	if err != nil {
		return mcpServerAuthoringKey{}, fmt.Errorf("%s.scope: %w", context, err)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(selectedTarget, selectedScope)
	if !ok {
		return mcpServerAuthoringKey{}, fmt.Errorf("%s: unsupported MCP target/scope %s/%s", context, selectedTarget, selectedScope)
	}
	return mcpServerAuthoringKey{target: placement.Target(), scope: placement.Scope(), name: name}, nil
}

type removeMCPServerCandidate struct {
	name    string
	scope   string
	targets []string
	start   int
	end     int
}

func removeMCPServerCandidates(content []byte, header declaration.ManifestHeader) ([]removeMCPServerCandidate, error) {
	blocks, err := declarationcodec.ScanMCPServerBlocks(content)
	if err != nil {
		return nil, err
	}
	candidates := make([]removeMCPServerCandidate, 0, len(blocks))
	for _, block := range blocks {
		server := block.Server
		candidates = append(candidates, removeMCPServerCandidate{
			name:    server.Name,
			scope:   header.EffectiveScope(server.Scope),
			targets: header.EffectiveTargets(server.Targets),
			start:   block.Start,
			end:     block.End,
		})
	}
	return candidates, nil
}

func countMCPServerCandidatesByName(candidates []removeMCPServerCandidate, name string) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.name == name {
			count++
		}
	}
	return count
}

func filterRemoveMCPServerCandidates(candidates []removeMCPServerCandidate, request RemoveMCPServerRequest) []removeMCPServerCandidate {
	matches := make([]removeMCPServerCandidate, 0)
	for _, candidate := range candidates {
		if candidate.name != request.Name {
			continue
		}
		if request.Scope != "" && candidate.scope != request.Scope {
			continue
		}
		if len(request.Targets) != 0 && !declaration.Targets(candidate.targets).Intersects(declaration.Targets(request.Targets)) {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

func mcpServerEnvReferences(assignments []MCPServerEnvAssignment) (map[string]declarationcodec.MCPEnvReference, error) {
	env := make(map[string]declarationcodec.MCPEnvReference, len(assignments))
	for _, assignment := range assignments {
		name := strings.TrimSpace(assignment.Name)
		fromEnv := strings.TrimSpace(assignment.FromEnv)
		if name != assignment.Name || fromEnv != assignment.FromEnv {
			return nil, fmt.Errorf("--env names must not contain leading or trailing whitespace")
		}
		if err := validateMCPEnvName(name, "--env "+name); err != nil {
			return nil, err
		}
		if err := validateMCPEnvName(fromEnv, "--env "+name+" from_env"); err != nil {
			return nil, err
		}
		if _, exists := env[name]; exists {
			return nil, fmt.Errorf("duplicate --env server name %q", name)
		}
		canonical, err := desiredmcp.NewEnvReference(fromEnv)
		if err != nil {
			return nil, fmt.Errorf("--env %s from_env: %w", name, err)
		}
		env[name] = declarationcodec.MCPEnvReference{FromEnv: canonical.FromEnv()}
	}
	return env, nil
}

func validatePortableMCPCommand(command string) error {
	if filepath.IsAbs(command) || strings.ContainsAny(command, "/\\ \t\n\r;&|$`") {
		return fmt.Errorf("--command must be a portable command token")
	}
	if err := validateMCPStableToken(command, "--command"); err != nil {
		return err
	}
	if _, err := desiredmcp.NewAmbientCommand(command); err != nil {
		return fmt.Errorf("--command: %w", err)
	}
	return nil
}

func validateMCPStableToken(value string, context string) error {
	if value == "" {
		return fmt.Errorf("%s is required", context)
	}
	if !isASCIIAlphaNumeric(value[0]) {
		return fmt.Errorf("%s must start with an ASCII letter or digit", context)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if isASCIIAlphaNumeric(character) || character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("%s must be a stable token", context)
	}
	return nil
}

func validateMCPEnvName(value string, context string) error {
	if value == "" {
		return fmt.Errorf("%s: env name is required", context)
	}
	if value[0] >= '0' && value[0] <= '9' {
		return fmt.Errorf("%s: env name must not start with a digit", context)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if isASCIIAlphaNumeric(character) || character == '_' {
			continue
		}
		return fmt.Errorf("%s: env name must contain only ASCII letters, digits, or underscore", context)
	}
	return nil
}

func isASCIIAlphaNumeric(character byte) bool {
	return (character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9')
}
