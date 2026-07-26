package apply

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type physicalOccupancyKind string

const (
	physicalOccupancyWholePath physicalOccupancyKind = "whole-path"
	physicalOccupancyAggregate physicalOccupancyKind = "aggregate"
)

type physicalOccupancy struct {
	scope       target.Scope
	destination output.Destination
	kind        physicalOccupancyKind
	target      target.Target
}

type physicalOccupancyIndex map[string]physicalOccupancy

func (occupancy physicalOccupancy) equal(other physicalOccupancy) bool {
	if occupancy.scope != other.scope ||
		occupancy.destination != other.destination ||
		occupancy.kind != other.kind {
		return false
	}
	return occupancy.kind != physicalOccupancyAggregate ||
		occupancy.target == other.target
}

func (occupancy physicalOccupancy) String() string {
	if occupancy.kind == physicalOccupancyAggregate {
		return fmt.Sprintf(
			"%s:%s:%s (%s)",
			occupancy.target,
			occupancy.scope,
			occupancy.destination,
			occupancy.kind,
		)
	}
	return fmt.Sprintf("%s:%s (%s)", occupancy.scope, occupancy.destination, occupancy.kind)
}

func (index physicalOccupancyIndex) register(path string, occupancy physicalOccupancy) error {
	key, err := mutation.CanonicalDirectoryEntryKey(path)
	if err != nil {
		return err
	}
	if existing, present := index[key]; present && !existing.equal(occupancy) {
		return fmt.Errorf(
			"physical destination %q aliases incompatible logical occupancies %s and %s",
			path,
			existing,
			occupancy,
		)
	}
	index[key] = occupancy
	return nil
}

func revisionRequestKey(path string, effect mutation.PathEffect) string {
	return strconv.Itoa(int(effect)) + ":" + path
}

func sortedRevisionRequests(requests map[string]mutation.RevisionRequest) []mutation.RevisionRequest {
	keys := make([]string, 0, len(requests))
	for key := range requests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]mutation.RevisionRequest, 0, len(keys))
	for _, key := range keys {
		result = append(result, requests[key])
	}
	return result
}

func applyAuthorityFactKey(fact applyAuthorityFact) string {
	return fact.Kind + "\x00" + fact.Path + "\x00" +
		strconv.Itoa(int(fact.Access)) + "\x00" +
		strconv.Itoa(int(fact.Effect)) + "\x00" +
		fact.Target + "\x00" + fact.Scope + "\x00" + fact.Family + "\x00" +
		strconv.Itoa(int(fact.Containment))
}
