package topology

import "sort"

// Graph is an immutable validated set of structural subjects and relations.
type Graph struct {
	subjects map[SubjectID]struct{}
	outgoing map[SubjectID][]Edge
}

// NewGraph validates subjects and edges and returns a deterministic structural graph.
func NewGraph(subjects []SubjectID, edges []Edge) (Graph, error) {
	builder := newGraphBuilder(len(subjects), len(edges))
	if err := builder.addSubjects(subjects); err != nil {
		return Graph{}, err
	}
	if err := builder.addEdges(edges); err != nil {
		return Graph{}, err
	}
	if err := builder.validateConsumedByAcyclic(); err != nil {
		return Graph{}, err
	}
	if err := builder.validateContributionProviders(); err != nil {
		return Graph{}, err
	}
	return builder.graph(), nil
}

// Subjects returns graph subjects in deterministic identity order.
func (graph Graph) Subjects() []SubjectID {
	result := make([]SubjectID, 0, len(graph.subjects))
	for subject := range graph.subjects {
		result = append(result, subject)
	}
	sortSubjectIDs(result)
	return result
}

// Contains reports whether subject is a member of the graph.
func (graph Graph) Contains(subject SubjectID) bool {
	_, ok := graph.subjects[subject]
	return ok
}

// DependenciesOf returns direct depends_on targets in deterministic order.
func (graph Graph) DependenciesOf(subject SubjectID) []SubjectID {
	return graph.targetsFor(subject, EdgeDependsOn)
}

// LauncherDependenciesOf returns direct launches_via targets in deterministic order.
func (graph Graph) LauncherDependenciesOf(subject SubjectID) []SubjectID {
	return graph.targetsFor(subject, EdgeLaunchesVia)
}

// ProviderOf returns the exact carrier provider of a contribution. Valid
// graphs guarantee one provider for every contribution.
func (graph Graph) ProviderOf(contribution SubjectID) (SubjectID, bool) {
	providers := graph.targetsFor(contribution, EdgeProvidedBy)
	if len(providers) != 1 {
		return SubjectID{}, false
	}
	return providers[0], true
}

// BoundTargetsOf returns the projection or binding subjects to which one
// structural provider or artifact subject is bound.
func (graph Graph) BoundTargetsOf(subject SubjectID) []SubjectID {
	return graph.targetsFor(subject, EdgeBoundTo)
}

// ConsumersOf returns direct consumed_by targets in deterministic order.
func (graph Graph) ConsumersOf(subject SubjectID) []SubjectID {
	return graph.targetsFor(subject, EdgeConsumedBy)
}

// ConsumedSubjectsOf returns subjects directly consumed by consumer in
// deterministic order.
func (graph Graph) ConsumedSubjectsOf(consumer SubjectID) []SubjectID {
	result := make([]SubjectID, 0)
	for source, edges := range graph.outgoing {
		for _, edge := range edges {
			if edge.kind == EdgeConsumedBy && edge.target == consumer {
				result = append(result, source)
			}
		}
	}
	sortSubjectIDs(result)
	return result
}

func (graph Graph) targetsFor(subject SubjectID, kind EdgeKind) []SubjectID {
	edges := graph.outgoing[subject]
	result := make([]SubjectID, 0, len(edges))
	for _, edge := range edges {
		if edge.kind == kind {
			result = append(result, edge.target)
		}
	}
	sortSubjectIDs(result)
	return result
}

func sortSubjectIDs(subjects []SubjectID) {
	sort.Slice(subjects, func(left int, right int) bool {
		return CompareSubjectID(subjects[left], subjects[right]) < 0
	})
}
