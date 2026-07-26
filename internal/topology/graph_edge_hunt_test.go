package topology

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestGraphEdgeHuntExhaustiveEndpointMatrix(t *testing.T) {
	kinds := []SubjectKind{
		SubjectResource,
		SubjectProjection,
		SubjectHostRelation,
		SubjectBinding,
		SubjectCarrier,
		SubjectContribution,
		SubjectProvisionedArtifact,
		SubjectRuntimeDependency,
		SubjectCredentialReference,
	}
	relations := []EdgeKind{
		EdgeProvidedBy,
		EdgeBoundTo,
		EdgeLaunchesVia,
		EdgeDependsOn,
		EdgeConsumedBy,
	}

	for _, relation := range relations {
		for _, sourceKind := range kinds {
			for _, targetKind := range kinds {
				source := graphSubject(t, sourceKind, "matrix", "source")
				target := graphSubject(t, targetKind, "matrix", "target")
				subjects := []SubjectID{source, target}
				edges := []Edge{NewEdge(relation, source, target)}

				if sourceKind == SubjectContribution &&
					!(relation == EdgeProvidedBy && targetKind == SubjectCarrier) {
					provider := graphSubject(t, SubjectCarrier, "matrix-provider", "source")
					subjects = append(subjects, provider)
					edges = append(edges, NewEdge(EdgeProvidedBy, source, provider))
				}
				if targetKind == SubjectContribution {
					provider := graphSubject(t, SubjectCarrier, "matrix-provider", "target")
					subjects = append(subjects, provider)
					edges = append(edges, NewEdge(EdgeProvidedBy, target, provider))
				}

				_, err := NewGraph(subjects, edges)
				if edgeHuntEndpointAllowed(relation, sourceKind, targetKind) {
					if err != nil {
						t.Fatalf("%s %s -> %s was rejected: %v", relation, sourceKind, targetKind, err)
					}
					continue
				}
				if reason, ok := graphReasonCodeOf(err); !ok || reason != ReasonInvalidEdgeEndpoint {
					t.Fatalf("%s %s -> %s error = %v, want %s", relation, sourceKind, targetKind, err, ReasonInvalidEdgeEndpoint)
				}
			}
		}
	}
}

func TestGraphEdgeHuntProviderFailuresRemainDistinct(t *testing.T) {
	contribution := graphSubject(t, SubjectContribution, "provider", "skill")
	first := graphSubject(t, SubjectCarrier, "provider", "first")
	second := graphSubject(t, SubjectCarrier, "provider", "second")

	duplicate := NewEdge(EdgeProvidedBy, contribution, first)
	_, err := NewGraph(
		[]SubjectID{contribution, first},
		[]Edge{duplicate, duplicate},
	)
	assertGraphReason(t, err, ReasonDuplicateEdge)

	_, err = NewGraph(
		[]SubjectID{contribution, first, second},
		[]Edge{
			NewEdge(EdgeProvidedBy, contribution, first),
			NewEdge(EdgeProvidedBy, contribution, second),
		},
	)
	assertGraphReason(t, err, ReasonMultipleProviders)
}

func TestGraphEdgeHuntProviderScopedLocalKeysRemainDistinct(t *testing.T) {
	claudeContribution := graphSubject(t, SubjectContribution, "claude-code.plugin", "shared")
	codexContribution := graphSubject(t, SubjectContribution, "codex.plugin", "shared")
	claudeCarrier := graphSubject(t, SubjectCarrier, "claude-code.marketplace", "owner")
	codexCarrier := graphSubject(t, SubjectCarrier, "codex.marketplace", "owner")

	graph, err := NewGraph(
		[]SubjectID{codexCarrier, claudeContribution, claudeCarrier, codexContribution},
		[]Edge{
			NewEdge(EdgeProvidedBy, codexContribution, codexCarrier),
			NewEdge(EdgeProvidedBy, claudeContribution, claudeCarrier),
		},
	)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}
	if claudeContribution == codexContribution ||
		!graph.Contains(claudeContribution) || !graph.Contains(codexContribution) {
		t.Fatalf("provider-scoped contributions collapsed: claude=%s codex=%s", claudeContribution, codexContribution)
	}
	if got := graph.outgoing[claudeContribution]; len(got) != 1 || got[0].target != claudeCarrier {
		t.Fatalf("Claude contribution providers = %v, want [%s]", got, claudeCarrier)
	}
	if got := graph.outgoing[codexContribution]; len(got) != 1 || got[0].target != codexCarrier {
		t.Fatalf("Codex contribution providers = %v, want [%s]", got, codexCarrier)
	}
}

func TestGraphEdgeHuntMissingProviderFailureIsInputOrderIndependent(t *testing.T) {
	first := graphSubject(t, SubjectContribution, "provider", "a")
	second := graphSubject(t, SubjectContribution, "provider", "z")

	_, forwardError := NewGraph([]SubjectID{second, first}, nil)
	_, reverseError := NewGraph([]SubjectID{first, second}, nil)
	assertGraphReason(t, forwardError, ReasonMissingProvider)
	assertGraphReason(t, reverseError, ReasonMissingProvider)
	if forwardError.Error() != reverseError.Error() {
		t.Fatalf("missing-provider failure depends on subject input order:\nforward: %v\nreverse: %v", forwardError, reverseError)
	}
}

func TestGraphEdgeHuntLongConsumedByChainAndCycle(t *testing.T) {
	const subjectCount = 20000
	subjects := make([]SubjectID, subjectCount)
	edges := make([]Edge, 0, subjectCount)
	for index := range subjects {
		subjects[index] = graphSubject(t, SubjectBinding, "long-chain", fmt.Sprintf("%05d", index))
		if index != 0 {
			edges = append(edges, NewEdge(EdgeConsumedBy, subjects[index-1], subjects[index]))
		}
	}

	if _, err := NewGraph(subjects, edges); err != nil {
		t.Fatalf("long acyclic chain was rejected: %v", err)
	}
	cycleEdges := append(append([]Edge(nil), edges...), NewEdge(EdgeConsumedBy, subjects[len(subjects)-1], subjects[0]))
	_, err := NewGraph(subjects, cycleEdges)
	assertGraphReason(t, err, ReasonCyclicRelation)
}

func TestGraphEdgeHuntConsumedByAllowsSharedFanInAndFanOut(t *testing.T) {
	firstArtifact := graphSubject(t, SubjectProvisionedArtifact, "artifact", "first")
	secondArtifact := graphSubject(t, SubjectProvisionedArtifact, "artifact", "second")
	firstBinding := graphSubject(t, SubjectBinding, "binding", "first")
	secondBinding := graphSubject(t, SubjectBinding, "binding", "second")
	edges := []Edge{
		NewEdge(EdgeConsumedBy, firstArtifact, firstBinding),
		NewEdge(EdgeConsumedBy, firstArtifact, secondBinding),
		NewEdge(EdgeConsumedBy, secondArtifact, secondBinding),
	}

	graph, err := NewGraph(
		[]SubjectID{secondBinding, firstArtifact, firstBinding, secondArtifact},
		[]Edge{edges[2], edges[0], edges[1]},
	)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}
	if got := graph.outgoing[firstArtifact]; !slices.Equal(got, []Edge{edges[0], edges[1]}) {
		t.Fatalf("first artifact consumers = %v, want %v", got, []Edge{edges[0], edges[1]})
	}
	if got := graph.outgoing[secondArtifact]; !slices.Equal(got, []Edge{edges[2]}) {
		t.Fatalf("second artifact consumers = %v, want %v", got, []Edge{edges[2]})
	}
}

func TestGraphEdgeHuntConstructionFailuresAreInputOrderIndependent(t *testing.T) {
	projectionA := graphSubject(t, SubjectProjection, "projection", "a")
	projectionZ := graphSubject(t, SubjectProjection, "projection", "z")
	runtime := graphSubject(t, SubjectRuntimeDependency, "executable", "npx")
	edgeA := NewEdge(EdgeLaunchesVia, projectionA, runtime)
	edgeZ := NewEdge(EdgeLaunchesVia, projectionZ, runtime)
	invalidA := NewEdge(EdgeProvidedBy, projectionA, runtime)
	invalidZ := NewEdge(EdgeLaunchesVia, runtime, projectionZ)

	tests := []struct {
		name            string
		forwardSubjects []SubjectID
		reverseSubjects []SubjectID
		forwardEdges    []Edge
		reverseEdges    []Edge
		wantReason      ReasonCode
	}{
		{
			name:            "duplicate subjects",
			forwardSubjects: []SubjectID{projectionZ, projectionZ, projectionA, projectionA},
			reverseSubjects: []SubjectID{projectionA, projectionA, projectionZ, projectionZ},
			wantReason:      ReasonDuplicateSubject,
		},
		{
			name:            "duplicate edges",
			forwardSubjects: []SubjectID{projectionZ, runtime, projectionA},
			reverseSubjects: []SubjectID{projectionA, runtime, projectionZ},
			forwardEdges:    []Edge{edgeZ, edgeZ, edgeA, edgeA},
			reverseEdges:    []Edge{edgeA, edgeA, edgeZ, edgeZ},
			wantReason:      ReasonDuplicateEdge,
		},
		{
			name:            "invalid endpoints",
			forwardSubjects: []SubjectID{projectionZ, runtime, projectionA},
			reverseSubjects: []SubjectID{projectionA, runtime, projectionZ},
			forwardEdges:    []Edge{invalidA, invalidZ},
			reverseEdges:    []Edge{invalidZ, invalidA},
			wantReason:      ReasonInvalidEdgeEndpoint,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, forwardError := NewGraph(test.forwardSubjects, test.forwardEdges)
			_, reverseError := NewGraph(test.reverseSubjects, test.reverseEdges)
			assertGraphReason(t, forwardError, test.wantReason)
			assertGraphReason(t, reverseError, test.wantReason)
			if forwardError.Error() != reverseError.Error() {
				t.Fatalf("failure depends on input order:\nforward: %v\nreverse: %v", forwardError, reverseError)
			}
		})
	}
}

func TestGraphEdgeHuntConcurrentReadsRemainImmutable(t *testing.T) {
	projection := graphSubject(t, SubjectProjection, "concurrent", "server")
	launcher := graphSubject(t, SubjectRuntimeDependency, "executable", "npx")
	credential := graphSubject(t, SubjectCredentialReference, "env", "TOKEN")
	graph, err := NewGraph(
		[]SubjectID{credential, projection, launcher},
		[]Edge{
			NewEdge(EdgeLaunchesVia, projection, launcher),
			NewEdge(EdgeDependsOn, projection, credential),
		},
	)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}

	const readerCount = 24
	const iterations = 400
	failures := make(chan string, readerCount)
	var readers sync.WaitGroup
	readers.Add(readerCount)
	for range readerCount {
		go func() {
			defer readers.Done()
			for range iterations {
				subjects := graph.Subjects()
				dependencies := graph.DependenciesOf(projection)
				launchers := graph.LauncherDependenciesOf(projection)
				if len(subjects) != 3 || len(graph.outgoing[projection]) != 2 ||
					!slices.Equal(dependencies, []SubjectID{credential}) ||
					!slices.Equal(launchers, []SubjectID{launcher}) {
					failures <- fmt.Sprintf("inconsistent read: subjects=%v dependencies=%v launchers=%v", subjects, dependencies, launchers)
					return
				}
				subjects[0] = SubjectID{}
				dependencies[0] = SubjectID{}
				launchers[0] = SubjectID{}
			}
		}()
	}
	readers.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func TestGraphEdgeHuntEscapedIdentityEdgesRemainDistinct(t *testing.T) {
	projection := graphSubject(t, SubjectProjection, "escaped", "server")
	slash := graphSubject(t, SubjectRuntimeDependency, "escaped", "command/with/slash")
	percent := graphSubject(t, SubjectRuntimeDependency, "escaped", "command%2Fwith%2Fslash")
	unicode := graphSubject(t, SubjectRuntimeDependency, "escaped", "한 글")

	graph, err := NewGraph(
		[]SubjectID{percent, unicode, projection, slash},
		[]Edge{
			NewEdge(EdgeDependsOn, projection, unicode),
			NewEdge(EdgeDependsOn, projection, slash),
			NewEdge(EdgeDependsOn, projection, percent),
		},
	)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}
	want := []SubjectID{slash, percent, unicode}
	sortSubjectIDs(want)
	if got := graph.DependenciesOf(projection); !slices.Equal(got, want) {
		t.Fatalf("escaped dependencies = %v, want %v", got, want)
	}
	for _, subject := range []SubjectID{slash, percent, unicode} {
		parsed, parseError := ParseSubjectID(subject.String())
		if parseError != nil || parsed != subject {
			t.Fatalf("ParseSubjectID(%q) = %v, %v", subject, parsed, parseError)
		}
	}
}

func TestGraphEdgeHuntAbsentAndZeroQueriesAreEmpty(t *testing.T) {
	present := graphSubject(t, SubjectProjection, "query", "present")
	absent := graphSubject(t, SubjectProjection, "query", "absent")
	graph, err := NewGraph([]SubjectID{present}, nil)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}

	for name, candidate := range map[string]Graph{"valid": graph, "zero": {}} {
		if candidate.Contains(absent) || len(candidate.DependenciesOf(absent)) != 0 ||
			len(candidate.LauncherDependenciesOf(absent)) != 0 {
			t.Fatalf("%s graph exposed relations for absent subject", name)
		}
	}
}

func edgeHuntEndpointAllowed(relation EdgeKind, source SubjectKind, target SubjectKind) bool {
	sourceBinding := source == SubjectProjection || source == SubjectBinding
	targetBinding := target == SubjectProjection || target == SubjectBinding
	switch relation {
	case EdgeProvidedBy:
		return source == SubjectContribution && target == SubjectCarrier
	case EdgeBoundTo:
		return (source == SubjectCarrier || source == SubjectContribution || source == SubjectProvisionedArtifact) && targetBinding
	case EdgeLaunchesVia:
		return (sourceBinding || source == SubjectContribution) &&
			(target == SubjectProvisionedArtifact || target == SubjectRuntimeDependency)
	case EdgeDependsOn:
		return (sourceBinding || source == SubjectCarrier || source == SubjectContribution || source == SubjectProvisionedArtifact) &&
			(target == SubjectRuntimeDependency || target == SubjectCredentialReference)
	case EdgeConsumedBy:
		return ((sourceBinding || source == SubjectProvisionedArtifact) &&
			(targetBinding || target == SubjectCarrier || target == SubjectContribution)) ||
			(source == SubjectCarrier && target == SubjectHostRelation)
	default:
		return false
	}
}
