package extension

import (
	"fmt"

	observepi "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

const (
	reasonSourceNotImportable           = "source_not_importable"
	reasonSourceProvenanceUnrecoverable = "source_provenance_unrecoverable"
)

// Collect reads each selected host inventory once, emits every skipped row at
// its first amplifying boundary, and returns one deterministic exact-extension
// authoring proposal.
func Collect(input Input, emitSkip func(Skip) error) (Result, error) {
	if err := validateInput(input); err != nil {
		return Result{}, err
	}
	if emitSkip == nil {
		return Result{}, fmt.Errorf("extension import skip emitter is required")
	}
	collector := importCollector{
		input:                  input,
		emitSkip:               emitSkip,
		candidates:             make(map[desiredextension.CarrierKey]candidateFact),
		openCodeSourcePaths:    make(map[target.Scope]string),
		existingLoadIdentities: make(map[desiredextension.CarrierKey]hostrelation.HostLoadIdentity),
	}
	if err := collector.collectSelected(); err != nil {
		return Result{}, err
	}
	if err := collector.loadExistingIdentities(); err != nil {
		return Result{}, err
	}
	assigned, err := assignExtensionIDs(collector.candidates, input.Existing)
	if err != nil {
		return Result{}, err
	}
	extensions, orderedExtensions, order, sequences, constraints, err := planOrder(
		collector.candidates,
		input.Existing,
		collector.existingLoadIdentities,
		assigned,
		collector.sequences,
	)
	if err != nil {
		return Result{}, err
	}
	return Result{
		extensions:        extensions,
		orderedExtensions: orderedExtensions,
		order:             order,
		sequences:         sequences,
		constraints:       constraints,
		scans:             append([]Scan(nil), collector.scans...),
		skipped:           append([]Skip(nil), collector.skipped...),
	}, nil
}

type importCollector struct {
	input                  Input
	emitSkip               func(Skip) error
	candidates             map[desiredextension.CarrierKey]candidateFact
	sequences              []sequenceFact
	scans                  []Scan
	skipped                []Skip
	openCodeSourcePaths    map[target.Scope]string
	existingLoadIdentities map[desiredextension.CarrierKey]hostrelation.HostLoadIdentity
}

func (collector *importCollector) addSkip(skip Skip) error {
	if collector == nil || collector.emitSkip == nil {
		return fmt.Errorf("extension import skip emitter is required")
	}
	if err := collector.emitSkip(skip); err != nil {
		return err
	}
	collector.skipped = append(collector.skipped, skip)
	return nil
}

func (collector *importCollector) collectSelected() error {
	if collector.includesTarget(target.TargetClaudeCode) {
		if err := collector.collectClaude(); err != nil {
			return err
		}
	}
	if collector.includesTarget(target.TargetCodex) &&
		collector.includesScope(target.ScopeGlobal) {
		if err := collector.collectCodex(); err != nil {
			return err
		}
	}
	if collector.includesTarget(target.TargetOpenCode) {
		for _, scope := range collector.input.Scopes {
			if err := collector.collectOpenCode(scope); err != nil {
				return err
			}
		}
	}
	if collector.includesTarget(target.TargetPi) {
		for _, scope := range collector.input.Scopes {
			if err := collector.collectPi(scope); err != nil {
				return err
			}
		}
	}
	if collector.includesTarget(target.TargetAntigravityCLI) &&
		collector.includesScope(target.ScopeGlobal) {
		if err := collector.collectAntigravity(); err != nil {
			return err
		}
	}
	return nil
}

func (collector *importCollector) addSource(
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	source string,
	loadIdentity string,
) error {
	key, err := newCarrierKey(
		carrier,
		selectedTarget,
		scope,
		sourceKind,
		source,
	)
	if err != nil {
		return err
	}
	identity, err := hostrelation.NewHostLoadIdentity(loadIdentity)
	if err != nil {
		return err
	}
	return collector.addCandidate(key, identity)
}

func (collector *importCollector) addCandidate(
	key desiredextension.CarrierKey,
	loadIdentity hostrelation.HostLoadIdentity,
) error {
	if previous, duplicate := collector.candidates[key]; duplicate {
		if previous.loadIdentity != loadIdentity {
			return fmt.Errorf(
				"extension relation %s maps to load identities %q and %q",
				canonicalRelationText(key),
				previous.loadIdentity,
				loadIdentity,
			)
		}
		return nil
	}
	collector.candidates[key] = candidateFact{
		key:          key,
		loadIdentity: loadIdentity,
	}
	return nil
}

func (collector *importCollector) loadExistingIdentities() error {
	for _, value := range collector.input.Existing {
		key := value.CarrierKey()
		capability, admitted := profile.Profile(key.Target()).ExtensionOrder(
			key.Carrier(),
			key.Scope(),
		)
		if !admitted || !collector.classObserved(capability.ClassID()) {
			continue
		}
		var identity string
		var err error
		switch key.Carrier() {
		case desiredextension.CarrierOpenCodePlugin:
			sourcePath := collector.openCodeSourcePaths[key.Scope()]
			if sourcePath == "" {
				return fmt.Errorf(
					"OpenCode %s source identity lacks selected config path",
					key.Scope(),
				)
			}
			identity, err = opencodeconfig.HostLoadIdentity(
				key.Source().Ref(),
				sourcePath,
			)
		case desiredextension.CarrierPiPackage:
			identity, err = observepi.HostLoadIdentityForInput(
				key.Source().Ref(),
				collector.input.ManifestRoot,
				key.Scope(),
			)
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf(
				"derive existing extension %q load identity: %w",
				value.ID().Name(),
				err,
			)
		}
		loadIdentity, err := hostrelation.NewHostLoadIdentity(identity)
		if err != nil {
			return err
		}
		collector.existingLoadIdentities[key] = loadIdentity
	}
	return nil
}

func (collector *importCollector) classObserved(
	classID hostrelation.OrderClassID,
) bool {
	for _, sequence := range collector.sequences {
		if sequence.classID == classID {
			return true
		}
	}
	return false
}

func (collector *importCollector) includesTarget(selected target.Target) bool {
	for _, candidate := range collector.input.Targets {
		if candidate == selected {
			return true
		}
	}
	return false
}

func (collector *importCollector) includesScope(selected target.Scope) bool {
	for _, candidate := range collector.input.Scopes {
		if candidate == selected {
			return true
		}
	}
	return false
}

func newCarrierKey(
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	kind desiredextension.SourceKind,
	source string,
) (desiredextension.CarrierKey, error) {
	ref, err := desiredextension.NewAuthoredSourceRef(kind, source)
	if err != nil {
		return desiredextension.CarrierKey{}, err
	}
	return desiredextension.NewCarrierKey(
		carrier,
		selectedTarget,
		scope,
		ref,
	)
}

// newCanonicalCarrierKey reconstructs a carrier key from a derived or
// canonicalized source that already passed authored admission in its raw
// spelling, so authored policy is not applied twice to transformed text.
func newCanonicalCarrierKey(
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	kind desiredextension.SourceKind,
	source string,
) (desiredextension.CarrierKey, error) {
	ref, err := desiredextension.NewSourceRef(kind, source)
	if err != nil {
		return desiredextension.CarrierKey{}, err
	}
	return desiredextension.NewCarrierKey(
		carrier,
		selectedTarget,
		scope,
		ref,
	)
}
