package host

import (
	"fmt"
	"path/filepath"
	"strings"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

type normalSourceSpec struct {
	id     string
	path   string
	shared bool
}

type observedNormalSource struct {
	evidence mcpeffective.SourceObservation
	document normalDocument
}

// PiAdapterInput names the physical source roots used by one current provider
// process. Inputs are explicit so tests never depend on the invoking machine.
type PiAdapterInput struct {
	Contract     lock.LockedSubjectContract
	HomeDir      string
	WorkDir      string
	AgentRoot    string
	SelectedPath string
}

// ObservePiAdapter reads the active pi-mcp-adapter config sources for one
// locked projection. Shared and imported files remain passive evidence only.
func ObservePiAdapter(input PiAdapterInput) (mcpeffective.Observation, error) {
	placement, ok := aggregate.MCPPlacementForSubject(input.Contract.SubjectID())
	if !ok ||
		(placement.ID() != aggregate.MCPPlacementPiProject &&
			placement.ID() != aggregate.MCPPlacementPiGlobal) {
		return mcpeffective.Observation{}, fmt.Errorf(
			"effective Pi MCP observation requires a Pi adapter projection",
		)
	}
	provider, present := input.Contract.MCPProviderContribution()
	if !present || provider.Kind() != "mcp-client" || provider.Key() != "default" {
		return mcpeffective.Observation{}, fmt.Errorf(
			"effective Pi MCP observation requires mcp-client/default provider contribution",
		)
	}
	serverName, ok := topologymcp.ServerID(input.Contract.SubjectID())
	if !ok {
		return mcpeffective.Observation{}, fmt.Errorf("effective Pi MCP subject has no server identity")
	}
	homeDir, err := cleanAbsoluteDirectory("home directory", input.HomeDir)
	if err != nil {
		return mcpeffective.Observation{}, err
	}
	workDir, err := cleanAbsoluteDirectory("work directory", input.WorkDir)
	if err != nil {
		return mcpeffective.Observation{}, err
	}
	agentRoot, err := cleanAbsoluteDirectory("Pi agent root", input.AgentRoot)
	if err != nil {
		return mcpeffective.Observation{}, err
	}
	selectedPath, err := cleanAbsolutePath("selected Pi MCP path", input.SelectedPath)
	if err != nil {
		return mcpeffective.Observation{}, err
	}

	specs := piNormalSourceSpecs(homeDir, workDir, agentRoot)
	selectedIndex := -1
	for index, spec := range specs {
		if spec.path == selectedPath {
			selectedIndex = index
			break
		}
	}
	if selectedIndex < 0 {
		return mcpeffective.Observation{}, fmt.Errorf(
			"selected Pi MCP path %q is not an active provider source",
			selectedPath,
		)
	}

	sources := make([]mcpeffective.SourceObservation, 0, len(specs))
	documents := make([]observedNormalSource, 0, len(specs))
	hostConfigDiscovery := "off"
	for index, spec := range specs {
		precedence := relativePrecedence(index, selectedIndex)
		observed := observeNormalSource(spec, precedence, serverName)
		sources = append(sources, observed.evidence)
		documents = append(documents, observed)
		if observed.evidence.State() == mcpeffective.SourceExact &&
			observed.document.hostConfigDiscovery != "" {
			hostConfigDiscovery = observed.document.hostConfigDiscovery
		}
	}

	for index, observed := range documents {
		if observed.evidence.State() != mcpeffective.SourceExact {
			continue
		}
		precedence := relativePrecedence(index, selectedIndex)
		if index == selectedIndex {
			precedence = mcpeffective.PrecedenceLower
		}
		for _, kind := range observed.document.imports {
			imported, present := observeImport(
				kind,
				"import/"+observed.evidence.ID()+"/"+string(kind),
				precedence,
				homeDir,
				workDir,
				serverName,
				mcpeffective.SourceImport,
			)
			if present {
				sources = append(sources, imported)
			}
		}
	}
	if hostConfigDiscovery == "on" {
		for _, kind := range orderedImportKinds {
			discovered, present := observeImport(
				kind,
				"host-discovery/"+string(kind),
				mcpeffective.PrecedenceLower,
				homeDir,
				workDir,
				serverName,
				mcpeffective.SourceHostDiscovery,
			)
			if present {
				sources = append(sources, discovered)
			}
		}
	}
	return mcpeffective.NewObservation(mcpeffective.ObservationInput{
		Subject:      input.Contract.SubjectID(),
		ServerName:   serverName,
		SelectedPath: selectedPath,
		Sources:      sources,
	})
}

func piNormalSourceSpecs(homeDir string, workDir string, agentRoot string) []normalSourceSpec {
	genericGlobal := filepath.Join(homeDir, ".config", "mcp", "mcp.json")
	agentsGlobal := filepath.Join(homeDir, ".agents", "mcp.json")
	agentsNested := filepath.Join(homeDir, ".agents", "mcp", "mcp.json")
	piGlobal := filepath.Join(agentRoot, "mcp.json")
	sharedProject := filepath.Join(workDir, ".mcp.json")
	piProject := filepath.Join(workDir, ".pi", "mcp.json")

	candidates := []normalSourceSpec{
		{id: "shared-global", path: genericGlobal, shared: true},
		{id: "agents-global", path: agentsGlobal, shared: true},
		{id: "agents-nested-global", path: agentsNested, shared: true},
		{id: "pi-global", path: piGlobal},
		{id: "shared-project", path: sharedProject, shared: true},
		{id: "pi-project", path: piProject},
	}
	result := make([]normalSourceSpec, 0, len(candidates))
	seenPaths := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, duplicate := seenPaths[candidate.path]; duplicate {
			continue
		}
		seenPaths[candidate.path] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func observeNormalSource(
	spec normalSourceSpec,
	precedence mcpeffective.RelativePrecedence,
	serverName string,
) observedNormalSource {
	content, exists, readErr := readStableRegularFile(spec.path)
	if readErr != nil {
		return observedNormalSource{
			evidence: mustSourceObservation(mcpeffective.SourceObservationInput{
				ID: spec.id, Path: spec.path, Kind: mcpeffective.SourceNormal, Precedence: precedence,
				Shared: spec.shared, State: mcpeffective.SourceOpaque, Detail: readErr.Error(),
			}),
		}
	}
	if !exists {
		return observedNormalSource{
			evidence: mustSourceObservation(mcpeffective.SourceObservationInput{
				ID: spec.id, Path: spec.path, Kind: mcpeffective.SourceNormal, Precedence: precedence,
				Shared: spec.shared, State: mcpeffective.SourceAbsent,
			}),
		}
	}
	document, decodeErr := decodeNormalDocument(content)
	if decodeErr != nil {
		return observedNormalSource{
			evidence: mustSourceObservation(mcpeffective.SourceObservationInput{
				ID: spec.id, Path: spec.path, Kind: mcpeffective.SourceNormal, Precedence: precedence,
				Shared: spec.shared, State: mcpeffective.SourceOpaque, Detail: decodeErr.Error(),
			}),
		}
	}
	_, defines := document.serverNames[serverName]
	return observedNormalSource{
		evidence: mustSourceObservation(mcpeffective.SourceObservationInput{
			ID: spec.id, Path: spec.path, Kind: mcpeffective.SourceNormal, Precedence: precedence,
			Shared: spec.shared, State: mcpeffective.SourceExact, DefinesSelectedName: defines,
		}),
		document: document,
	}
}

func relativePrecedence(index int, selectedIndex int) mcpeffective.RelativePrecedence {
	switch {
	case index < selectedIndex:
		return mcpeffective.PrecedenceLower
	case index > selectedIndex:
		return mcpeffective.PrecedenceHigher
	default:
		return mcpeffective.PrecedenceSelected
	}
}

func cleanAbsoluteDirectory(label string, value string) (string, error) {
	return cleanAbsolutePath(label, value)
}

func cleanAbsolutePath(label string, value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("%s must be non-empty, trimmed, and NUL-free", label)
	}
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return "", fmt.Errorf("%s %q must be absolute and clean", label, value)
	}
	return value, nil
}

func mustSourceObservation(input mcpeffective.SourceObservationInput) mcpeffective.SourceObservation {
	source, err := mcpeffective.NewSourceObservation(input)
	if err != nil {
		panic(err)
	}
	return source
}
