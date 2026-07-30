package profile

import (
	"reflect"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestExtensionOrderCapabilityMatrixAdmitsOnlyOpenCodeAndPi(t *testing.T) {
	tests := []struct {
		target         target.Target
		carrier        desiredextension.Carrier
		scope          target.Scope
		classID        string
		sequenceIDs    []string
		membership     SequenceMembershipContract
		runtimeMeaning hostrelation.RuntimeMeaning
	}{
		{
			target: target.TargetOpenCode, carrier: desiredextension.CarrierOpenCodePlugin,
			scope: target.ScopeProject, classID: "extension:opencode:project:plugins",
			sequenceIDs:    []string{"opencode:project:server.plugins", "opencode:project:tui.plugins"},
			membership:     LoadedClassSubset,
			runtimeMeaning: hostrelation.ConfigOrderOnly,
		},
		{
			target: target.TargetOpenCode, carrier: desiredextension.CarrierOpenCodePlugin,
			scope: target.ScopeGlobal, classID: "extension:opencode:global:plugins",
			sequenceIDs:    []string{"opencode:global:server.plugins", "opencode:global:tui.plugins"},
			membership:     LoadedClassSubset,
			runtimeMeaning: hostrelation.ConfigOrderOnly,
		},
		{
			target: target.TargetPi, carrier: desiredextension.CarrierPiPackage,
			scope: target.ScopeProject, classID: "extension:pi:project:packages",
			sequenceIDs:    []string{"pi:project:settings.packages"},
			membership:     CompleteClassMembership,
			runtimeMeaning: hostrelation.RuntimePrecedence,
		},
		{
			target: target.TargetPi, carrier: desiredextension.CarrierPiPackage,
			scope: target.ScopeGlobal, classID: "extension:pi:global:packages",
			sequenceIDs:    []string{"pi:global:settings.packages"},
			membership:     CompleteClassMembership,
			runtimeMeaning: hostrelation.RuntimePrecedence,
		},
	}

	for _, test := range tests {
		t.Run(string(test.target)+"/"+string(test.scope), func(t *testing.T) {
			capability, ok := Profile(test.target).ExtensionOrder(test.carrier, test.scope)
			if !ok {
				t.Fatal("ExtensionOrder returned no capability")
			}
			if string(capability.ClassID()) != test.classID ||
				capability.SequenceMembership() != test.membership ||
				capability.RuntimeMeaning() != test.runtimeMeaning {
				t.Fatalf("capability = %#v", capability)
			}
			gotSequenceIDs := make([]string, 0, len(capability.PhysicalSequenceIDs()))
			for _, sequenceID := range capability.PhysicalSequenceIDs() {
				gotSequenceIDs = append(gotSequenceIDs, string(sequenceID))
			}
			if !reflect.DeepEqual(gotSequenceIDs, test.sequenceIDs) {
				t.Fatalf("sequence ids = %v, want %v", gotSequenceIDs, test.sequenceIDs)
			}
			if capability.MemberIdentityContract() == "" ||
				capability.observerContract == "" ||
				capability.mutatorContract == "" ||
				capability.codecContractVersion == "" {
				t.Fatalf("capability has an incomplete contract: %#v", capability)
			}
		})
	}

	for _, unsupported := range []struct {
		target  target.Target
		carrier desiredextension.Carrier
		scope   target.Scope
	}{
		{target.TargetClaudeCode, desiredextension.CarrierClaudeCodePlugin, target.ScopeProject},
		{target.TargetClaudeCode, desiredextension.CarrierClaudeCodePlugin, target.ScopeGlobal},
		{target.TargetCodex, desiredextension.CarrierCodexPlugin, target.ScopeGlobal},
		{target.TargetAntigravityCLI, desiredextension.CarrierAntigravityCLIPlugin, target.ScopeGlobal},
	} {
		if capability, ok := Profile(unsupported.target).ExtensionOrder(
			unsupported.carrier,
			unsupported.scope,
		); ok {
			t.Fatalf(
				"%s/%s unexpectedly admitted order capability %#v",
				unsupported.target,
				unsupported.scope,
				capability,
			)
		}
	}
}

func TestExtensionOrderCapabilityForClassReturnsUniqueCatalogOwner(t *testing.T) {
	classID := mustOrderClassID("extension:opencode:project:plugins")
	selectedTarget, capability, ok := ExtensionOrderCapabilityForClass(classID)
	if !ok {
		t.Fatal("ExtensionOrderCapabilityForClass returned no capability")
	}
	if selectedTarget != target.TargetOpenCode ||
		capability.ClassID() != classID ||
		capability.Scope() != target.ScopeProject ||
		capability.Carrier() != desiredextension.CarrierOpenCodePlugin {
		t.Fatalf(
			"owner=%q capability=%#v",
			selectedTarget,
			capability,
		)
	}

	unknown := mustOrderClassID("extension:unknown:project:plugins")
	if selectedTarget, capability, ok := ExtensionOrderCapabilityForClass(unknown); ok ||
		selectedTarget != "" ||
		!reflect.DeepEqual(capability, ExtensionOrderCapability{}) {
		t.Fatalf(
			"unknown owner=%q capability=%#v ok=%t",
			selectedTarget,
			capability,
			ok,
		)
	}
}

func TestExtensionOrderCapabilityDefensivelyCopiesAndCanonicalizesSequences(t *testing.T) {
	input := ExtensionOrderCapabilityInput{
		Carrier:                desiredextension.CarrierOpenCodePlugin,
		Scope:                  target.ScopeProject,
		ClassID:                mustOrderClassID("extension:test:project:plugins"),
		MemberIdentityContract: "test-member-v1",
		SequenceMembership:     LoadedClassSubset,
		PhysicalSequenceIDs: []hostrelation.PhysicalSequenceID{
			mustPhysicalSequenceID("test:tui"),
			mustPhysicalSequenceID("test:server"),
		},
		ObserverContract:     "test-observer-v1",
		MutatorContract:      "test-mutator-v1",
		CodecContractVersion: "test-codec-v1",
		RuntimeMeaning:       hostrelation.ConfigOrderOnly,
	}
	capability, err := NewExtensionOrderCapability(input)
	if err != nil {
		t.Fatal(err)
	}
	input.PhysicalSequenceIDs[0] = ""
	returned := capability.PhysicalSequenceIDs()
	returned[0] = ""

	got := capability.PhysicalSequenceIDs()
	if capability.Validate() != nil ||
		!reflect.DeepEqual(got, []hostrelation.PhysicalSequenceID{
			mustPhysicalSequenceID("test:server"),
			mustPhysicalSequenceID("test:tui"),
		}) {
		t.Fatalf("canonical sequence ids = %v", got)
	}
}

func TestExtensionOrderCapabilityRejectsPartialAndCollidingContracts(t *testing.T) {
	base := ExtensionOrderCapabilityInput{
		Carrier:                desiredextension.CarrierOpenCodePlugin,
		Scope:                  target.ScopeProject,
		ClassID:                mustOrderClassID("extension:test:project:plugins"),
		MemberIdentityContract: "test-member-v1",
		SequenceMembership:     LoadedClassSubset,
		PhysicalSequenceIDs:    []hostrelation.PhysicalSequenceID{mustPhysicalSequenceID("test:server")},
		ObserverContract:       "test-observer-v1",
		MutatorContract:        "test-mutator-v1",
		CodecContractVersion:   "test-codec-v1",
		RuntimeMeaning:         hostrelation.ConfigOrderOnly,
	}

	for _, mutate := range []func(*ExtensionOrderCapabilityInput){
		func(input *ExtensionOrderCapabilityInput) { input.MemberIdentityContract = "" },
		func(input *ExtensionOrderCapabilityInput) { input.SequenceMembership = "" },
		func(input *ExtensionOrderCapabilityInput) { input.PhysicalSequenceIDs = nil },
		func(input *ExtensionOrderCapabilityInput) { input.ObserverContract = "" },
		func(input *ExtensionOrderCapabilityInput) { input.MutatorContract = "" },
		func(input *ExtensionOrderCapabilityInput) { input.CodecContractVersion = "" },
		func(input *ExtensionOrderCapabilityInput) { input.RuntimeMeaning = "" },
	} {
		input := base
		mutate(&input)
		if _, err := NewExtensionOrderCapability(input); err == nil {
			t.Fatalf("NewExtensionOrderCapability accepted %#v", input)
		}
	}

	first := extensionOrderCapabilityRow{
		target:     target.TargetOpenCode,
		capability: mustExtensionOrderCapability(base),
	}
	duplicate := first
	if err := validateExtensionOrderCapabilityCatalog(
		[]extensionOrderCapabilityRow{first, duplicate},
	); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate catalog error = %v", err)
	}
}
