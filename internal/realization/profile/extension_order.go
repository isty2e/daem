package profile

import (
	"cmp"
	"fmt"
	"slices"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

const (
	openCodeOrderMemberIdentityV1 = "opencode-plugin-package-v1"
	piOrderMemberIdentityV1       = "pi-package-load-identity-v1"
)

// SequenceMembershipContract defines whether each physical sequence must
// contain every logical class member or may load an independent subset.
type SequenceMembershipContract string

const (
	CompleteClassMembership SequenceMembershipContract = "complete_class_membership"
	LoadedClassSubset       SequenceMembershipContract = "loaded_class_subset"
)

func (contract SequenceMembershipContract) validate() error {
	switch contract {
	case CompleteClassMembership, LoadedClassSubset:
		return nil
	default:
		return fmt.Errorf("unsupported extension order sequence membership contract %q", contract)
	}
}

// ExtensionOrderCapabilityInput carries the complete static contract for one
// carrier/scope-relative order class.
type ExtensionOrderCapabilityInput struct {
	Carrier                desiredextension.Carrier
	Scope                  target.Scope
	ClassID                hostrelation.OrderClassID
	MemberIdentityContract string
	SequenceMembership     SequenceMembershipContract
	NativeSequences        []NativeSequenceProjection
	RuntimeMeaning         hostrelation.RuntimeMeaning
}

// ExtensionOrderCapability admits one complete static extension-order
// contract. Absence is the only representation of unsupported order.
type ExtensionOrderCapability struct {
	carrier                desiredextension.Carrier
	scope                  target.Scope
	classID                hostrelation.OrderClassID
	memberIdentityContract string
	sequenceMembership     SequenceMembershipContract
	nativeSequences        []NativeSequenceProjection
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
	if err := input.SequenceMembership.validate(); err != nil {
		return ExtensionOrderCapability{}, err
	}
	if err := validateProfileToken(
		"extension order member identity contract",
		input.MemberIdentityContract,
	); err != nil {
		return ExtensionOrderCapability{}, err
	}
	if len(input.NativeSequences) == 0 {
		return ExtensionOrderCapability{}, fmt.Errorf(
			"extension order class %q requires at least one physical sequence",
			input.ClassID,
		)
	}

	sequences := slices.Clone(input.NativeSequences)
	seen := make(map[hostrelation.PhysicalSequenceID]struct{}, len(sequences))
	type arraySelection struct {
		destination output.Destination
		array       output.KeyPath
	}
	arrays := make(map[arraySelection]struct{}, len(sequences))
	for _, projection := range sequences {
		if err := projection.Validate(); err != nil {
			return ExtensionOrderCapability{}, err
		}
		if err := projection.destination.ValidateScope(input.Scope); err != nil {
			return ExtensionOrderCapability{}, err
		}
		sequenceID := projection.sequence
		if _, duplicate := seen[sequenceID]; duplicate {
			return ExtensionOrderCapability{}, fmt.Errorf(
				"extension order physical sequence %q appears more than once",
				sequenceID,
			)
		}
		seen[sequenceID] = struct{}{}
		selection := arraySelection{projection.destination, projection.array}
		if _, duplicate := arrays[selection]; duplicate {
			return ExtensionOrderCapability{}, fmt.Errorf("extension order repeats a native document array")
		}
		arrays[selection] = struct{}{}
	}
	slices.SortFunc(sequences, func(left, right NativeSequenceProjection) int {
		return cmp.Compare(left.sequence, right.sequence)
	})

	return ExtensionOrderCapability{
		carrier:                input.Carrier,
		scope:                  input.Scope,
		classID:                input.ClassID,
		memberIdentityContract: input.MemberIdentityContract,
		sequenceMembership:     input.SequenceMembership,
		nativeSequences:        sequences,
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
		SequenceMembership:     capability.sequenceMembership,
		NativeSequences:        capability.nativeSequences,
		RuntimeMeaning:         capability.runtimeMeaning,
	})
	if err != nil {
		return err
	}
	if capability.carrier != canonical.carrier ||
		capability.scope != canonical.scope ||
		capability.classID != canonical.classID ||
		capability.memberIdentityContract != canonical.memberIdentityContract ||
		capability.sequenceMembership != canonical.sequenceMembership ||
		!slices.Equal(capability.nativeSequences, canonical.nativeSequences) ||
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

func (capability ExtensionOrderCapability) SequenceMembership() SequenceMembershipContract {
	return capability.sequenceMembership
}

func (capability ExtensionOrderCapability) PhysicalSequenceIDs() []hostrelation.PhysicalSequenceID {
	result := make([]hostrelation.PhysicalSequenceID, 0, len(capability.nativeSequences))
	for _, projection := range capability.nativeSequences {
		result = append(result, projection.sequence)
	}
	return result
}

func (capability ExtensionOrderCapability) NativeSequences() []NativeSequenceProjection {
	return slices.Clone(capability.nativeSequences)
}

func (capability ExtensionOrderCapability) NativeSequence(sequence hostrelation.PhysicalSequenceID) (NativeSequenceProjection, bool) {
	for _, projection := range capability.nativeSequences {
		if projection.sequence == sequence {
			return projection, true
		}
	}
	return NativeSequenceProjection{}, false
}

// AdmitsPhysicalSequenceID reports whether one independently mutable sequence
// belongs to this static order capability.
func (capability ExtensionOrderCapability) AdmitsPhysicalSequenceID(
	sequenceID hostrelation.PhysicalSequenceID,
) bool {
	_, admitted := capability.NativeSequence(sequenceID)
	return admitted
}

func (capability ExtensionOrderCapability) RuntimeMeaning() hostrelation.RuntimeMeaning {
	return capability.runtimeMeaning
}

// ExtensionOrderAdmission binds one target to an owner-local order capability.
type ExtensionOrderAdmission struct {
	target     target.Target
	capability ExtensionOrderCapability
}

func (admission ExtensionOrderAdmission) Target() target.Target { return admission.target }
func (admission ExtensionOrderAdmission) Capability() ExtensionOrderCapability {
	capability := admission.capability
	capability.nativeSequences = slices.Clone(capability.nativeSequences)
	return capability
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
			SequenceMembership:     LoadedClassSubset,
			NativeSequences: []NativeSequenceProjection{
				mustNativeSequenceProjection("opencode:project:server.json.plugins", ".opencode/opencode.json", configuration.JSONC, "plugin"),
				mustNativeSequenceProjection("opencode:project:server.jsonc.plugins", ".opencode/opencode.jsonc", configuration.JSONC, "plugin"),
				mustNativeSequenceProjection("opencode:project:tui.json.plugins", ".opencode/tui.json", configuration.JSONC, "plugin"),
				mustNativeSequenceProjection("opencode:project:tui.jsonc.plugins", ".opencode/tui.jsonc", configuration.JSONC, "plugin"),
			},
			RuntimeMeaning: hostrelation.ConfigOrderOnly,
		}),
	},
	{
		target: target.TargetOpenCode,
		capability: mustExtensionOrderCapability(ExtensionOrderCapabilityInput{
			Carrier:                desiredextension.CarrierOpenCodePlugin,
			Scope:                  target.ScopeGlobal,
			ClassID:                mustOrderClassID("extension:opencode:global:plugins"),
			MemberIdentityContract: openCodeOrderMemberIdentityV1,
			SequenceMembership:     LoadedClassSubset,
			NativeSequences: []NativeSequenceProjection{
				mustNativeSequenceProjection("opencode:global:server.json.plugins", "~/.config/opencode/opencode.json", configuration.JSONC, "plugin"),
				mustNativeSequenceProjection("opencode:global:server.jsonc.plugins", "~/.config/opencode/opencode.jsonc", configuration.JSONC, "plugin"),
				mustNativeSequenceProjection("opencode:global:tui.json.plugins", "~/.config/opencode/tui.json", configuration.JSONC, "plugin"),
				mustNativeSequenceProjection("opencode:global:tui.jsonc.plugins", "~/.config/opencode/tui.jsonc", configuration.JSONC, "plugin"),
			},
			RuntimeMeaning: hostrelation.ConfigOrderOnly,
		}),
	},
	{
		target: target.TargetPi,
		capability: mustExtensionOrderCapability(ExtensionOrderCapabilityInput{
			Carrier:                desiredextension.CarrierPiPackage,
			Scope:                  target.ScopeProject,
			ClassID:                mustOrderClassID("extension:pi:project:packages"),
			MemberIdentityContract: piOrderMemberIdentityV1,
			SequenceMembership:     CompleteClassMembership,
			NativeSequences: []NativeSequenceProjection{
				mustNativeSequenceProjection("pi:project:settings.packages", ".pi/settings.json", configuration.JSON, "packages"),
			},
			RuntimeMeaning: hostrelation.RuntimePrecedence,
		}),
	},
	{
		target: target.TargetPi,
		capability: mustExtensionOrderCapability(ExtensionOrderCapabilityInput{
			Carrier:                desiredextension.CarrierPiPackage,
			Scope:                  target.ScopeGlobal,
			ClassID:                mustOrderClassID("extension:pi:global:packages"),
			MemberIdentityContract: piOrderMemberIdentityV1,
			SequenceMembership:     CompleteClassMembership,
			NativeSequences: []NativeSequenceProjection{
				mustNativeSequenceProjection("pi:global:settings.packages", "@pi-agent/settings.json", configuration.JSON, "packages"),
			},
			RuntimeMeaning: hostrelation.RuntimePrecedence,
		}),
	},
}

// ExtensionOrderAdmissions returns the complete target-owned order catalog in
// stable owner order with independently owned sequence slices.
func ExtensionOrderAdmissions() []ExtensionOrderAdmission {
	result := make([]ExtensionOrderAdmission, 0, len(extensionOrderCapabilityCatalog))
	for _, row := range extensionOrderCapabilityCatalog {
		result = append(result, ExtensionOrderAdmission{
			target:     row.target,
			capability: row.capability,
		})
	}
	return result
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
	type selectedArray struct {
		target      target.Target
		scope       target.Scope
		destination output.Destination
		array       output.KeyPath
	}
	selectedArrays := make(map[selectedArray]struct{})
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
		for _, projection := range row.capability.nativeSequences {
			if _, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: row.target, Scope: row.capability.scope, Destination: projection.destination}); err != nil {
				return fmt.Errorf("extension order native document has no admitted root binding: %w", err)
			}
			selected := selectedArray{row.target, row.capability.scope, projection.destination, projection.array}
			if _, duplicate := selectedArrays[selected]; duplicate {
				return fmt.Errorf("extension order classes share a native document array")
			}
			selectedArrays[selected] = struct{}{}
		}
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
