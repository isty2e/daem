package profile

import (
	"errors"
	"fmt"
	"path"
	"strings"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

var (
	ErrHostConfigReserved = errors.New("host configuration overlaps a reserved area")
	ErrHostConfigGrammar  = errors.New("host configuration grammar is not admitted")
)

// HostConfigGrammar is selected from logical profile facts, before physical qualification.
type HostConfigGrammar string

const (
	HostConfigOpaque           HostConfigGrammar = "opaque"
	HostConfigJSON             HostConfigGrammar = "json"
	HostConfigJSONC            HostConfigGrammar = "jsonc"
	HostConfigJSONLineComments HostConfigGrammar = "json-line-comments"
	HostConfigTOML             HostConfigGrammar = "toml"
)

type HostConfigObservation string

const (
	HostConfigObserveReferentAliases       HostConfigObservation = "referent_aliases"
	HostConfigObserveInstalledClaims       HostConfigObservation = "installed_claims"
	HostConfigObservePiLoader              HostConfigObservation = "pi_loader_entries"
	HostConfigObserveCodexRuntimeLocations HostConfigObservation = "codex_runtime_locations"
)

type hostConfigFieldReservation struct {
	key   string
	owner string
}

type HostConfigPlacementInput struct {
	Target    target.Target
	Scope     target.Scope
	Path      string
	Ownership desiredhostconfig.Ownership
	Syntax    desiredhostconfig.Syntax
}

// HostConfigPlacement contains static facts only. It is not physical admission
// or evidence that the required observations have completed.
type HostConfigPlacement struct {
	target          target.Target
	scope           target.Scope
	nativePath      string
	ownership       desiredhostconfig.Ownership
	grammar         HostConfigGrammar
	requestedSyntax desiredhostconfig.Syntax
	documentKind    hostConfigDocumentKind
	destination     output.Destination
}

func ResolveHostConfigPlacement(input HostConfigPlacementInput) (HostConfigPlacement, error) {
	selected, err := target.ParseTarget(string(input.Target))
	if err != nil {
		return HostConfigPlacement{}, err
	}
	if selected != target.TargetPi && selected != target.TargetCodex {
		return HostConfigPlacement{}, fmt.Errorf("host configuration target %q is unsupported", selected)
	}
	scope, err := target.ParseScope(string(input.Scope))
	if err != nil {
		return HostConfigPlacement{}, err
	}
	if err := desiredhostconfig.ValidateRelativePath(input.Path); err != nil {
		return HostConfigPlacement{}, err
	}
	ownership, err := desiredhostconfig.ParseOwnership(string(input.Ownership))
	if err != nil {
		return HostConfigPlacement{}, err
	}
	syntax, err := desiredhostconfig.ParseSyntax(string(input.Syntax))
	if err != nil {
		return HostConfigPlacement{}, err
	}

	kind := hostConfigDocumentClass(selected, scope, input.Path)
	if owner, reserved := hostConfigPathOwner(selected, scope, input.Path); reserved {
		return HostConfigPlacement{}, fmt.Errorf("%w: %q belongs to %s", ErrHostConfigReserved, input.Path, owner)
	}
	if ownership == desiredhostconfig.OwnershipDocument && hostConfigSharedDocument(kind) {
		return HostConfigPlacement{}, fmt.Errorf("%w: %q requires key ownership alongside its dedicated resources", ErrHostConfigReserved, input.Path)
	}
	grammar, err := hostConfigGrammar(kind, input.Path, ownership, syntax)
	if err != nil {
		return HostConfigPlacement{}, err
	}

	root := "@pi-agent/"
	projectRoot := ".pi/"
	if selected == target.TargetCodex {
		root, projectRoot = "@codex-home/", ".codex/"
	}
	if scope == target.ScopeProject {
		root = projectRoot
	}
	destination, err := output.Parse(root + input.Path)
	if err != nil {
		return HostConfigPlacement{}, err
	}
	if err := destination.ValidateScope(scope); err != nil {
		return HostConfigPlacement{}, err
	}
	return HostConfigPlacement{
		target: selected, scope: scope, nativePath: input.Path,
		ownership: ownership, grammar: grammar, requestedSyntax: syntax,
		documentKind: kind, destination: destination,
	}, nil
}

func (placement HostConfigPlacement) Target() target.Target { return placement.target }
func (placement HostConfigPlacement) Scope() target.Scope   { return placement.scope }
func (placement HostConfigPlacement) NativePath() string    { return placement.nativePath }
func (placement HostConfigPlacement) Ownership() desiredhostconfig.Ownership {
	return placement.ownership
}
func (placement HostConfigPlacement) Grammar() HostConfigGrammar { return placement.grammar }
func (placement HostConfigPlacement) Destination() output.Destination {
	return placement.destination
}

func (placement HostConfigPlacement) RequestedSyntax() desiredhostconfig.Syntax {
	return placement.requestedSyntax
}

func (placement HostConfigPlacement) ID() string {
	if placement.documentKind == 0 {
		return ""
	}
	return fmt.Sprintf("%s-host-config-%s-v1", placement.target, placement.scope)
}

func (placement HostConfigPlacement) Validate() error {
	canonical, err := ResolveHostConfigPlacement(HostConfigPlacementInput{
		Target: placement.target, Scope: placement.scope, Path: placement.nativePath,
		Ownership: placement.ownership, Syntax: placement.requestedSyntax,
	})
	if err != nil {
		return err
	}
	if canonical != placement {
		return fmt.Errorf("host configuration placement is not canonical")
	}
	return nil
}

// ValidateStaticFootprint checks logical ownership only, never current aliases,
// installed resources or host loader state.
func (placement HostConfigPlacement) ValidateStaticFootprint(footprint output.Footprint) error {
	if placement.documentKind == 0 {
		return fmt.Errorf("host configuration placement is required")
	}
	if err := footprint.Validate(); err != nil {
		return err
	}
	if placement.ownership == desiredhostconfig.OwnershipDocument {
		if footprint.Kind() != output.FootprintPath {
			return fmt.Errorf("document ownership requires a whole-path footprint")
		}
		return nil
	}
	key, keyOwned := footprint.Key()
	if !keyOwned {
		return fmt.Errorf("key ownership requires a native key footprint")
	}
	for _, field := range hostConfigReservedFields(placement.documentKind) {
		reserved, err := output.NewKeyPath(field.key)
		if err != nil {
			return err
		}
		if reserved.Overlaps(key) {
			return fmt.Errorf("%w: %q belongs to %s; use its dedicated resource", ErrHostConfigReserved, key.String(), field.owner)
		}
	}
	return nil
}

func (placement HostConfigPlacement) RequiredObservations() []HostConfigObservation {
	if placement.documentKind == 0 {
		return nil
	}
	observations := []HostConfigObservation{HostConfigObserveReferentAliases, HostConfigObserveInstalledClaims}
	if placement.target == target.TargetPi {
		return append(observations, HostConfigObservePiLoader)
	}
	return append(observations, HostConfigObserveCodexRuntimeLocations)
}

func hostConfigGrammar(kind hostConfigDocumentKind, nativePath string, ownership desiredhostconfig.Ownership, requested desiredhostconfig.Syntax) (HostConfigGrammar, error) {
	known := HostConfigGrammar("")
	switch kind {
	case hostConfigPiSettings:
		known = HostConfigJSON
	case hostConfigPiModels:
		known = HostConfigJSONLineComments
	case hostConfigCodexConfiguration:
		known = HostConfigTOML
	}
	if known != "" {
		if requested != desiredhostconfig.SyntaxDefault && HostConfigGrammar(requested) != known {
			return "", fmt.Errorf("%w: %q requires %s", ErrHostConfigGrammar, nativePath, known)
		}
		return known, nil
	}
	if requested != desiredhostconfig.SyntaxDefault {
		return HostConfigGrammar(requested), nil
	}
	switch strings.ToLower(path.Ext(nativePath)) {
	case ".json":
		return HostConfigJSON, nil
	case ".jsonc":
		return HostConfigJSONC, nil
	case ".toml":
		return HostConfigTOML, nil
	}
	if ownership == desiredhostconfig.OwnershipDocument {
		return HostConfigOpaque, nil
	}
	return "", fmt.Errorf("%w: key ownership requires an explicit supported syntax for %q", ErrHostConfigGrammar, nativePath)
}

func hostConfigCodexDocument(scope target.Scope, nativePath string) bool {
	if nativePath == "config.toml" {
		return true
	}
	if scope != target.ScopeGlobal {
		return false
	}
	name, profile := strings.CutSuffix(nativePath, ".config.toml")
	if !profile || name == "" {
		return false
	}
	return strings.IndexFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) < 0
}

func hostConfigSharedDocument(kind hostConfigDocumentKind) bool {
	return kind == hostConfigPiSettings || kind == hostConfigCodexConfiguration
}

func hostConfigReservedFields(kind hostConfigDocumentKind) []hostConfigFieldReservation {
	if kind == hostConfigPiSettings {
		return []hostConfigFieldReservation{
			{"packages", "extension packages"},
			{"extensions", "extensions"},
			{"skills", "skills"},
		}
	}
	if kind == hostConfigCodexConfiguration {
		return []hostConfigFieldReservation{
			{"mcp_servers", "MCP servers"},
			{"hooks", "hooks"},
			{"plugins", "extensions"},
			{"marketplaces", "extension marketplaces"},
		}
	}
	return nil
}

type hostConfigDocumentKind uint8

const (
	hostConfigOrdinaryDocument hostConfigDocumentKind = iota + 1
	hostConfigPiSettings
	hostConfigPiModels
	hostConfigCodexConfiguration
)

func hostConfigDocumentClass(selected target.Target, scope target.Scope, nativePath string) hostConfigDocumentKind {
	if selected == target.TargetPi {
		if nativePath == "settings.json" {
			return hostConfigPiSettings
		}
		if scope == target.ScopeGlobal && nativePath == "models.json" {
			return hostConfigPiModels
		}
	}
	if selected == target.TargetCodex && hostConfigCodexDocument(scope, nativePath) {
		return hostConfigCodexConfiguration
	}
	return hostConfigOrdinaryDocument
}
