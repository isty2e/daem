package mcp

import (
	"fmt"
	"sort"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/topology"
)

// ProviderSelection carries the two canonical structural subjects required to
// bind one provider contribution to an MCP projection. Provider-family
// semantics remain owned by the selecting refinement boundary.
type ProviderSelection struct {
	provider     topology.SubjectID
	contribution topology.SubjectID
}

// NewProviderSelection constructs one structural provider selection.
func NewProviderSelection(
	provider topology.SubjectID,
	contribution topology.SubjectID,
) (ProviderSelection, error) {
	if err := provider.Validate(); err != nil {
		return ProviderSelection{}, fmt.Errorf("MCP provider subject: %w", err)
	}
	if provider.Kind() != topology.SubjectCarrier {
		return ProviderSelection{}, fmt.Errorf(
			"MCP provider subject kind must be %q, got %q",
			topology.SubjectCarrier,
			provider.Kind(),
		)
	}
	if err := contribution.Validate(); err != nil {
		return ProviderSelection{}, fmt.Errorf("MCP contribution subject: %w", err)
	}
	if contribution.Kind() != topology.SubjectContribution {
		return ProviderSelection{}, fmt.Errorf(
			"MCP contribution subject kind must be %q, got %q",
			topology.SubjectContribution,
			contribution.Kind(),
		)
	}
	return ProviderSelection{provider: provider, contribution: contribution}, nil
}

// Binding lowers one canonical MCP server binding into structural topology.
func Binding(server desiredmcp.Server, binding desiredmcp.Binding) (topology.Graph, error) {
	builder := newGraphBuilder()
	if err := builder.addBinding(server, binding, "mcp_server.binding"); err != nil {
		return topology.Graph{}, err
	}
	return topology.NewGraph(builder.subjectList(), builder.edgeList())
}

// BindingWithProvider lowers one binding together with its explicitly selected
// provider contribution.
func BindingWithProvider(
	server desiredmcp.Server,
	binding desiredmcp.Binding,
	selection ProviderSelection,
) (topology.Graph, error) {
	builder := newGraphBuilder()
	if err := builder.addBinding(server, binding, "mcp_server.binding"); err != nil {
		return topology.Graph{}, err
	}
	projection, err := ProjectionSubject(binding.Target(), binding.Scope(), server.ID().Name())
	if err != nil {
		return topology.Graph{}, err
	}
	if err := builder.addProviderSelection(projection, selection, "mcp_server.binding.provider"); err != nil {
		return topology.Graph{}, err
	}
	return topology.NewGraph(builder.subjectList(), builder.edgeList())
}

// Servers lowers canonical MCP server bindings into structural topology.
func Servers(servers []desiredmcp.Server) (topology.Graph, error) {
	return serversWithProviderSelections(servers, nil)
}

// ServersWithProviderSelections lowers canonical MCP bindings and the exact
// provider contributions selected for provider-mediated projections. The map
// key is the canonical MCP projection SubjectID.
func ServersWithProviderSelections(
	servers []desiredmcp.Server,
	selections map[topology.SubjectID]ProviderSelection,
) (topology.Graph, error) {
	return serversWithProviderSelections(servers, selections)
}

func serversWithProviderSelections(
	servers []desiredmcp.Server,
	selections map[topology.SubjectID]ProviderSelection,
) (topology.Graph, error) {
	builder := newGraphBuilder()
	consumedSelections := make(map[topology.SubjectID]struct{}, len(selections))
	for index, server := range servers {
		if err := builder.addServer(server, fmt.Sprintf("mcp_server[%d]", index)); err != nil {
			return topology.Graph{}, err
		}
		for _, binding := range server.Bindings() {
			projection, err := ProjectionSubject(binding.Target(), binding.Scope(), server.ID().Name())
			if err != nil {
				return topology.Graph{}, err
			}
			selection, selected := selections[projection]
			if !selected {
				continue
			}
			if err := builder.addProviderSelection(
				projection,
				selection,
				fmt.Sprintf("mcp_server[%d].provider", index),
			); err != nil {
				return topology.Graph{}, err
			}
			consumedSelections[projection] = struct{}{}
		}
	}
	for projection := range selections {
		if _, consumed := consumedSelections[projection]; !consumed {
			return topology.Graph{}, fmt.Errorf(
				"MCP provider selection references unknown projection %q",
				projection,
			)
		}
	}
	return topology.NewGraph(builder.subjectList(), builder.edgeList())
}

type graphBuilder struct {
	subjects map[topology.SubjectID]struct{}
	edges    map[topology.Edge]struct{}
}

func newGraphBuilder() graphBuilder {
	return graphBuilder{
		subjects: make(map[topology.SubjectID]struct{}),
		edges:    make(map[topology.Edge]struct{}),
	}
}

func (builder *graphBuilder) addServer(server desiredmcp.Server, context string) error {
	if err := server.Validate(); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	for index, binding := range server.Bindings() {
		if err := builder.addServerBinding(server.ID().Name(), binding, fmt.Sprintf("%s.binding[%d]", context, index)); err != nil {
			return err
		}
	}
	return nil
}

func (builder *graphBuilder) addBinding(server desiredmcp.Server, binding desiredmcp.Binding, context string) error {
	if err := server.Validate(); err != nil {
		return fmt.Errorf("%s.server: %w", context, err)
	}
	projection, err := ProjectionSubject(binding.Target(), binding.Scope(), server.ID().Name())
	if err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	if !server.OwnsBinding(binding) {
		return fmt.Errorf("%s: binding is not owned by MCP server %q", context, server.ID().Name())
	}
	return builder.addProjectionBinding(projection, binding, context)
}

func (builder *graphBuilder) addServerBinding(serverName string, binding desiredmcp.Binding, context string) error {
	projection, err := ProjectionSubject(binding.Target(), binding.Scope(), serverName)
	if err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return builder.addProjectionBinding(projection, binding, context)
}

func (builder *graphBuilder) addProjectionBinding(
	projection topology.SubjectID,
	binding desiredmcp.Binding,
	context string,
) error {
	if _, exists := builder.subjects[projection]; exists {
		serverID, _ := ServerID(projection)
		return fmt.Errorf("%s: duplicate MCP projection subject %q for id %q", context, projection, serverID)
	}
	builder.addSubject(projection)

	stdio, ok := binding.Transport().Stdio()
	if !ok {
		return fmt.Errorf("%s: unsupported MCP transport %q", context, binding.Transport().Kind())
	}
	if err := builder.addCommand(stdio, projection, context); err != nil {
		return err
	}
	if err := builder.addEnvironmentReferences(stdio, projection, context); err != nil {
		return err
	}
	return nil
}

func (builder *graphBuilder) addCommand(stdio desiredmcp.Stdio, projection topology.SubjectID, context string) error {
	command := stdio.Command()
	dependency, err := ExecutableSubject(command.Executable())
	if err != nil {
		return fmt.Errorf("%s.command: %w", context, err)
	}
	builder.addSubject(dependency)
	builder.addEdge(topology.NewEdge(topology.EdgeLaunchesVia, projection, dependency))
	return nil
}

func (builder *graphBuilder) addEnvironmentReferences(stdio desiredmcp.Stdio, projection topology.SubjectID, context string) error {
	env := stdio.Env()
	slots := make([]string, 0, len(env))
	for slot := range env {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	for _, slot := range slots {
		reference, err := EnvironmentReferenceSubject(env[slot].FromEnv())
		if err != nil {
			return fmt.Errorf("%s.env.%s.from_env: %w", context, slot, err)
		}
		builder.addSubject(reference)
		builder.addEdge(topology.NewEdge(topology.EdgeDependsOn, projection, reference))
	}
	return nil
}

func (builder *graphBuilder) addProviderSelection(
	projection topology.SubjectID,
	selection ProviderSelection,
	context string,
) error {
	if !builder.hasSubject(projection) {
		return fmt.Errorf("%s: projection %q is not part of the MCP graph", context, projection)
	}
	canonical, err := NewProviderSelection(selection.provider, selection.contribution)
	if err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	if canonical != selection {
		return fmt.Errorf("%s: MCP provider selection is not canonical", context)
	}
	builder.addSubject(selection.provider)
	builder.addSubject(selection.contribution)
	builder.addEdge(topology.NewEdge(
		topology.EdgeProvidedBy,
		selection.contribution,
		selection.provider,
	))
	builder.addEdge(topology.NewEdge(
		topology.EdgeBoundTo,
		selection.contribution,
		projection,
	))
	return nil
}

func (builder *graphBuilder) addSubject(subject topology.SubjectID) {
	builder.subjects[subject] = struct{}{}
}

func (builder graphBuilder) hasSubject(subject topology.SubjectID) bool {
	_, exists := builder.subjects[subject]
	return exists
}

func (builder *graphBuilder) addEdge(edge topology.Edge) {
	builder.edges[edge] = struct{}{}
}

func (builder graphBuilder) subjectList() []topology.SubjectID {
	subjects := make([]topology.SubjectID, 0, len(builder.subjects))
	for subject := range builder.subjects {
		subjects = append(subjects, subject)
	}
	sort.Slice(subjects, func(left int, right int) bool {
		return topology.CompareSubjectID(subjects[left], subjects[right]) < 0
	})
	return subjects
}

func (builder graphBuilder) edgeList() []topology.Edge {
	edges := make([]topology.Edge, 0, len(builder.edges))
	for edge := range builder.edges {
		edges = append(edges, edge)
	}
	return edges
}
