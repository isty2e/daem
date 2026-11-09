package record

import (
	"fmt"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

// Native16 did not record the Pi document grammar or array projection. Both
// capture and replay must use its historical contract, not the current catalog.
func native16PiOrderCapability(scope target.Scope) (profile.ExtensionOrderCapability, error) {
	var spelling string
	switch scope {
	case target.ScopeProject:
		spelling = ".pi/settings.json"
	case target.ScopeGlobal:
		spelling = "@pi-agent/settings.json"
	default:
		return profile.ExtensionOrderCapability{}, fmt.Errorf("native16 Pi order scope is invalid")
	}
	classID, err := hostrelation.NewOrderClassID("extension:pi:" + string(scope) + ":packages")
	if err != nil {
		return profile.ExtensionOrderCapability{}, err
	}
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:" + string(scope) + ":settings.packages")
	if err != nil {
		return profile.ExtensionOrderCapability{}, err
	}
	destination, err := output.Parse(spelling)
	if err != nil {
		return profile.ExtensionOrderCapability{}, err
	}
	array, err := output.NewKeyPath("packages")
	if err != nil {
		return profile.ExtensionOrderCapability{}, err
	}
	projection, err := profile.NewNativeSequenceProjection(sequenceID, destination, configuration.JSON, array)
	if err != nil {
		return profile.ExtensionOrderCapability{}, err
	}
	return profile.NewExtensionOrderCapability(profile.ExtensionOrderCapabilityInput{
		Carrier: desiredextension.CarrierPiPackage, Scope: scope, ClassID: classID,
		MemberIdentityContract: "pi-package-load-identity-v1",
		SequenceMembership:     profile.CompleteClassMembership,
		NativeSequences:        []profile.NativeSequenceProjection{projection}, RuntimeMeaning: hostrelation.RuntimePrecedence,
	})
}

func PiOrderDocumentAlias(scope target.Scope) (aggregate.DocumentAddress, error) {
	capability, err := native16PiOrderCapability(scope)
	if err != nil {
		return aggregate.DocumentAddress{}, err
	}
	return aggregate.NewDocumentAddress(target.TargetPi, scope, capability.NativeSequences()[0].Destination())
}
