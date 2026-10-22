package authoring

import (
	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func extensionSourceWarnings(value desiredextension.Extension) ([]Warning, error) {
	if value.Carrier() != desiredextension.CarrierPiPackage {
		return nil, nil
	}
	difference, err := extensiontopology.PiPackageSourceInterpretationDifference(value.CarrierKey())
	if err != nil {
		return nil, err
	}
	if difference == extensiontopology.PiSourceDifferenceNone {
		return nil, nil
	}

	return []Warning{newExtensionSourceWarning(difference, value.ID())}, nil
}

func newExtensionSourceWarning(difference extensiontopology.PiSourceInterpretationDifference, id entity.ID) Warning {
	var detail string
	switch difference {
	case extensiontopology.PiSourceDifferenceGitHubShorthand:
		detail = "uses bare github: shorthand, which has a documented local-path interpretation in Pi; for Git intent use git:host/owner/repo or a lowercase https:// URL; the authored source is not converted"
	case extensiontopology.PiSourceDifferenceGitPlusURL:
		detail = "uses a bare git+ transport URL, which has a documented local-path interpretation in Pi; for Git intent use git:host/owner/repo or a lowercase https:// URL; the authored source is not converted"
	case extensiontopology.PiSourceDifferenceBareSCP:
		detail = "uses a bare SCP-style Git address, which has a documented local-path interpretation in Pi; for SSH Git intent use git:git@github.com:owner/repo or ssh://git@github.com/owner/repo; the authored source is not converted"
	}
	return Warning{Resource: id, Detail: detail}
}
