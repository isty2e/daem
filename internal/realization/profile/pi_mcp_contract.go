package profile

import (
	"fmt"
	"strings"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"golang.org/x/mod/semver"
)

// PiMCPVersion is bounded version evidence, not an activation or runtime-readiness certificate.
type PiMCPVersion struct {
	version string
}

// ObservePiMCPVersion admits only a complete stable semantic version from a successful version command.
func ObservePiMCPVersion(output string) PiMCPVersion {
	value := strings.TrimSpace(output)
	if len(value) > 64 {
		return PiMCPVersion{}
	}
	version := "v" + strings.TrimPrefix(value, "v")
	if !semver.IsValid(version) || semver.Canonical(version) != version ||
		semver.Prerelease(version) != "" || semver.Build(version) != "" {
		return PiMCPVersion{}
	}
	return PiMCPVersion{version: version}
}

// NativeCompatible reports compatibility with the admitted native file contract.
func (version PiMCPVersion) NativeCompatible() bool {
	return version.version != "" && semver.Compare(version.version, "v1.0.2") >= 0 &&
		semver.Compare(version.version, "v2.0.0") < 0
}

// PiMCPContract owns the interpretation of one recorded Pi MCP backend.
type PiMCPContract struct {
	backend desiredmcp.Backend
}

// PiMCPContractForBackend admits an explicit backend, independent of current host facts.
func PiMCPContractForBackend(backend desiredmcp.Backend) (PiMCPContract, error) {
	switch backend {
	case desiredmcp.BackendNative, desiredmcp.BackendAdapter:
		return PiMCPContract{backend: backend}, nil
	default:
		return PiMCPContract{}, fmt.Errorf("unsupported Pi MCP backend %q", backend)
	}
}

// PiMCPContractForCodec resolves a stored Pi contract for observation and retirement.
func PiMCPContractForCodec(codec aggregate.CodecContractID) (PiMCPContract, bool) {
	switch codec {
	case aggregate.MCPCodecPiNativeStdio:
		return PiMCPContract{backend: desiredmcp.BackendNative}, true
	case aggregate.MCPCodecPiAdapterStdio:
		return PiMCPContract{backend: desiredmcp.BackendAdapter}, true
	default:
		return PiMCPContract{}, false
	}
}

// SelectPiMCPContract preserves existing intent before considering automatic version selection.
func SelectPiMCPContract(existing desiredmcp.Backend, explicitProvider bool, version PiMCPVersion) (PiMCPContract, error) {
	if existing != "" {
		contract, err := PiMCPContractForBackend(existing)
		if err != nil {
			return PiMCPContract{}, err
		}
		if contract.Backend() == desiredmcp.BackendNative && explicitProvider {
			return PiMCPContract{}, fmt.Errorf("native Pi MCP cannot be combined with a declared pi-mcp-adapter")
		}
		return contract, nil
	}
	if explicitProvider || !version.NativeCompatible() {
		return PiMCPContract{backend: desiredmcp.BackendAdapter}, nil
	}
	return PiMCPContract{backend: desiredmcp.BackendNative}, nil
}

func (contract PiMCPContract) Backend() desiredmcp.Backend { return contract.backend }

func (contract PiMCPContract) CodecContractID() aggregate.CodecContractID {
	return aggregate.PiMCPProjectionCodec(contract.backend)
}

func (contract PiMCPContract) RequiresProvider() bool {
	return contract.backend == desiredmcp.BackendAdapter
}

// AdmitPeer rejects mixed native and adapter contracts in one Pi session context.
func (contract PiMCPContract) AdmitPeer(peer desiredmcp.Backend) error {
	if _, err := PiMCPContractForBackend(contract.backend); err != nil {
		return err
	}
	if peer != contract.backend {
		return fmt.Errorf("mixed native and adapter Pi MCP declarations are unsupported; existing bindings were not converted")
	}
	return nil
}

// Placement projects this selected contract onto the canonical physical address.
func (contract PiMCPContract) Placement(scope target.Scope) (aggregate.MCPPlacement, error) {
	if _, err := PiMCPContractForBackend(contract.backend); err != nil {
		return aggregate.MCPPlacement{}, err
	}
	physical, ok := aggregate.ImplementedMCPPlacement(target.TargetPi, scope)
	if !ok {
		return aggregate.MCPPlacement{}, fmt.Errorf("unsupported Pi MCP scope %q", scope)
	}
	placement, ok := aggregate.MCPPlacementForCodec(physical.ID(), contract.CodecContractID())
	if !ok {
		return aggregate.MCPPlacement{}, fmt.Errorf("unsupported Pi MCP projection contract")
	}
	return placement, nil
}

// PiNativeHostFacts qualifies configuration selection, not trust or runtime connectivity.
type PiNativeHostFacts struct {
	Version               PiMCPVersion
	Scope                 target.Scope
	GlobalBuiltinEnabled  bool
	ProjectBuiltinEnabled bool
	AdapterDeclared       bool
}

// PiMCPBuiltinEnabled evaluates builtin directives in one ordered settings layer.
func PiMCPBuiltinEnabled(base bool, directives []string) bool {
	for _, directive := range directives {
		switch directive {
		case "-builtin:mcp":
			base = false
		case "+builtin:mcp", "builtin:mcp":
			base = true
		}
	}
	return base
}

// PiMCPAdapterPackageSource identifies configured npm replacement intent, independent of version admission.
func PiMCPAdapterPackageSource(source string) bool {
	value := strings.TrimPrefix(source, "npm:")
	return value == piMCPProviderPackageName || strings.HasPrefix(value, piMCPProviderPackageName+"@")
}

// QualifyNativeHost consumes fresh selection facts without changing this fixed backend.
func (contract PiMCPContract) QualifyNativeHost(facts PiNativeHostFacts) error {
	if err := contract.QualifyNativeVersion(facts.Version); err != nil {
		return err
	}
	if facts.Scope != target.ScopeProject && facts.Scope != target.ScopeGlobal {
		return fmt.Errorf("native Pi MCP qualification requires project or global scope")
	}
	if !facts.ProjectBuiltinEnabled || (facts.Scope == target.ScopeGlobal && !facts.GlobalBuiltinEnabled) {
		return fmt.Errorf("native Pi MCP builtin is disabled in the applicable settings envelope; no settings were changed")
	}
	if facts.AdapterDeclared {
		return fmt.Errorf("configured pi-mcp-adapter may replace the native MCP builtin; package removal is separate and no backend fallback was performed")
	}
	return nil
}

// QualifyNativeVersion checks a fixed native choice; it never selects an adapter fallback.
func (contract PiMCPContract) QualifyNativeVersion(version PiMCPVersion) error {
	if contract.backend != desiredmcp.BackendNative {
		return fmt.Errorf("native version qualification requires the native Pi MCP contract")
	}
	if !version.NativeCompatible() {
		return fmt.Errorf("native Pi MCP requires an observable stable Pi version >=1.0.2,<2.0.0; the recorded backend was not changed")
	}
	return nil
}
