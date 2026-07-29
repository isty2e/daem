package antigravityplugin

import (
	"crypto/sha256"
	"fmt"
	"sort"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// Inventory is one immutable observation of the Antigravity import manifest.
// Installed bundle state is read only when correlating a selected plugin.
type Inventory struct {
	paths       HostPaths
	manifest    string
	exists      bool
	fingerprint [sha256.Size]byte
	imports     []string
}

// ReadInventory reads the host import manifest. A missing manifest is fresh
// empty evidence; malformed, unstable, symlinked, or unreadable files fail.
func ReadInventory(paths HostPaths) (Inventory, error) {
	manifest := paths.ImportManifestPath()
	if manifest == "" {
		return Inventory{}, fmt.Errorf("Antigravity CLI host paths are unresolved")
	}
	content, exists, err := readStableRegularFile(manifest)
	if err != nil {
		return Inventory{}, fmt.Errorf("read Antigravity CLI import manifest %q: %w", manifest, err)
	}
	imports := []string(nil)
	if exists {
		imports, err = decodeImports(content)
		if err != nil {
			return Inventory{}, fmt.Errorf("decode Antigravity CLI import manifest %q: %w", manifest, err)
		}
	}
	return Inventory{
		paths:       paths,
		manifest:    manifest,
		exists:      exists,
		fingerprint: sha256.Sum256(content),
		imports:     append([]string(nil), imports...),
	}, nil
}

// Equal reports whether two reads observed the same import manifest bytes.
func (inventory Inventory) Equal(other Inventory) bool {
	return inventory.manifest == other.manifest &&
		inventory.exists == other.exists &&
		inventory.fingerprint == other.fingerprint
}

// CompletePluginNames returns stable plugin-name diagnostics only when each
// selected import row has one complete installed bundle. Names carry no source
// provenance and therefore cannot become extension declarations.
func (inventory Inventory) CompletePluginNames() ([]string, error) {
	names := append([]string(nil), inventory.imports...)
	sort.Strings(names)
	for index, name := range names {
		if index > 0 && name == names[index-1] {
			return nil, fmt.Errorf(
				"Antigravity CLI import manifest contains duplicate plugin %q",
				name,
			)
		}
		present, err := observePluginBundle(inventory.paths, name, true)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, fmt.Errorf(
				"Antigravity CLI plugin %q has a partial import/bundle relation",
				name,
			)
		}
	}
	return names, nil
}

// CorrelateDesired classifies one desired plugin. The import row and installed
// bundle must be consistently present or absent before installation state can
// be trusted.
func (inventory Inventory) CorrelateDesired(
	key relationobserve.CorrelationKey,
	carrier desiredextension.CarrierKey,
) (relationobserve.CorrelationResult, error) {
	return inventory.correlate(key, carrier, true)
}

// CorrelateRemoval classifies one claim-only plugin during desired-absence
// reconciliation. Either residual import or bundle state keeps the relation
// live so removal can retry; retirement still requires independent artifact
// absence evidence.
func (inventory Inventory) CorrelateRemoval(
	key relationobserve.CorrelationKey,
	carrier desiredextension.CarrierKey,
) (relationobserve.CorrelationResult, error) {
	return inventory.correlate(key, carrier, false)
}

func (inventory Inventory) correlate(
	key relationobserve.CorrelationKey,
	carrier desiredextension.CarrierKey,
	requireCompletePair bool,
) (relationobserve.CorrelationResult, error) {
	if err := key.Validate(); err != nil {
		return relationobserve.CorrelationResult{}, fmt.Errorf(
			"Antigravity CLI relation correlation key: %w",
			err,
		)
	}
	source, err := validateObservedCarrier(carrier)
	if err != nil {
		return relationobserve.CorrelationResult{}, err
	}
	plugin := source.RelationIdentity()
	if string(key.ExpectedRelation().SubjectKey()) != plugin {
		return relationobserve.CorrelationResult{}, fmt.Errorf(
			"Antigravity CLI expected relation key %q does not match host plugin name %q",
			key.ExpectedRelation().SubjectKey(),
			plugin,
		)
	}

	importCount := 0
	for _, name := range inventory.imports {
		if name == plugin {
			importCount++
		}
	}
	if importCount > 1 {
		return relationobserve.CorrelationResult{}, fmt.Errorf(
			"Antigravity CLI import manifest contains %d rows for plugin %q",
			importCount,
			plugin,
		)
	}
	bundlePresent, err := observePluginBundle(
		inventory.paths,
		plugin,
		requireCompletePair,
	)
	if err != nil {
		return relationobserve.CorrelationResult{}, err
	}
	if requireCompletePair && (importCount == 1) != bundlePresent {
		return relationobserve.CorrelationResult{}, fmt.Errorf(
			"Antigravity CLI plugin %q has a partial import/bundle relation",
			plugin,
		)
	}

	rows := []relationobserve.Row(nil)
	if importCount == 1 || bundlePresent {
		row, err := relationobserve.NewRow(relationobserve.RowSpec{
			SubjectKey:            plugin,
			HasManagedInstanceKey: false,
		})
		if err != nil {
			return relationobserve.CorrelationResult{}, err
		}
		rows = append(rows, row)
	}
	relationInventory, err := relationobserve.NewInventory(relationobserve.InventorySpec{
		Availability: relationobserve.InventorySupported,
		Freshness:    relationobserve.EvidenceFresh,
		Rows:         rows,
	})
	if err != nil {
		return relationobserve.CorrelationResult{}, err
	}
	return relationobserve.Correlate(key.ExpectedRelation(), relationInventory), nil
}

func validateObservedCarrier(
	carrier desiredextension.CarrierKey,
) (extensiontopology.CarrierSource, error) {
	if err := carrier.Validate(); err != nil {
		return extensiontopology.CarrierSource{}, fmt.Errorf(
			"Antigravity CLI observation carrier: %w",
			err,
		)
	}
	if carrier.Carrier() != desiredextension.CarrierAntigravityCLIPlugin ||
		carrier.Target() != target.TargetAntigravityCLI ||
		carrier.Scope() != target.ScopeGlobal {
		return extensiontopology.CarrierSource{}, fmt.Errorf(
			"Antigravity CLI observer requires its explicit-global plugin carrier",
		)
	}
	source, err := extensiontopology.InterpretCarrierSource(carrier)
	if err != nil {
		return extensiontopology.CarrierSource{}, err
	}
	if source.Class() != extensiontopology.CarrierSourceMarketplace {
		return extensiontopology.CarrierSource{}, fmt.Errorf(
			"Antigravity CLI plugin source %q has no passive removal correlation",
			carrier.Source().Ref(),
		)
	}
	return source, nil
}
