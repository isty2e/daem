// Package extension lowers canonical desired extension relations into structural topology.
package extension

import (
	"fmt"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/topology"
)

// Model is one immutable desired extension topology.
type Model struct {
	graph topology.Graph
}

type relationAddress struct {
	carrier string
	target  string
	scope   string
	key     string
}

// Relation lowers one canonical desired extension into its structural host relation identity.
func Relation(value desiredextension.Extension) (topology.SubjectID, error) {
	if err := value.Validate(); err != nil {
		return topology.SubjectID{}, fmt.Errorf("extension relation: %w", err)
	}
	namespace, err := namespaceFor(value.Carrier())
	if err != nil {
		return topology.SubjectID{}, err
	}
	return topology.NewSubjectID(topology.SubjectHostRelation, namespace, value.ID().Name())
}

// Lower lowers canonical desired extensions into one validated structural
// graph. Equal carrier identities are shared; relation identities are never
// deduplicated.
func Lower(values []desiredextension.Extension) (Model, error) {
	subjects := make([]topology.SubjectID, 0, len(values)*2)
	edges := make([]topology.Edge, 0, len(values))
	carriers := make(map[topology.SubjectID]struct{}, len(values))
	addresses := make(map[relationAddress]desiredextension.CarrierKey, len(values))
	for index, value := range values {
		relation, err := Relation(value)
		if err != nil {
			return Model{}, fmt.Errorf("extension[%d]: %w", index, err)
		}
		carrier, err := carrierFor(value)
		if err != nil {
			return Model{}, fmt.Errorf("extension[%d]: %w", index, err)
		}
		relationKey, err := HostVisibleRelationKey(carrier.Key())
		if err != nil {
			return Model{}, fmt.Errorf("extension[%d]: relation address: %w", index, err)
		}
		address := relationAddress{
			carrier: string(carrier.Family()),
			target:  string(carrier.Key().Target()),
			scope:   string(carrier.Key().Scope()),
			key:     relationKey,
		}
		if previous, exists := addresses[address]; exists && previous != carrier.Key() {
			left := previous.Source().Ref()
			right := carrier.Source().Ref()
			if left > right {
				left, right = right, left
			}
			return Model{}, fmt.Errorf(
				"extension relation address %q maps structural sources %q and %q to the same host relation",
				relationKey,
				left,
				right,
			)
		}
		addresses[address] = carrier.Key()
		subjects = append(subjects, relation)
		if _, exists := carriers[carrier.SubjectID()]; !exists {
			carriers[carrier.SubjectID()] = struct{}{}
			subjects = append(subjects, carrier.SubjectID())
		}
		edges = append(edges, topology.NewEdge(topology.EdgeConsumedBy, carrier.SubjectID(), relation))
	}
	graph, err := topology.NewGraph(subjects, edges)
	if err != nil {
		return Model{}, fmt.Errorf("lower extension topology: %w", err)
	}
	return Model{graph: graph}, nil
}

// Graph returns the immutable structural graph.
func (model Model) Graph() topology.Graph { return model.graph }

// IsCarrierRelation reports whether subject is a structural relation for carrier.
func IsCarrierRelation(carrier desiredextension.Carrier, subject topology.SubjectID) bool {
	namespace, err := namespaceFor(carrier)
	if err != nil || subject.Validate() != nil {
		return false
	}
	return subject.Kind() == topology.SubjectHostRelation && subject.Namespace() == namespace
}

func namespaceFor(carrier desiredextension.Carrier) (string, error) {
	namespace, ok := CarrierNamespace(carrier)
	if !ok {
		return "", fmt.Errorf("unsupported extension carrier %q", carrier)
	}
	return namespace, nil
}
