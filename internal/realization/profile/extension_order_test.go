package profile

import (
	"reflect"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
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
			sequenceIDs: []string{
				"opencode:project:server.json.plugins",
				"opencode:project:server.jsonc.plugins",
				"opencode:project:tui.json.plugins",
				"opencode:project:tui.jsonc.plugins",
			},
			membership:     LoadedClassSubset,
			runtimeMeaning: hostrelation.ConfigOrderOnly,
		},
		{
			target: target.TargetOpenCode, carrier: desiredextension.CarrierOpenCodePlugin,
			scope: target.ScopeGlobal, classID: "extension:opencode:global:plugins",
			sequenceIDs: []string{
				"opencode:global:server.json.plugins",
				"opencode:global:server.jsonc.plugins",
				"opencode:global:tui.json.plugins",
				"opencode:global:tui.jsonc.plugins",
			},
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
			if capability.MemberIdentityContract() == "" {
				t.Fatalf("capability has an incomplete contract: %#v", capability)
			}
			for _, sequenceID := range capability.PhysicalSequenceIDs() {
				if !capability.AdmitsPhysicalSequenceID(sequenceID) {
					t.Fatalf("capability rejected admitted sequence %q", sequenceID)
				}
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

func TestExtensionOrderCapabilityDefensivelyCopiesAndCanonicalizesSequences(t *testing.T) {
	input := ExtensionOrderCapabilityInput{
		Carrier:                desiredextension.CarrierOpenCodePlugin,
		Scope:                  target.ScopeProject,
		ClassID:                mustOrderClassID("extension:test:project:plugins"),
		MemberIdentityContract: "test-member-v1",
		SequenceMembership:     LoadedClassSubset,
		NativeSequences: []NativeSequenceProjection{
			mustNativeSequenceProjection("test:tui", ".opencode/tui.json", configuration.JSONC, "plugin"),
			mustNativeSequenceProjection("test:server", ".opencode/opencode.json", configuration.JSONC, "plugin"),
		},
		RuntimeMeaning: hostrelation.ConfigOrderOnly,
	}
	capability, err := NewExtensionOrderCapability(input)
	if err != nil {
		t.Fatal(err)
	}
	input.NativeSequences[0] = NativeSequenceProjection{}
	returned := capability.PhysicalSequenceIDs()
	returned[0] = ""
	projections := capability.NativeSequences()
	projections[0] = NativeSequenceProjection{}

	got := capability.PhysicalSequenceIDs()
	if capability.Validate() != nil ||
		capability.AdmitsPhysicalSequenceID("") ||
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
		NativeSequences:        []NativeSequenceProjection{mustNativeSequenceProjection("test:server", ".opencode/opencode.json", configuration.JSONC, "plugin")},
		RuntimeMeaning:         hostrelation.ConfigOrderOnly,
	}

	for _, mutate := range []func(*ExtensionOrderCapabilityInput){
		func(input *ExtensionOrderCapabilityInput) { input.MemberIdentityContract = "" },
		func(input *ExtensionOrderCapabilityInput) { input.SequenceMembership = "" },
		func(input *ExtensionOrderCapabilityInput) { input.NativeSequences = nil },
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

func TestNativeSequenceProjectionRetainsExplicitDocumentAndArrayIdentity(t *testing.T) {
	capability, admitted := Profile(target.TargetOpenCode).ExtensionOrder(desiredextension.CarrierOpenCodePlugin, target.ScopeGlobal)
	if !admitted {
		t.Fatal("OpenCode order capability missing")
	}
	sequence, present := capability.NativeSequence(mustPhysicalSequenceID("opencode:global:server.json.plugins"))
	if !present || sequence.Destination().String() != "~/.config/opencode/opencode.json" || sequence.Grammar() != configuration.JSONC || sequence.ArrayPath().String() != "/plugin" {
		t.Fatal("stable sequence ID was used to infer a native filename or plural array key", sequence)
	}
	if _, present := capability.NativeSequence(mustPhysicalSequenceID("unknown:sequence")); present {
		t.Fatal("unregistered sequence acquired a projection")
	}
	if (NativeSequenceProjection{}).Validate() == nil {
		t.Fatal("zero native sequence projection admitted")
	}
	destination, err := output.Parse(".opencode/opencode.json")
	if err != nil {
		t.Fatal(err)
	}
	array, err := output.NewKeyPath("plugin")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		grammar configuration.Grammar
		array   output.KeyPath
	}{
		{configuration.TOML, array}, {configuration.JSON, output.KeyPath{}},
	} {
		if projection, err := NewNativeSequenceProjection(mustPhysicalSequenceID("test:array"), destination, test.grammar, test.array); err == nil || projection != (NativeSequenceProjection{}) {
			t.Fatal("unsupported array projection retained partial facts", projection, err)
		}
	}
	input := ExtensionOrderCapabilityInput{
		Carrier: desiredextension.CarrierOpenCodePlugin, Scope: target.ScopeProject,
		ClassID: mustOrderClassID("test:class"), MemberIdentityContract: "test-member-v1",
		SequenceMembership: LoadedClassSubset, RuntimeMeaning: hostrelation.ConfigOrderOnly,
		NativeSequences: []NativeSequenceProjection{
			mustNativeSequenceProjection("test:first", ".opencode/opencode.json", configuration.JSONC, "plugin"),
			mustNativeSequenceProjection("test:second", ".opencode/opencode.json", configuration.JSONC, "plugin"),
		},
	}
	if _, err := NewExtensionOrderCapability(input); err == nil {
		t.Fatal("two physical IDs selected one native array")
	}
	input.NativeSequences = []NativeSequenceProjection{mustNativeSequenceProjection("test:global", "~/.config/opencode/opencode.json", configuration.JSONC, "plugin")}
	if _, err := NewExtensionOrderCapability(input); err == nil {
		t.Fatal("a global document entered a project sequence projection")
	}
}
