package authoring

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func piMCPAuthoringIntent(content []byte, header declaration.ManifestHeader, key mcpServerAuthoringKey) (desiredmcp.Backend, bool, error) {
	if key.target != target.TargetPi {
		return "", false, nil
	}
	authoringProfile, ok := profile.MCPProviderAuthoringProfileForTarget(key.target)
	if !ok {
		return "", false, fmt.Errorf("Pi MCP provider authoring profile is missing")
	}
	providers, err := declaredMCPProviderContributions(content, header, authoringProfile)
	if err != nil {
		return "", false, err
	}
	blocks, err := declarationcodec.ScanMCPServerBlocks(content)
	if err != nil {
		return "", false, err
	}
	for _, block := range blocks {
		if block.Server.Name != key.name {
			continue
		}
		existingKey, err := mcpServerAuthoringKeyFor(block.Server, header, "existing mcp_server")
		if err != nil {
			return "", false, err
		}
		if existingKey == key {
			backend, err := desiredmcp.ParseBackend(key.target, block.Server.Backend)
			return backend, len(providers) != 0, err
		}
	}
	return "", len(providers) != 0, nil
}

func requiresPiVersionForAuthoring(document ManifestDocument, request AddMCPServerRequest) (bool, error) {
	if err := document.validateOriginal(); err != nil {
		return false, err
	}
	header, err := declaration.DecodeManifestHeader(document.Content)
	if err != nil {
		return false, err
	}
	server, err := MCPServerFromAddRequest(request, header, document.Paths.ManifestOrigin)
	if err != nil {
		return false, err
	}
	key, err := mcpServerAuthoringKeyFor(server, header, "incoming mcp_server")
	if err != nil {
		return false, err
	}
	existing, provider, err := piMCPAuthoringIntent(document.Content, header, key)
	return key.target == target.TargetPi && existing == "" && !provider, err
}

func planSelectedMCPAuthoring(content []byte, header declaration.ManifestHeader, server declarationcodec.MCPServer, key mcpServerAuthoringKey, version profile.PiMCPVersion) (declarationcodec.MCPServer, mcpProviderAuthoringPlan, error) {
	if key.target != target.TargetPi {
		plan, err := planMCPProviderAuthoring(content, header, key.target, key.scope)
		return server, plan, err
	}
	existing, provider, err := piMCPAuthoringIntent(content, header, key)
	if err != nil {
		return server, mcpProviderAuthoringPlan{}, err
	}
	contract, err := profile.SelectPiMCPContract(existing, provider, version)
	if err != nil {
		return server, mcpProviderAuthoringPlan{}, err
	}
	blocks, err := declarationcodec.ScanMCPServerBlocks(content)
	if err != nil {
		return server, mcpProviderAuthoringPlan{}, err
	}
	contracts := []profile.PiMCPContract{contract}
	for _, block := range blocks {
		peerKey, err := mcpServerAuthoringKeyFor(block.Server, header, "existing mcp_server")
		if err != nil {
			return server, mcpProviderAuthoringPlan{}, err
		}
		if peerKey.target == target.TargetPi {
			peer, err := desiredmcp.ParseBackend(peerKey.target, block.Server.Backend)
			if err != nil {
				return server, mcpProviderAuthoringPlan{}, err
			}
			peerContract, err := profile.PiMCPContractForBackend(peer)
			if err != nil {
				return server, mcpProviderAuthoringPlan{}, err
			}
			contracts = append(contracts, peerContract)
		}
	}
	if err := profile.AdmitPiMCPContext(contracts, provider); err != nil {
		return server, mcpProviderAuthoringPlan{}, err
	}
	if !contract.RequiresProvider() {
		server.Backend = string(contract.Backend())
		if err := validateCanonicalMCPServerAuthoring(server); err != nil {
			return server, mcpProviderAuthoringPlan{}, err
		}
		return server, mcpProviderAuthoringPlan{warnings: []string{
			"Pi MCP records the native backend; enabled servers connect at host startup, and project activation remains subject to Pi trust and builtin selection",
		}}, nil
	}
	plan, err := planMCPProviderAuthoring(content, header, key.target, key.scope)
	if err == nil && existing == "" && !provider && !version.NativeCompatible() {
		plan.warnings = append(plan.warnings, "Pi version is unavailable or outside the admitted native range; adapter authoring was retained without claiming native capability")
	}
	return server, plan, err
}
