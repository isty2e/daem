package document

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// AgentConfigManagement is one complete retained declaration/target/scope/document
// identity. It grants neither host access nor Registry mutation authority.
type AgentConfigManagement struct {
	declaration entity.ID
	target      target.Target
	scope       target.Scope
	destination output.Destination
	ownership   agentconfig.Ownership
	subjects    []topology.SubjectID
}

func (management AgentConfigManagement) Declaration() entity.ID { return management.declaration }
func (management AgentConfigManagement) Target() target.Target  { return management.target }
func (management AgentConfigManagement) Scope() target.Scope    { return management.scope }
func (management AgentConfigManagement) Destination() output.Destination {
	return management.destination
}

func (management AgentConfigManagement) Ownership() agentconfig.Ownership {
	return management.ownership
}

func (management AgentConfigManagement) Subjects() []topology.SubjectID {
	return slices.Clone(management.subjects)
}

func (management AgentConfigManagement) Equal(other AgentConfigManagement) bool {
	return management.declaration.Kind() == entity.KindAgentConfig && len(management.subjects) != 0 &&
		management.declaration == other.declaration && management.target == other.target &&
		management.scope == other.scope && management.destination == other.destination &&
		management.ownership == other.ownership && slices.Equal(management.subjects, other.subjects)
}

// AgentConfigManagements selects retained identities without a declaration,
// source file, current host observation or reconstructed management state.
func AgentConfigManagements(ctx context.Context, snapshot durable.Snapshot, declaration entity.ID) ([]AgentConfigManagement, error) {
	if ctx == nil {
		return nil, fmt.Errorf("retained native management context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := declaration.Validate(); err != nil || declaration.Kind() != entity.KindAgentConfig {
		return nil, fmt.Errorf("retained native management requires a host-config declaration identity")
	}
	type identity struct {
		target      target.Target
		scope       target.Scope
		destination output.Destination
	}
	groups := make(map[identity]AgentConfigManagement)
	add := func(selected target.Target, scope target.Scope, destination output.Destination, ownership agentconfig.Ownership, subject topology.SubjectID) error {
		key := identity{target: selected, scope: scope, destination: destination}
		management, present := groups[key]
		if present && management.ownership != ownership {
			return fmt.Errorf("retained native identity mixes key and document ownership")
		}
		if !present {
			management = AgentConfigManagement{declaration: declaration, target: selected, scope: scope, destination: destination, ownership: ownership}
		}
		management.subjects = append(management.subjects, subject)
		groups[key] = management
		return nil
	}
	for _, row := range snapshot.ManagedPaths() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, present := topologyprojection.EntityID(row.Subject())
		if !present || id != declaration {
			continue
		}
		consumers := row.ConsumerTargets()
		if len(consumers) != 1 {
			return nil, fmt.Errorf("retained native whole-file identity has ambiguous consumer targets")
		}
		selected := consumers[0]
		expected, err := aggregate.AgentConfigDocumentSubject(id, selected, row.Scope())
		if err != nil || expected != row.Subject() {
			return nil, fmt.Errorf("retained native whole-file subject contradicts its placement")
		}
		if err := add(selected, row.Scope(), row.Destination(), agentconfig.OwnershipDocument, row.Subject()); err != nil {
			return nil, err
		}
	}
	for _, row := range snapshot.ManagedAggregates() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, _, present := topologyprojection.AgentConfigKeyIdentity(row.Subject())
		if !present || id != declaration {
			continue
		}
		contribution := row.Contribution()
		if _, native := aggregate.AgentConfigSyntaxForCodec(contribution.CodecContractID()); !native {
			return nil, fmt.Errorf("retained native key subject has a different codec family")
		}
		if err := add(contribution.Target(), contribution.Scope(), contribution.AggregateRoot(), agentconfig.OwnershipKeys, row.Subject()); err != nil {
			return nil, err
		}
	}
	result := make([]AgentConfigManagement, 0, len(groups))
	for _, management := range groups {
		slices.SortFunc(management.subjects, topology.CompareSubjectID)
		result = append(result, management)
	}
	slices.SortFunc(result, func(left, right AgentConfigManagement) int {
		return cmp.Or(cmp.Compare(left.target, right.target), cmp.Compare(left.scope, right.scope), cmp.Compare(left.destination.String(), right.destination.String()))
	})
	return result, nil
}

// RemoveFrom removes exactly the complete selected identity.
// A stale selection refuses rather than releasing a newly changed member set.
func (management AgentConfigManagement) RemoveFrom(ctx context.Context, snapshot durable.Snapshot) (durable.Snapshot, error) {
	identities, err := AgentConfigManagements(ctx, snapshot, management.declaration)
	if err != nil {
		return durable.Snapshot{}, err
	}
	if !slices.ContainsFunc(identities, management.Equal) {
		return durable.Snapshot{}, fmt.Errorf("retained native management identity changed or is absent")
	}
	selected := make(map[topology.SubjectID]struct{}, len(management.subjects))
	for _, subject := range management.subjects {
		selected[subject] = struct{}{}
	}
	beforePaths, beforeAggregates := snapshot.ManagedPaths(), snapshot.ManagedAggregates()
	paths := make([]durable.ManagedPathState, 0, len(beforePaths))
	for _, row := range beforePaths {
		if _, remove := selected[row.Subject()]; !remove {
			paths = append(paths, row)
		}
	}
	aggregates := make([]durable.ManagedAggregateState, 0, len(beforeAggregates))
	for _, row := range beforeAggregates {
		if _, remove := selected[row.Subject()]; !remove {
			aggregates = append(aggregates, row)
		}
	}
	after, err := snapshot.WithManagedProjections(paths, aggregates)
	if err != nil {
		return durable.Snapshot{}, err
	}
	if after.Equal(snapshot) {
		return durable.Snapshot{}, fmt.Errorf("native release requires distinct complete snapshots")
	}
	if err := ctx.Err(); err != nil {
		return durable.Snapshot{}, err
	}
	return after, nil
}
