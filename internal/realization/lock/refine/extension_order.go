package refine

import (
	"fmt"
	"sort"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ExtensionOrderIdentityResolver derives the canonical identity used by one
// host order class. Host codecs own source interpretation; refinement accepts
// only their already-normalized result.
type ExtensionOrderIdentityResolver = lock.ExtensionOrderIdentityResolver

type extensionOrderClass struct {
	capability profile.ExtensionOrderCapability
	candidates []extensionOrderCandidate
}

type extensionOrderCandidate struct {
	index     int
	extension desiredextension.Extension
}

// ExtensionOrderConstraints refines authored extension order into canonical
// class-relative constraints. Class rows are sorted; member order is preserved.
func ExtensionOrderConstraints(
	extensions []desiredextension.Extension,
	resolveIdentity ExtensionOrderIdentityResolver,
) ([]hostrelation.RelationOrderConstraint, error) {
	classes := make(map[hostrelation.OrderClassID]extensionOrderClass)
	for index, extension := range extensions {
		if err := extension.Validate(); err != nil {
			return nil, fmt.Errorf("extension[%d]: %w", index, err)
		}
		capability, admitted := profile.Profile(extension.Target()).ExtensionOrder(
			extension.Carrier(),
			extension.Scope(),
		)
		if !admitted {
			continue
		}
		class := classes[capability.ClassID()]
		if class.candidates == nil {
			class.capability = capability
		}
		class.candidates = append(class.candidates, extensionOrderCandidate{
			index:     index,
			extension: extension,
		})
		classes[capability.ClassID()] = class
	}

	classIDs := make([]hostrelation.OrderClassID, 0, len(classes))
	for classID := range classes {
		classIDs = append(classIDs, classID)
	}
	sort.Slice(classIDs, func(left int, right int) bool {
		return classIDs[left] < classIDs[right]
	})

	constraints := make([]hostrelation.RelationOrderConstraint, 0, len(classIDs))
	for _, classID := range classIDs {
		class := classes[classID]
		if len(class.candidates) < 2 {
			continue
		}
		if resolveIdentity == nil {
			return nil, fmt.Errorf(
				"extension[%d] order class %q requires a host-load identity resolver",
				class.candidates[0].index,
				classID,
			)
		}
		members := make([]hostrelation.RelationOrderMember, 0, len(class.candidates))
		for _, candidate := range class.candidates {
			hostLoadIdentity, err := resolveIdentity(candidate.extension.CarrierKey())
			if err != nil {
				return nil, fmt.Errorf(
					"extension[%d] order class %q host-load identity: %w",
					candidate.index,
					classID,
					err,
				)
			}
			subject, err := extensiontopology.Relation(candidate.extension)
			if err != nil {
				return nil, fmt.Errorf(
					"extension[%d] order subject: %w",
					candidate.index,
					err,
				)
			}
			member, err := hostrelation.NewRelationOrderMember(subject, hostLoadIdentity)
			if err != nil {
				return nil, fmt.Errorf(
					"extension[%d] order member: %w",
					candidate.index,
					err,
				)
			}
			members = append(members, member)
		}
		constraint, err := hostrelation.NewRelationOrderConstraint(
			classID,
			class.capability.MemberIdentityContract(),
			class.capability.RuntimeMeaning(),
			members,
		)
		if err != nil {
			return nil, fmt.Errorf("extension order class %q: %w", classID, err)
		}
		constraints = append(constraints, constraint)
	}
	return constraints, nil
}
