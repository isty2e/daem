package opencodeplugin

import (
	"fmt"
	"path/filepath"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// ScopedRelation pairs one exact OpenCode relation expectation with its target
// scope. Config-relative source identity is derived from the selected document
// directory, not from process working directory.
type ScopedRelation struct {
	key   observerelation.CorrelationKey
	scope target.Scope
}

// NewScopedRelation validates one OpenCode order correlation request.
func NewScopedRelation(
	key observerelation.CorrelationKey,
	scope target.Scope,
) (ScopedRelation, error) {
	if err := key.Validate(); err != nil {
		return ScopedRelation{}, fmt.Errorf("OpenCode relation correlation key: %w", err)
	}
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return ScopedRelation{}, fmt.Errorf("OpenCode relation scope: %w", err)
	}
	switch parsedScope {
	case target.ScopeProject, target.ScopeGlobal:
	default:
		return ScopedRelation{}, fmt.Errorf(
			"OpenCode relation scope %q is not observable",
			parsedScope,
		)
	}
	return ScopedRelation{key: key, scope: parsedScope}, nil
}

type orderSelection struct {
	scope         target.Scope
	directory     string
	constraint    hostrelation.RelationOrderConstraint
	capability    profile.ExtensionOrderCapability
	relations     []ScopedRelation
	exactSubjects map[string]topology.SubjectID
}

func newOrderSelection(
	scope target.Scope,
	directory string,
	constraint hostrelation.RelationOrderConstraint,
	relations []ScopedRelation,
) (orderSelection, error) {
	if err := constraint.Validate(); err != nil {
		return orderSelection{}, fmt.Errorf("OpenCode plugin order constraint: %w", err)
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return orderSelection{}, fmt.Errorf("OpenCode plugin order directory is not canonical")
	}
	capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(
		target.TargetOpenCode,
		scope,
		desiredextension.CarrierOpenCodePlugin,
	)
	if !admitted {
		return orderSelection{}, fmt.Errorf("OpenCode %s plugin order is not admitted", scope)
	}
	if constraint.ClassID() != capability.ClassID() ||
		constraint.MemberIdentityContract() != capability.MemberIdentityContract() ||
		constraint.RuntimeMeaning() != capability.RuntimeMeaning() {
		return orderSelection{}, fmt.Errorf(
			"OpenCode plugin order constraint does not match the %s profile capability",
			scope,
		)
	}
	if len(capability.PhysicalSequenceIDs()) != 4 {
		return orderSelection{}, fmt.Errorf(
			"OpenCode %s plugin order requires four candidate physical sequences",
			scope,
		)
	}

	bySubject := make(map[topology.SubjectID]ScopedRelation, len(relations))
	for index, relation := range relations {
		if relation.scope != scope {
			return orderSelection{}, fmt.Errorf(
				"OpenCode plugin order relation[%d] scope %q does not match %q",
				index,
				relation.scope,
				scope,
			)
		}
		subject := relation.key.Subject()
		if _, duplicate := bySubject[subject]; duplicate {
			return orderSelection{}, fmt.Errorf(
				"OpenCode plugin order relation subject %q appears more than once",
				subject,
			)
		}
		bySubject[subject] = relation
	}

	identitySourcePath := filepath.Join(directory, "opencode.json")
	ordered := make([]ScopedRelation, 0, len(constraint.Members()))
	exactSubjects := make(map[string]topology.SubjectID, len(constraint.Members()))
	for index, member := range constraint.Members() {
		relation, present := bySubject[member.Subject()]
		if !present {
			return orderSelection{}, fmt.Errorf(
				"OpenCode plugin order member[%d] subject %q has no exact relation",
				index,
				member.Subject(),
			)
		}
		source := string(relation.key.ExpectedRelation().SubjectKey())
		if _, err := desiredextension.NewSourceRef(
			desiredextension.SourceKindHostSource,
			source,
		); err != nil {
			return orderSelection{}, fmt.Errorf(
				"OpenCode plugin order member[%d] source: %w",
				index,
				err,
			)
		}
		if previous, duplicate := exactSubjects[source]; duplicate {
			return orderSelection{}, fmt.Errorf(
				"OpenCode plugin order subjects %q and %q map to exact source %q",
				previous,
				relation.key.Subject(),
				source,
			)
		}
		identity, err := opencodeconfig.HostLoadIdentity(source, identitySourcePath)
		if err != nil {
			return orderSelection{}, fmt.Errorf(
				"derive OpenCode plugin order member[%d] load identity: %w",
				index,
				err,
			)
		}
		if hostrelation.HostLoadIdentity(identity) != member.HostLoadIdentity() {
			return orderSelection{}, fmt.Errorf(
				"OpenCode plugin order member[%d] host load identity %q does not match relation identity %q",
				index,
				member.HostLoadIdentity(),
				identity,
			)
		}
		ordered = append(ordered, relation)
		exactSubjects[source] = relation.key.Subject()
		delete(bySubject, member.Subject())
	}
	if len(bySubject) != 0 {
		return orderSelection{}, fmt.Errorf(
			"OpenCode plugin order contains relations outside its constraint",
		)
	}
	return orderSelection{
		scope:         scope,
		directory:     directory,
		constraint:    constraint,
		capability:    capability,
		relations:     ordered,
		exactSubjects: exactSubjects,
	}, nil
}
