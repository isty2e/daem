package profile

import (
	"testing"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeVersionSelectionAndFixedIntent(t *testing.T) {
	for _, test := range []struct {
		output string
		native bool
	}{
		{"1.0.2\n", true},
		{"v1.0.2", true},
		{"1.9.3", true},
		{"", false},
		{"0.99.0", false},
		{"1.0.1", false},
		{"2.0.0", false},
		{"1.0", false},
		{"1.0.2-beta.1", false},
		{"1.0.2+build", false},
		{"pi 1.0.2", false},
		{"1.0.2\n1.0.3", false},
	} {
		t.Run(test.output, func(t *testing.T) {
			version := ObservePiMCPVersion(test.output)
			selected, err := SelectPiMCPContract("", false, version)
			if err != nil || (selected.Backend() == desiredmcp.BackendNative) != test.native {
				t.Fatalf("selection = %q, %v; native = %t", selected.Backend(), err, test.native)
			}
			for _, backend := range []desiredmcp.Backend{desiredmcp.BackendAdapter, desiredmcp.BackendNative} {
				fixed, err := SelectPiMCPContract(backend, false, version)
				if err != nil || fixed.Backend() != backend {
					t.Fatalf("fixed %q = %q, %v", backend, fixed.Backend(), err)
				}
			}
			provider, err := SelectPiMCPContract("", true, version)
			if err != nil || provider.Backend() != desiredmcp.BackendAdapter {
				t.Fatalf("explicit provider selection = %q, %v", provider.Backend(), err)
			}
		})
	}
	if _, err := SelectPiMCPContract(desiredmcp.BackendNative, true, ObservePiMCPVersion("1.0.2")); err == nil {
		t.Fatal("native plus explicit adapter was accepted")
	}
	if _, err := PiMCPContractForBackend("invented"); err == nil {
		t.Fatal("unknown backend was accepted")
	}
}

func TestPiNativeAndAdapterSharePhysicalIdentityNotCodecOrPrerequisite(t *testing.T) {
	native, _ := PiMCPContractForBackend(desiredmcp.BackendNative)
	adapter, _ := PiMCPContractForBackend(desiredmcp.BackendAdapter)
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		nativePlacement, err := native.Placement(scope)
		if err != nil {
			t.Fatal(err)
		}
		adapterPlacement, err := adapter.Placement(scope)
		if err != nil {
			t.Fatal(err)
		}
		nativePath, _ := nativePlacement.ContentPath("context7")
		adapterPath, _ := adapterPlacement.ContentPath("context7")
		if nativePlacement.ID() != adapterPlacement.ID() || nativePlacement.ConfigPath() != adapterPlacement.ConfigPath() || nativePath != adapterPath {
			t.Fatal("backend selection changed physical identity")
		}
		if nativePlacement.CodecContractID() != aggregate.MCPCodecPiNativeStdio || adapterPlacement.CodecContractID() != aggregate.MCPCodecPiAdapterStdio {
			t.Fatal("distinct backend codecs were not retained")
		}
	}
	if native.RequiresProvider() || !adapter.RequiresProvider() {
		t.Fatal("provider requirements were inferred incorrectly")
	}
	for _, codec := range []aggregate.CodecContractID{aggregate.MCPCodecPiAdapterStdio, aggregate.MCPCodecPiNativeStdio} {
		stored, ok := PiMCPContractForCodec(codec)
		if !ok || stored.CodecContractID() != codec {
			t.Fatalf("stored codec %q was reinterpreted", codec)
		}
	}
}

func TestPiNativeHostQualificationRefusesWithoutFallback(t *testing.T) {
	contract, _ := PiMCPContractForBackend(desiredmcp.BackendNative)
	valid := PiNativeHostFacts{
		Version: ObservePiMCPVersion("1.0.2"), Scope: target.ScopeProject,
		BuiltinSelection: ResolvePiMCPBuiltinSelection(nil, nil),
	}
	if err := contract.QualifyNativeHost(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PiNativeHostFacts){
		func(facts *PiNativeHostFacts) { facts.Version = PiMCPVersion{} },
		func(facts *PiNativeHostFacts) {
			facts.BuiltinSelection = ResolvePiMCPBuiltinSelection(nil, []string{"-builtin:mcp"})
		},
		func(facts *PiNativeHostFacts) { facts.BuiltinSelection = PiMCPBuiltinSelection{} },
		func(facts *PiNativeHostFacts) { facts.AdapterDeclared = true },
		func(facts *PiNativeHostFacts) { facts.Scope = "invalid" },
	} {
		facts := valid
		change(&facts)
		if err := contract.QualifyNativeHost(facts); err == nil || contract.Backend() != desiredmcp.BackendNative {
			t.Fatalf("qualification = %v, backend = %q", err, contract.Backend())
		}
	}
}
