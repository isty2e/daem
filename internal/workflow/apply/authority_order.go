package apply

import (
	"fmt"

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
