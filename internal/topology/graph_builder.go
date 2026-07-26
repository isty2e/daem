package topology

import (
	"fmt"
	"sort"
)

type graphBuilder struct {
	subjects      map[SubjectID]struct{}
	outgoing      map[SubjectID][]Edge
	providerCount map[SubjectID]int
	seenEdges     map[Edge]struct{}
}

func newGraphBuilder(subjectCount int, edgeCount int) graphBuilder {
	return graphBuilder{
		subjects:      make(map[SubjectID]struct{}, subjectCount),
		outgoing:      make(map[SubjectID][]Edge),
		providerCount: make(map[SubjectID]int),
		seenEdges:     make(map[Edge]struct{}, edgeCount),
	}
}

func (builder graphBuilder) graph() Graph {
	return Graph{
		subjects: builder.subjects,
		outgoing: builder.outgoing,
	}
}

func (builder *graphBuilder) addSubjects(subjects []SubjectID) error {
	ordered := append([]SubjectID(nil), subjects...)
	sortSubjectIDs(ordered)
	for _, subject := range ordered {
		if err := subject.Validate(); err != nil {
			return validationError(ReasonInvalidSubject, subject.String(), err.Error())
		}
		if _, exists := builder.subjects[subject]; exists {
			return validationError(ReasonDuplicateSubject, subject.String(), "duplicate topology subject")
		}
		builder.subjects[subject] = struct{}{}
	}
	return nil
}

func (builder *graphBuilder) addEdges(edges []Edge) error {
	ordered := append([]Edge(nil), edges...)
	sort.Slice(ordered, func(left int, right int) bool {
		return compareEdge(ordered[left], ordered[right]) < 0
	})
	for _, edge := range ordered {
		if err := builder.validateEdge(edge); err != nil {
			return err
		}
		if _, exists := builder.seenEdges[edge]; exists {
			return validationError(ReasonDuplicateEdge, edge.source.String(), fmt.Sprintf("duplicate edge %q", edge.kind))
		}
		builder.seenEdges[edge] = struct{}{}
		builder.outgoing[edge.source] = append(builder.outgoing[edge.source], edge)
		if edge.kind == EdgeProvidedBy {
			builder.providerCount[edge.source]++
		}
	}
	return nil
}

func (builder graphBuilder) validateEdge(edge Edge) error {
	if _, ok := builder.subjects[edge.source]; !ok {
		return validationError(ReasonDanglingEdge, edge.source.String(), "edge source is not a graph subject")
	}
	if _, ok := builder.subjects[edge.target]; !ok {
		return validationError(ReasonDanglingEdge, edge.target.String(), "edge target is not a graph subject")
	}
	if edge.source == edge.target {
		return validationError(ReasonInvalidEdgeEndpoint, edge.source.String(), "self edge is not allowed")
	}
	if !edgeEndpointAllowed(edge.kind, edge.source.Kind(), edge.target.Kind()) {
		return validationError(
			ReasonInvalidEdgeEndpoint,
			edge.source.String(),
			fmt.Sprintf("invalid %q edge from %q to %q", edge.kind, edge.source.Kind(), edge.target.Kind()),
		)
	}
	return nil
}

func (builder graphBuilder) validateConsumedByAcyclic() error {
	const (
		unvisited uint8 = iota
		visiting
		visited
	)
	states := make(map[SubjectID]uint8, len(builder.subjects))
	var visit func(SubjectID) error
	visit = func(subject SubjectID) error {
		switch states[subject] {
		case visiting:
			return validationError(ReasonCyclicRelation, subject.String(), "consumed_by relation contains a cycle")
		case visited:
			return nil
		}
		states[subject] = visiting
		for _, edge := range builder.outgoing[subject] {
			if edge.kind != EdgeConsumedBy {
				continue
			}
			if err := visit(edge.target); err != nil {
				return err
			}
		}
		states[subject] = visited
		return nil
	}

	subjects := make([]SubjectID, 0, len(builder.subjects))
	for subject := range builder.subjects {
		subjects = append(subjects, subject)
	}
	sortSubjectIDs(subjects)
	for _, subject := range subjects {
		if states[subject] != unvisited {
			continue
		}
		if err := visit(subject); err != nil {
			return err
		}
	}
	return nil
}

func (builder graphBuilder) validateContributionProviders() error {
	subjects := make([]SubjectID, 0, len(builder.subjects))
	for subject := range builder.subjects {
		if subject.Kind() == SubjectContribution {
			subjects = append(subjects, subject)
		}
	}
	sortSubjectIDs(subjects)
	for _, subject := range subjects {
		switch builder.providerCount[subject] {
		case 0:
			return validationError(ReasonMissingProvider, subject.String(), "contribution requires a carrier provider")
		case 1:
		default:
			return validationError(ReasonMultipleProviders, subject.String(), "contribution has multiple carrier providers")
		}
	}
	return nil
}

func edgeEndpointAllowed(kind EdgeKind, source SubjectKind, target SubjectKind) bool {
	bindingLike := func(value SubjectKind) bool {
		return value == SubjectProjection || value == SubjectBinding
	}

	switch kind {
	case EdgeProvidedBy:
		return source == SubjectContribution && target == SubjectCarrier
	case EdgeBoundTo:
		return bindingLike(target) &&
			(source == SubjectCarrier || source == SubjectContribution || source == SubjectProvisionedArtifact)
	case EdgeLaunchesVia:
		return (bindingLike(source) || source == SubjectContribution) &&
			(target == SubjectProvisionedArtifact || target == SubjectRuntimeDependency)
	case EdgeDependsOn:
		return (bindingLike(source) ||
			source == SubjectCarrier ||
			source == SubjectContribution ||
			source == SubjectProvisionedArtifact) &&
			(target == SubjectRuntimeDependency || target == SubjectCredentialReference)
	case EdgeConsumedBy:
		return ((bindingLike(source) || source == SubjectProvisionedArtifact) &&
			(bindingLike(target) || target == SubjectCarrier || target == SubjectContribution)) ||
			(source == SubjectCarrier && target == SubjectHostRelation)
	default:
		return false
	}
}
