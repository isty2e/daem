package topology

import (
	"errors"
	"slices"
	"testing"
)

func TestGraphAcceptsCompleteStructuralRelationMatrix(t *testing.T) {
	projection := graphSubject(t, SubjectProjection, "projection", "project")
	binding := graphSubject(t, SubjectBinding, "binding", "global")
	hostRelation := graphSubject(t, SubjectHostRelation, "host-relation", "plugin")
	carrier := graphSubject(t, SubjectCarrier, "carrier", "bundle")
	contribution := graphSubject(t, SubjectContribution, "contribution", "skill")
	artifact := graphSubject(t, SubjectProvisionedArtifact, "artifact", "runner")
	runtime := graphSubject(t, SubjectRuntimeDependency, "executable", "npx")
	credential := graphSubject(t, SubjectCredentialReference, "env", "TOKEN")

	graph, err := NewGraph(
		[]SubjectID{credential, runtime, artifact, contribution, carrier, hostRelation, binding, projection},
		[]Edge{
			NewEdge(EdgeProvidedBy, contribution, carrier),
			NewEdge(EdgeBoundTo, carrier, projection),
			NewEdge(EdgeBoundTo, contribution, binding),
			NewEdge(EdgeBoundTo, artifact, projection),
			NewEdge(EdgeLaunchesVia, projection, runtime),
			NewEdge(EdgeLaunchesVia, binding, artifact),
			NewEdge(EdgeLaunchesVia, contribution, runtime),
			NewEdge(EdgeDependsOn, projection, credential),
			NewEdge(EdgeDependsOn, binding, credential),
			NewEdge(EdgeDependsOn, carrier, runtime),
			NewEdge(EdgeDependsOn, contribution, credential),
			NewEdge(EdgeDependsOn, artifact, runtime),
			NewEdge(EdgeConsumedBy, projection, binding),
			NewEdge(EdgeConsumedBy, binding, carrier),
			NewEdge(EdgeConsumedBy, artifact, contribution),
			NewEdge(EdgeConsumedBy, carrier, hostRelation),
		},
	)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}
	if !graph.Contains(projection) || !graph.Contains(credential) {
		t.Fatal("Graph does not contain admitted subjects")
	}
	if got := graph.DependenciesOf(projection); !slices.Equal(got, []SubjectID{credential}) {
		t.Fatalf("DependenciesOf(projection) = %v, want [%v]", got, credential)
	}
	if got := graph.LauncherDependenciesOf(binding); !slices.Equal(got, []SubjectID{artifact}) {
		t.Fatalf("LauncherDependenciesOf(binding) = %v, want [%v]", got, artifact)
	}
	if got := graph.ConsumersOf(artifact); !slices.Equal(got, []SubjectID{contribution}) {
		t.Fatalf("ConsumersOf(artifact) = %v, want [%v]", got, contribution)
	}
	if got := graph.ConsumedSubjectsOf(contribution); !slices.Equal(got, []SubjectID{artifact}) {
		t.Fatalf("ConsumedSubjectsOf(contribution) = %v, want [%v]", got, artifact)
	}
	if got := graph.ConsumersOf(carrier); !slices.Equal(got, []SubjectID{hostRelation}) {
		t.Fatalf("ConsumersOf(carrier) = %v, want [%v]", got, hostRelation)
	}
}

func TestGraphRejectsInvalidConstruction(t *testing.T) {
	projection := graphSubject(t, SubjectProjection, "projection", "server")
	carrier := graphSubject(t, SubjectCarrier, "carrier", "bundle")
	contribution := graphSubject(t, SubjectContribution, "contribution", "skill")
	runtime := graphSubject(t, SubjectRuntimeDependency, "executable", "npx")
	otherRuntime := graphSubject(t, SubjectRuntimeDependency, "executable", "uvx")

	tests := []struct {
		name     string
		subjects []SubjectID
		edges    []Edge
		reason   ReasonCode
	}{
		{
			name:     "zero subject",
			subjects: []SubjectID{{}},
			reason:   ReasonInvalidSubject,
		},
		{
			name:     "duplicate subject",
			subjects: []SubjectID{projection, projection},
			reason:   ReasonDuplicateSubject,
		},
		{
			name:     "dangling source",
			subjects: []SubjectID{runtime},
			edges:    []Edge{NewEdge(EdgeLaunchesVia, projection, runtime)},
			reason:   ReasonDanglingEdge,
		},
		{
			name:     "dangling target",
			subjects: []SubjectID{projection},
			edges:    []Edge{NewEdge(EdgeLaunchesVia, projection, runtime)},
			reason:   ReasonDanglingEdge,
		},
		{
			name:     "duplicate edge",
			subjects: []SubjectID{projection, runtime},
			edges: []Edge{
				NewEdge(EdgeLaunchesVia, projection, runtime),
				NewEdge(EdgeLaunchesVia, projection, runtime),
			},
			reason: ReasonDuplicateEdge,
		},
		{
			name:     "self edge",
			subjects: []SubjectID{projection},
			edges:    []Edge{NewEdge(EdgeConsumedBy, projection, projection)},
			reason:   ReasonInvalidEdgeEndpoint,
		},
		{
			name:     "unknown relation",
			subjects: []SubjectID{projection, runtime},
			edges:    []Edge{NewEdge(EdgeKind("future"), projection, runtime)},
			reason:   ReasonInvalidEdgeEndpoint,
		},
		{
			name:     "invalid endpoint roles",
			subjects: []SubjectID{projection, runtime},
			edges:    []Edge{NewEdge(EdgeProvidedBy, projection, runtime)},
			reason:   ReasonInvalidEdgeEndpoint,
		},
		{
			name:     "missing contribution provider",
			subjects: []SubjectID{contribution},
			reason:   ReasonMissingProvider,
		},
		{
			name:     "multiple contribution providers",
			subjects: []SubjectID{contribution, carrier, projection},
			edges: []Edge{
				NewEdge(EdgeProvidedBy, contribution, carrier),
				NewEdge(EdgeProvidedBy, contribution, projection),
			},
			reason: ReasonInvalidEdgeEndpoint,
		},
		{
			name:     "two valid providers",
			subjects: []SubjectID{contribution, carrier, graphSubject(t, SubjectCarrier, "carrier", "other")},
			edges: []Edge{
				NewEdge(EdgeProvidedBy, contribution, carrier),
				NewEdge(EdgeProvidedBy, contribution, graphSubject(t, SubjectCarrier, "carrier", "other")),
			},
			reason: ReasonMultipleProviders,
		},
		{
			name:     "runtime cannot launch runtime",
			subjects: []SubjectID{runtime, otherRuntime},
			edges:    []Edge{NewEdge(EdgeLaunchesVia, runtime, otherRuntime)},
			reason:   ReasonInvalidEdgeEndpoint,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewGraph(test.subjects, test.edges)
			assertGraphReason(t, err, test.reason)
		})
	}
}

func TestGraphRejectsConsumedByCyclesButAllowsMixedRelationCycle(t *testing.T) {
	first := graphSubject(t, SubjectProjection, "projection", "first")
	second := graphSubject(t, SubjectBinding, "binding", "second")

	_, err := NewGraph(
		[]SubjectID{first, second},
		[]Edge{
			NewEdge(EdgeConsumedBy, first, second),
			NewEdge(EdgeConsumedBy, second, first),
		},
	)
	assertGraphReason(t, err, ReasonCyclicRelation)

	artifact := graphSubject(t, SubjectProvisionedArtifact, "artifact", "runner")
	if _, err := NewGraph(
		[]SubjectID{first, artifact},
		[]Edge{
			NewEdge(EdgeConsumedBy, artifact, first),
			NewEdge(EdgeLaunchesVia, first, artifact),
		},
	); err != nil {
		t.Fatalf("mixed relation cycle was rejected: %v", err)
	}
}

func TestGraphCycleFailureIsIndependentOfEdgeInputOrder(t *testing.T) {
	root := graphSubject(t, SubjectBinding, "binding", "00-root")
	first := graphSubject(t, SubjectBinding, "binding", "10-first")
	firstTail := graphSubject(t, SubjectBinding, "binding", "11-first-tail")
	second := graphSubject(t, SubjectBinding, "binding", "20-second")
	secondTail := graphSubject(t, SubjectBinding, "binding", "21-second-tail")
	edges := []Edge{
		NewEdge(EdgeConsumedBy, root, first),
		NewEdge(EdgeConsumedBy, root, second),
		NewEdge(EdgeConsumedBy, first, firstTail),
		NewEdge(EdgeConsumedBy, firstTail, first),
		NewEdge(EdgeConsumedBy, second, secondTail),
		NewEdge(EdgeConsumedBy, secondTail, second),
	}
	subjects := []SubjectID{secondTail, root, firstTail, second, first}

	_, forwardError := NewGraph(subjects, edges)
	_, reverseError := NewGraph(
		[]SubjectID{first, second, firstTail, root, secondTail},
		[]Edge{edges[5], edges[4], edges[3], edges[2], edges[1], edges[0]},
	)
	assertGraphReason(t, forwardError, ReasonCyclicRelation)
	assertGraphReason(t, reverseError, ReasonCyclicRelation)

	if forwardError.Error() != reverseError.Error() {
		t.Fatalf("cycle failure depends on edge input order:\nforward: %v\nreverse: %v", forwardError, reverseError)
	}
}

func TestGraphOrdersAndDefensivelyCopiesValues(t *testing.T) {
	projection := graphSubject(t, SubjectProjection, "projection", "server")
	runtimeA := graphSubject(t, SubjectRuntimeDependency, "executable", "a")
	runtimeB := graphSubject(t, SubjectRuntimeDependency, "executable", "b")
	subjects := []SubjectID{runtimeB, projection, runtimeA}
	edges := []Edge{
		NewEdge(EdgeDependsOn, projection, runtimeB),
		NewEdge(EdgeLaunchesVia, projection, runtimeA),
	}
	originalSubjects := slices.Clone(subjects)
	originalEdges := slices.Clone(edges)

	graph, err := NewGraph(subjects, edges)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}
	if !slices.Equal(subjects, originalSubjects) || !slices.Equal(edges, originalEdges) {
		t.Fatalf("NewGraph mutated caller inputs: subjects=%v edges=%v", subjects, edges)
	}
	subjects[0] = SubjectID{}
	edges[0] = Edge{}

	wantSubjects := []SubjectID{projection, runtimeA, runtimeB}
	sortSubjectIDs(wantSubjects)
	if got := graph.Subjects(); !slices.Equal(got, wantSubjects) {
		t.Fatalf("Subjects() = %v, want %v", got, wantSubjects)
	}
	dependencies := graph.DependenciesOf(projection)
	dependencies[0] = SubjectID{}
	if got := graph.DependenciesOf(projection); !slices.Equal(got, []SubjectID{runtimeB}) {
		t.Fatalf("DependenciesOf() did not return a defensive value: %v", got)
	}
}

func TestGraphEmptyInputIsValid(t *testing.T) {
	graph, err := NewGraph(nil, nil)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}
	if len(graph.Subjects()) != 0 {
		t.Fatalf("empty Graph subjects = %v", graph.Subjects())
	}
}

func graphSubject(t *testing.T, kind SubjectKind, namespace string, key string) SubjectID {
	t.Helper()
	subject, err := NewSubjectID(kind, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID(%q, %q, %q): %v", kind, namespace, key, err)
	}
	return subject
}

func assertGraphReason(t *testing.T, err error, want ReasonCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("NewGraph returned nil error, want %s", want)
	}
	got, ok := graphReasonCodeOf(err)
	if !ok || got != want {
		t.Fatalf("graphReasonCodeOf(%v) = (%q, %t), want (%q, true)", err, got, ok, want)
	}
}

func graphReasonCodeOf(err error) (ReasonCode, bool) {
	var validation *ValidationError
	if errors.As(err, &validation) {
		return validation.Code(), true
	}
	return "", false
}
