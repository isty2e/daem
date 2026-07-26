package topology

// EdgeKind identifies one closed structural relation kind.
type EdgeKind string

const (
	EdgeProvidedBy  EdgeKind = "provided_by"
	EdgeBoundTo     EdgeKind = "bound_to"
	EdgeLaunchesVia EdgeKind = "launches_via"
	EdgeDependsOn   EdgeKind = "depends_on"
	EdgeConsumedBy  EdgeKind = "consumed_by"
)

// Edge records one typed structural relation between canonical subjects.
type Edge struct {
	kind   EdgeKind
	source SubjectID
	target SubjectID
}

// NewEdge constructs a subject-to-subject structural edge. NewGraph validates
// the relation kind and endpoint roles against its complete subject set.
func NewEdge(kind EdgeKind, source SubjectID, target SubjectID) Edge {
	return Edge{kind: kind, source: source, target: target}
}

func compareEdge(left Edge, right Edge) int {
	if left.kind < right.kind {
		return -1
	}
	if left.kind > right.kind {
		return 1
	}
	if order := CompareSubjectID(left.source, right.source); order != 0 {
		return order
	}
	return CompareSubjectID(left.target, right.target)
}
