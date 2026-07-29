package profile

import (
	"fmt"
	"slices"
	"sort"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

const (
	openCodeOrderMemberIdentityV1 = "opencode-plugin-package-v1"
	openCodeOrderObserverV1       = "opencode-plugin-order-observer-v1"
	openCodeOrderMutatorV1        = "opencode-plugin-order-mutator-v1"
	openCodeOrderCodecV1          = "opencode-plugin-list-v1"
	piOrderMemberIdentityV1       = "pi-package-load-identity-v1"
	piOrderObserverV1             = "pi-package-order-observer-v1"
	piOrderMutatorV1              = "pi-package-order-mutator-v1"
	piOrderCodecV1                = "pi-package-list-v1"
)

// ExtensionOrderCapabilityInput carries the complete static contract for one
// carrier/scope-relative order class.
type ExtensionOrderCapabilityInput struct {
	Carrier                desiredextension.Carrier
	Scope                  target.Scope
	ClassID                hostrelation.OrderClassID
	MemberIdentityContract string
	PhysicalSequenceIDs    []hostrelation.PhysicalSequenceID
	ObserverContract       string
	MutatorContract        string
	CodecContractVersion   string
	RuntimeMeaning         hostrelation.RuntimeMeaning
}

// ExtensionOrderCapability admits one complete static extension-order
// contract. Absence is the only representation of unsupported order.
type ExtensionOrderCapability struct {
	carrier                desiredextension.Carrier
	scope                  target.Scope
	classID                hostrelation.OrderClassID
	memberIdentityContract string
	physicalSequenceIDs    []hostrelation.PhysicalSequenceID
	observerContract       string
	mutatorContract        string
	codecContractVersion   string
	runtimeMeaning         hostrelation.RuntimeMeaning
}

// NewExtensionOrderCapability validates one complete order capability.
func NewExtensionOrderCapability(
	input ExtensionOrderCapabilityInput,
) (ExtensionOrderCapability, error) {
	if _, err := desiredextension.ParseCarrier(string(input.Carrier)); err != nil {
		return ExtensionOrderCapability{}, err
	}
	if _, err := target.ParseScope(string(input.Scope)); err != nil {
		return ExtensionOrderCapability{}, err
	}
	if err := input.ClassID.Validate(); err != nil {
		return ExtensionOrderCapability{}, err
	}
	if err := input.RuntimeMeaning.Validate(); err != nil {
		return ExtensionOrderCapability{}, err
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "extension order member identity contract", value: input.MemberIdentityContract},
		{label: "extension order observer contract", value: input.ObserverContract},
		{label: "extension order mutator contract", value: input.MutatorContract},
		{label: "extension order codec contract", value: input.CodecContractVersion},
	} {
		if err := validateProfileToken(field.label, field.value); err != nil {
			return ExtensionOrderCapability{}, err
		}
	}
	if len(input.PhysicalSequenceIDs) == 0 {
		return ExtensionOrderCapability{}, fmt.Errorf(
			"extension order class %q requires at least one physical sequence",
			input.ClassID,
		)
	}

	sequenceIDs := append([]hostrelation.PhysicalSequenceID(nil), input.PhysicalSequenceIDs...)
	seen := make(map[hostrelation.PhysicalSequenceID]struct{}, len(sequenceIDs))
	for _, sequenceID := range sequenceIDs {
		if err := sequenceID.Validate(); err != nil {
			return ExtensionOrderCapability{}, err
		}
		if _, duplicate := seen[sequenceID]; duplicate {
			return ExtensionOrderCapability{}, fmt.Errorf(
				"extension order physical sequence %q appears more than once",
				sequenceID,
			)
		}
		seen[sequenceID] = struct{}{}
	}
	sort.Slice(sequenceIDs, func(left int, right int) bool {
		return sequenceIDs[left] < sequenceIDs[right]
	})

	return ExtensionOrderCapability{
		carrier:                input.Carrier,
		scope:                  input.Scope,
		classID:                input.ClassID,
		memberIdentityContract: input.MemberIdentityContract,
		physicalSequenceIDs:    sequenceIDs,
		observerContract:       input.ObserverContract,
		mutatorContract:        input.MutatorContract,
		codecContractVersion:   input.CodecContractVersion,
		runtimeMeaning:         input.RuntimeMeaning,
	}, nil
}

// Validate rejects a zero or forged extension-order capability.
func (capability ExtensionOrderCapability) Validate() error {
	canonical, err := NewExtensionOrderCapability(ExtensionOrderCapabilityInput{
		Carrier:                capability.carrier,
		Scope:                  capability.scope,
		ClassID:                capability.classID,
		MemberIdentityContract: capability.memberIdentityContract,
		PhysicalSequenceIDs:    capability.physicalSequenceIDs,
		ObserverContract:       capability.observerContract,
		MutatorContract:        capability.mutatorContract,
		CodecContractVersion:   capability.codecContractVersion,
		RuntimeMeaning:         capability.runtimeMeaning,
	})
	if err != nil {
		return err
	}
	if capability.carrier != canonical.carrier ||
		capability.scope != canonical.scope ||
		capability.classID != canonical.classID ||
		capability.memberIdentityContract != canonical.memberIdentityContract ||
		!slices.Equal(capability.physicalSequenceIDs, canonical.physicalSequenceIDs) ||
		capability.observerContract != canonical.observerContract ||
		capability.mutatorContract != canonical.mutatorContract ||
		capability.codecContractVersion != canonical.codecContractVersion ||
		capability.runtimeMeaning != canonical.runtimeMeaning {
		return fmt.Errorf("extension order capability is not canonical")
	}
	return nil
}

func (capability ExtensionOrderCapability) Carrier() desiredextension.Carrier {
	return capability.carrier
}

func (capability ExtensionOrderCapability) Scope() target.Scope { return capability.scope }

func (capability ExtensionOrderCapability) ClassID() hostrelation.OrderClassID {
	return capability.classID
}

func (capability ExtensionOrderCapability) MemberIdentityContract() string {
	return capability.memberIdentityContract
}

func (capability ExtensionOrderCapability) PhysicalSequenceIDs() []hostrelation.PhysicalSequenceID {
	return append([]hostrelation.PhysicalSequenceID(nil), capability.physicalSequenceIDs...)
}

func (capability ExtensionOrderCapability) RuntimeMeaning() hostrelation.RuntimeMeaning {
	return capability.runtimeMeaning
}

type extensionOrderCapabilityRow struct {
	target     target.Target
	capability ExtensionOrderCapability
}

var extensionOrderCapabilityCatalog = []extensionOrderCapabilityRow{
	{
		target: target.TargetOpenCode,
		capability: mustExtensionOrderCapability(ExtensionOrderCapabilityInput{
			Carrier:                desiredextension.CarrierOpenCodePlugin,
			Scope:                  target.ScopeProject,
			ClassID:                mustOrderClassID("extension:opencode:project:plugins"),
			MemberIdentityContract: openCodeOrderMemberIdentityV1,
			PhysicalSequenceIDs: []hostrelation.PhysicalSequenceID{
				mustPhysicalSequenceID("opencode:project:server.plugins"),
				mustPhysicalSequenceID("opencode:project:tui.plugins"),
			},
			ObserverContract:     openCodeOrderObserverV1,
			MutatorContract:      openCodeOrderMutatorV1,
			CodecContractVersion: openCodeOrderCodecV1,
			RuntimeMeaning:       hostrelation.ConfigOrderOnly,
		}),
	},
	{
		target: target.TargetOpenCode,
		capability: mustExtensionOrderCapability(ExtensionOrderCapabilityInput{
			Carrier:                desiredextension.CarrierOpenCodePlugin,
			Scope:                  target.ScopeGlobal,
			ClassID:                mustOrderClassID("extension:opencode:global:plugins"),
			MemberIdentityContract: openCodeOrderMemberIdentityV1,
			PhysicalSequenceIDs: []hostrelation.PhysicalSequenceID{
				mustPhysicalSequenceID("opencode:global:server.plugins"),
				mustPhysicalSequenceID("opencode:global:tui.plugins"),
			},
			ObserverContract:     openCodeOrderObserverV1,
			MutatorContract:      openCodeOrderMutatorV1,
			CodecContractVersion: openCodeOrderCodecV1,
			RuntimeMeaning:       hostrelation.ConfigOrderOnly,
		}),
	},
	{
		target: target.TargetPi,
		capability: mustExtensionOrderCapability(ExtensionOrderCapabilityInput{
			Carrier:                desiredextension.CarrierPiPackage,
			Scope:                  target.ScopeProject,
			ClassID:                mustOrderClassID("extension:pi:project:packages"),
			MemberIdentityContract: piOrderMemberIdentityV1,
			PhysicalSequenceIDs: []hostrelation.PhysicalSequenceID{
				mustPhysicalSequenceID("pi:project:settings.packages"),
			},
			ObserverContract:     piOrderObserverV1,
			MutatorContract:      piOrderMutatorV1,
			CodecContractVersion: piOrderCodecV1,
			RuntimeMeaning:       hostrelation.RuntimePrecedence,
		}),
	},
	{
		target: target.TargetPi,
		capability: mustExtensionOrderCapability(ExtensionOrderCapabilityInput{
			Carrier:                desiredextension.CarrierPiPackage,
			Scope:                  target.ScopeGlobal,
			ClassID:                mustOrderClassID("extension:pi:global:packages"),
			MemberIdentityContract: piOrderMemberIdentityV1,
			PhysicalSequenceIDs: []hostrelation.PhysicalSequenceID{
				mustPhysicalSequenceID("pi:global:settings.packages"),
			},
			ObserverContract:     piOrderObserverV1,
			MutatorContract:      piOrderMutatorV1,
			CodecContractVersion: piOrderCodecV1,
			RuntimeMeaning:       hostrelation.RuntimePrecedence,
		}),
	},
}

// ExtensionOrderCapabilityForClass returns the unique target-owned admission
// for one locked order class. Absence means the class is not admitted.
func ExtensionOrderCapabilityForClass(
	classID hostrelation.OrderClassID,
) (target.Target, ExtensionOrderCapability, bool) {
	var selectedTarget target.Target
	var selected ExtensionOrderCapability
	count := 0
	for _, row := range extensionOrderCapabilityCatalog {
		if row.capability.ClassID() != classID {
			continue
		}
		selectedTarget = row.target
		selected = row.capability
		count++
	}
	return selectedTarget, selected, count == 1
}

func mustExtensionOrderCapability(
	input ExtensionOrderCapabilityInput,
) ExtensionOrderCapability {
	capability, err := NewExtensionOrderCapability(input)
	if err != nil {
		panic(err)
	}
	return capability
}

func mustOrderClassID(value string) hostrelation.OrderClassID {
	id, err := hostrelation.NewOrderClassID(value)
	if err != nil {
		panic(err)
	}
	return id
}

func mustPhysicalSequenceID(value string) hostrelation.PhysicalSequenceID {
	id, err := hostrelation.NewPhysicalSequenceID(value)
	if err != nil {
		panic(err)
	}
	return id
}

func profileExtensionOrderCapabilities(
	selectedTarget target.Target,
) []ExtensionOrderCapability {
	result := make([]ExtensionOrderCapability, 0)
	for _, row := range extensionOrderCapabilityCatalog {
		if row.target == selectedTarget {
			result = append(result, row.capability)
		}
	}
	return result
}

func validateExtensionOrderCapabilityCatalog(
	catalog []extensionOrderCapabilityRow,
) error {
	type admissionKey struct {
		target  target.Target
		carrier desiredextension.Carrier
		scope   target.Scope
	}

	admissions := make(map[admissionKey]struct{}, len(catalog))
	classIDs := make(map[hostrelation.OrderClassID]admissionKey, len(catalog))
	sequenceIDs := make(map[hostrelation.PhysicalSequenceID]hostrelation.OrderClassID)
	for index, row := range catalog {
		if _, err := target.ParseTarget(string(row.target)); err != nil {
			return fmt.Errorf("extension order capability[%d] target: %w", index, err)
		}
		if err := row.capability.Validate(); err != nil {
			return fmt.Errorf("extension order capability[%d]: %w", index, err)
		}
		if !row.capability.Carrier().AdmitsTargetScope(row.target, row.capability.Scope()) {
			return fmt.Errorf(
				"extension order capability[%d] carrier %q does not admit %q/%q",
				index,
				row.capability.Carrier(),
				row.target,
				row.capability.Scope(),
			)
		}
		key := admissionKey{row.target, row.capability.Carrier(), row.capability.Scope()}
		if _, duplicate := admissions[key]; duplicate {
			return fmt.Errorf(
				"duplicate extension order capability for %q/%q/%q",
				key.target,
				key.carrier,
				key.scope,
			)
		}
		admissions[key] = struct{}{}
		if previous, duplicate := classIDs[row.capability.ClassID()]; duplicate {
			return fmt.Errorf(
				"extension order class %q is shared by %q/%q/%q and %q/%q/%q",
				row.capability.ClassID(),
				previous.target,
				previous.carrier,
				previous.scope,
				key.target,
				key.carrier,
				key.scope,
			)
		}
		classIDs[row.capability.ClassID()] = key
		for _, sequenceID := range row.capability.PhysicalSequenceIDs() {
			if previous, duplicate := sequenceIDs[sequenceID]; duplicate {
				return fmt.Errorf(
					"physical sequence %q is shared by order classes %q and %q",
					sequenceID,
					previous,
					row.capability.ClassID(),
				)
			}
			sequenceIDs[sequenceID] = row.capability.ClassID()
		}
	}
	return nil
}
