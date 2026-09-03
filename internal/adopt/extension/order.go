package extension

import (
	"bytes"
	"fmt"
	"sort"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type relationEdge struct {
	before desiredextension.CarrierKey
	after  desiredextension.CarrierKey
}

func planOrder(
	candidates map[desiredextension.CarrierKey]candidateFact,
	existing []desiredextension.Extension,
	existingLoadIdentities map[desiredextension.CarrierKey]hostrelation.HostLoadIdentity,
	assignedIDs map[desiredextension.CarrierKey]string,
	sequenceFacts []sequenceFact,
) (
	[]desiredextension.Extension,
	[]desiredextension.Extension,
	[]desiredextension.CarrierKey,
	[]relationobserve.ObservedRelationSequence,
	[]hostrelation.RelationOrderConstraint,
	error,
) {
	extensionsByKey := make(map[desiredextension.CarrierKey]desiredextension.Extension)
	loadIdentityByKey := make(map[desiredextension.CarrierKey]hostrelation.HostLoadIdentity)
	for _, value := range existing {
		extensionsByKey[value.CarrierKey()] = value
	}
	for key, identity := range existingLoadIdentities {
		loadIdentityByKey[key] = identity
	}
	for key, candidate := range candidates {
		value, err := desiredextension.New(desiredextension.Spec{
			Name:    assignedIDs[key],
			Carrier: key.Carrier(),
			Target:  key.Target(),
			Scope:   key.Scope(),
			Source:  key.Source(),
		})
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		extensionsByKey[key] = value
		loadIdentityByKey[key] = candidate.loadIdentity
	}

	subjectToKey := make(map[topology.SubjectID]desiredextension.CarrierKey)
	for key, value := range extensionsByKey {
		subject, err := extensiontopology.Relation(value)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		subjectToKey[subject] = key
	}

	sequences := make([]relationobserve.ObservedRelationSequence, 0, len(sequenceFacts))
	edges := make(map[relationEdge]struct{})
	touchedClasses := make(map[hostrelation.OrderClassID]profile.ExtensionOrderCapability)
	for _, fact := range sequenceFacts {
		rows := make([]relationobserve.ObservedRelationRow, 0, len(fact.rows))
		for _, factRow := range fact.rows {
			var (
				row relationobserve.ObservedRelationRow
				err error
			)
			if factRow.correlated {
				value := extensionsByKey[factRow.key]
				subject, relationErr := extensiontopology.Relation(value)
				if relationErr != nil {
					return nil, nil, nil, nil, nil, relationErr
				}
				row, err = relationobserve.NewCorrelatedObservedRelationRow(
					factRow.loadIdentity,
					subject,
				)
			} else {
				row, err = relationobserve.NewObservedRelationRow(factRow.loadIdentity)
			}
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			rows = append(rows, row)
		}
		sequence, err := relationobserve.NewObservedRelationSequence(
			fact.classID,
			fact.sequence,
			fact.authority,
			fact.revision,
			rows,
		)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		sequences = append(sequences, sequence)

		var previous desiredextension.CarrierKey
		hasPrevious := false
		for _, row := range sequence.OrderedRows() {
			subject, correlated := row.CorrelatedSubject()
			if !correlated {
				continue
			}
			key := subjectToKey[subject]
			if hasPrevious && previous != key {
				edges[relationEdge{before: previous, after: key}] = struct{}{}
			}
			previous = key
			hasPrevious = true
		}
	}

	for index := 1; index < len(existing); index++ {
		edges[relationEdge{
			before: existing[index-1].CarrierKey(),
			after:  existing[index].CarrierKey(),
		}] = struct{}{}
	}

	ordered, err := stableTopologicalOrder(extensionsByKey, edges)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	for _, fact := range sequenceFacts {
		capability, admitted := compiledCapabilityForClass(fact.classID)
		if !admitted {
			return nil, nil, nil, nil, nil, fmt.Errorf(
				"extension order class %q is not admitted by any target profile",
				fact.classID,
			)
		}
		touchedClasses[fact.classID] = capability
	}
	for key := range extensionsByKey {
		capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(
			key.Target(),
			key.Scope(),
			key.Carrier(),
		)
		if !admitted {
			continue
		}
		if _, touched := touchedClasses[capability.ClassID()]; !touched {
			continue
		}
		if _, known := loadIdentityByKey[key]; !known {
			return nil, nil, nil, nil, nil, fmt.Errorf(
				"existing extension %s lacks host load identity",
				canonicalRelationText(key),
			)
		}
	}

	constraints := make([]hostrelation.RelationOrderConstraint, 0, len(touchedClasses))
	classIDs := make([]hostrelation.OrderClassID, 0, len(touchedClasses))
	for classID := range touchedClasses {
		classIDs = append(classIDs, classID)
	}
	sort.Slice(classIDs, func(left int, right int) bool {
		return classIDs[left] < classIDs[right]
	})
	for _, classID := range classIDs {
		capability := touchedClasses[classID]
		members := make([]hostrelation.RelationOrderMember, 0)
		for _, key := range ordered {
			selected, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(
				key.Target(),
				key.Scope(),
				key.Carrier(),
			)
			if !admitted || selected.ClassID() != classID {
				continue
			}
			subject, err := extensiontopology.Relation(extensionsByKey[key])
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			member, err := hostrelation.NewRelationOrderMember(
				subject,
				loadIdentityByKey[key],
			)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			members = append(members, member)
		}
		if len(members) == 0 {
			continue
		}
		constraint, err := hostrelation.NewRelationOrderConstraint(
			classID,
			capability.MemberIdentityContract(),
			capability.RuntimeMeaning(),
			members,
		)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		constraints = append(constraints, constraint)
	}

	imported := make([]desiredextension.Extension, 0, len(candidates))
	orderedExtensions := make([]desiredextension.Extension, 0, len(ordered))
	for _, key := range ordered {
		orderedExtensions = append(orderedExtensions, extensionsByKey[key])
		if _, observed := candidates[key]; observed {
			imported = append(imported, extensionsByKey[key])
		}
	}
	return imported, orderedExtensions, ordered, sequences, constraints, nil
}

func stableTopologicalOrder(
	vertices map[desiredextension.CarrierKey]desiredextension.Extension,
	edges map[relationEdge]struct{},
) ([]desiredextension.CarrierKey, error) {
	inDegree := make(map[desiredextension.CarrierKey]int, len(vertices))
	outgoing := make(map[desiredextension.CarrierKey][]desiredextension.CarrierKey)
	for key := range vertices {
		inDegree[key] = 0
	}
	for edge := range edges {
		if edge.before == edge.after {
			continue
		}
		if _, exists := vertices[edge.before]; !exists {
			continue
		}
		if _, exists := vertices[edge.after]; !exists {
			continue
		}
		outgoing[edge.before] = append(outgoing[edge.before], edge.after)
		inDegree[edge.after]++
	}
	less := func(left desiredextension.CarrierKey, right desiredextension.CarrierKey) bool {
		return bytes.Compare(canonicalRelationBytes(left), canonicalRelationBytes(right)) < 0
	}
	ready := make([]desiredextension.CarrierKey, 0)
	for key, degree := range inDegree {
		if degree == 0 {
			ready = append(ready, key)
		}
	}
	sort.Slice(ready, func(left int, right int) bool {
		return less(ready[left], ready[right])
	})

	ordered := make([]desiredextension.CarrierKey, 0, len(vertices))
	for len(ready) != 0 {
		key := ready[0]
		ready = ready[1:]
		ordered = append(ordered, key)
		next := outgoing[key]
		sort.Slice(next, func(left int, right int) bool {
			return less(next[left], next[right])
		})
		for _, successor := range next {
			inDegree[successor]--
			if inDegree[successor] == 0 {
				ready = append(ready, successor)
			}
		}
		sort.Slice(ready, func(left int, right int) bool {
			return less(ready[left], ready[right])
		})
	}
	if len(ordered) != len(vertices) {
		return nil, fmt.Errorf(
			"selected extension order evidence contradicts existing or native order",
		)
	}
	return ordered, nil
}

func compiledCapabilityForClass(
	classID hostrelation.OrderClassID,
) (profile.ExtensionOrderCapability, bool) {
	_, capability, ok := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(classID)
	return capability, ok
}
