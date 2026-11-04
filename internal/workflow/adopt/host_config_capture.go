package adopt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	captureconfig "github.com/isty2e/daem/internal/adopt/hostconfig"
	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

var (
	ErrHostConfigCaptureApproval        = errors.New("capture requires approval of the named file")
	ErrHostConfigCaptureArtifactOverlap = errors.New("capture artifacts overlap another contract")
)

type HostConfigUpdateInput struct {
	Paths      daempaths.Paths
	Name       string
	SourcePath string
	Keys       []output.KeyPath
}

type HostConfigInitialInput struct {
	Paths  daempaths.Paths
	Config hostconfig.Config
	Keys   []output.KeyPath
	Merge  bool
}

// HostConfigCapturePlan retains actual data and roots for one named-file edit.
// Its inspection data is not display-safe; presentation must project and redact.
type HostConfigCapturePlan struct {
	input          HostConfigUpdateInput
	initial        *HostConfigInitialInput
	barrier        recoverygate.EffectAuthority
	manifest       declarationartifact.MutationTarget
	originalSource declarationartifact.MutationTarget
	source         declarationartifact.MutationTarget
	observation    *nativeconfig.DocumentObservation
	config         hostconfig.Config
	native         aggregate.Document
	manifestBefore []byte
	manifestAfter  []byte
	sourceBefore   []byte
	desiredBefore  []byte
	sourceAfter    []byte
	selected       []output.KeyPath
	references     []HostConfigSourceReference
	hostOutputs    []output.Destination
	retire         bool
	narrowing      bool
	closed         bool
}

func (plan *HostConfigCapturePlan) SourcePath() string {
	if plan.input.SourcePath != "" {
		return plan.input.SourcePath
	}
	path, _ := localCaptureSourcePath(plan.input.Paths.ManifestRoot, plan.config.Source())
	return path
}
func (plan *HostConfigCapturePlan) SourceReferent() string    { return plan.source.Path() }
func (plan *HostConfigCapturePlan) ManifestPath() string      { return plan.input.Paths.ManifestPath }
func (plan *HostConfigCapturePlan) Initial() bool             { return plan.initial != nil }
func (plan *HostConfigCapturePlan) Config() hostconfig.Config { return plan.config }
func (plan *HostConfigCapturePlan) KnownReferences() []HostConfigSourceReference {
	return slices.Clone(plan.references)
}

func (plan *HostConfigCapturePlan) SelectedKeys() []output.KeyPath {
	return slices.Clone(plan.selected)
}
func (plan *HostConfigCapturePlan) RetiresDeclaration() bool       { return plan.retire }
func (plan *HostConfigCapturePlan) ReplacesDesiredValues() bool    { return !plan.retire }
func (plan *HostConfigCapturePlan) NarrowingHandoff() bool         { return plan.narrowing }
func (plan *HostConfigCapturePlan) OtherReadersUnenumerated() bool { return true }
func (plan *HostConfigCapturePlan) SourceBefore() []byte           { return bytes.Clone(plan.sourceBefore) }

func (plan *HostConfigCapturePlan) DesiredBefore() []byte { return bytes.Clone(plan.desiredBefore) }

func (plan *HostConfigCapturePlan) SourceAfter() []byte { return bytes.Clone(plan.sourceAfter) }

func (plan *HostConfigCapturePlan) ManifestBefore() []byte { return bytes.Clone(plan.manifestBefore) }

func (plan *HostConfigCapturePlan) ManifestAfter() []byte { return bytes.Clone(plan.manifestAfter) }

func (plan *HostConfigCapturePlan) ApprovalPath() string {
	if plan.retire {
		return plan.manifest.Path()
	}
	return plan.SourcePath()
}

func (plan *HostConfigCapturePlan) Close() error {
	if plan == nil || plan.closed {
		return nil
	}
	plan.closed = true
	if plan.observation != nil {
		return plan.observation.Close()
	}
	return nil
}

// BuildHostConfigUpdatePlan previews an existing explicit local declaration,
// without remote materialization, leases, recovery or writes. Callers must Close
// the retained plan. An export preserves unselected desired source values.
func BuildHostConfigUpdatePlan(ctx context.Context, input HostConfigUpdateInput) (plan *HostConfigCapturePlan, resultErr error) {
	return buildHostConfigCapturePlan(ctx, input, nil)
}

func BuildHostConfigInitialPlan(ctx context.Context, input HostConfigInitialInput) (*HostConfigCapturePlan, error) {
	if err := input.Config.Validate(); err != nil {
		return nil, err
	}
	input.Keys = slices.Clone(input.Keys)
	path, err := localCaptureSourcePath(input.Paths.ManifestRoot, input.Config.Source())
	if err != nil {
		return nil, err
	}
	return buildHostConfigCapturePlan(ctx, HostConfigUpdateInput{Paths: input.Paths, Name: input.Config.ID().Name(), SourcePath: path, Keys: input.Keys}, &input)
}

func buildHostConfigCapturePlan(ctx context.Context, input HostConfigUpdateInput, initial *HostConfigInitialInput) (plan *HostConfigCapturePlan, resultErr error) {
	if ctx == nil || input.Paths.ManifestPath == "" || input.Name == "" {
		return nil, fmt.Errorf("capture requires context, selected paths and declaration name")
	}
	input.Keys = slices.Clone(input.Keys)
	if input.SourcePath != "" && !filepath.IsAbs(input.SourcePath) {
		input.SourcePath = filepath.Join(input.Paths.ManifestRoot, input.SourcePath)
	}
	if input.SourcePath != "" {
		input.SourcePath = filepath.Clean(input.SourcePath)
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, input.Paths)
	if err != nil {
		return nil, err
	}
	if err := barrier.Validate(ctx); err != nil {
		return nil, err
	}
	plan = &HostConfigCapturePlan{input: input, initial: initial, barrier: barrier}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, plan.Close())
			plan = nil
		}
	}()
	plan.manifest, err = declarationartifact.CaptureMutationTarget(ctx, input.Paths.ManifestPath)
	if err != nil {
		return plan, err
	}
	manifestSnapshot, manifestExists, err := filesnapshot.ReadRegularFileSnapshotContext(ctx, plan.manifest.Path(), declarationartifact.MaximumBytes)
	if err != nil {
		return plan, err
	}
	plan.manifestBefore = manifestSnapshot.Content()
	manifestContent := plan.manifestBefore
	if initial != nil {
		if manifestExists && !initial.Merge {
			return plan, fmt.Errorf("capture into an existing manifest requires --merge")
		}
		if !manifestExists {
			manifestContent, err = declarationcodec.RenderImportManifest([]string{string(initial.Config.Target())}, declarationcodec.ImportManifestBody{})
			if err != nil {
				return plan, err
			}
		}
	} else if !manifestExists {
		return plan, fmt.Errorf("capture update requires an existing manifest")
	}
	environment, err := declarationmanifest.Decode(manifestContent)
	if err != nil {
		return plan, err
	}
	for _, config := range environment.HostConfigs() {
		placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
		if err != nil {
			return plan, err
		}
		plan.hostOutputs = append(plan.hostOutputs, placement.Destination())
	}
	found := false
	for _, config := range environment.HostConfigs() {
		if config.ID().Name() == input.Name {
			plan.config, found = config, true
			break
		}
	}
	if initial != nil {
		if found {
			return plan, fmt.Errorf("host_config %q already exists; use --update-source", input.Name)
		}
		plan.config = initial.Config
	} else if !found {
		return plan, fmt.Errorf("host_config %q is not declared", input.Name)
	}
	placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: plan.config.Target(), Scope: plan.config.Scope(), Path: plan.config.Path(), Ownership: plan.config.Ownership(), Syntax: plan.config.Syntax()})
	if err != nil {
		return plan, err
	}
	plan.hostOutputs = append(plan.hostOutputs, placement.Destination())
	oldSourcePath, err := localCaptureSourcePath(input.Paths.ManifestRoot, plan.config.Source())
	if err != nil {
		return plan, err
	}
	plan.originalSource, err = declarationartifact.CaptureMutationTarget(ctx, oldSourcePath)
	if err != nil {
		return plan, err
	}
	original, exists, err := filesnapshot.ReadRegularFileSnapshotContext(ctx, plan.originalSource.Path(), nativeconfig.DocumentLimits().MaximumBytes)
	if err != nil {
		return plan, fmt.Errorf("read declared local capture source: %w", err)
	}
	if initial != nil && exists {
		return plan, fmt.Errorf("capture source destination already exists")
	}
	if !exists && initial == nil && plan.config.Ownership() == hostconfig.OwnershipKeys {
		return plan, fmt.Errorf("declared capture source is missing")
	}
	plan.source = plan.originalSource
	plan.desiredBefore = original.Content()
	plan.sourceBefore = plan.desiredBefore
	if input.SourcePath != "" {
		plan.source, err = declarationartifact.CaptureMutationTarget(ctx, input.SourcePath)
		if err != nil {
			return plan, err
		}
		if exists, err := pathExists(plan.source.Path()); err != nil {
			return plan, err
		} else if exists {
			return plan, fmt.Errorf("capture export destination already exists")
		}
		plan.sourceBefore = nil
	}
	epoch, err := nativeconfig.LoadPersistenceEpoch(ctx, input.Paths)
	if err != nil {
		return plan, err
	}
	address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		return plan, err
	}
	plan.observation, err = nativeconfig.ObserveSelectedDocuments(ctx, input.Paths, &epoch, []aggregate.DocumentAddress{address})
	if err != nil {
		return plan, err
	}
	facts, present := plan.observation.NativeInput()
	if !present {
		return plan, fmt.Errorf("capture observation is unavailable")
	}
	plan.native, _, present = facts.Documents.Evidence().Document(address)
	if !present {
		return plan, fmt.Errorf("capture document was not observed")
	}
	var footprints []output.Footprint
	var syntax configdocument.Syntax
	if placement.Grammar() != configuration.Opaque {
		syntax, err = configdocument.ParseSyntax(string(placement.Grammar()))
		if err != nil {
			return plan, err
		}
	}
	if plan.config.Ownership() == hostconfig.OwnershipDocument {
		if len(input.Keys) != 0 {
			return plan, fmt.Errorf("whole-document capture cannot select keys")
		}
		plan.retire = !plan.native.Exists()
		if plan.retire && initial != nil {
			return plan, fmt.Errorf("initial whole-document capture requires an existing host document")
		}
		if plan.retire && input.SourcePath != "" {
			return plan, fmt.Errorf("missing whole document cannot be exported; preview retirement without a new source")
		}
		plan.sourceAfter = plan.native.Content()
		footprints = []output.Footprint{output.WholePathFootprint()}
		if !plan.retire && placement.Grammar() != configuration.Opaque {
			if _, err := configdocument.Parse(ctx, plan.sourceAfter, syntax, nativeconfig.DocumentLimits()); err != nil {
				return plan, err
			}
		}
	} else {
		sourceDocument, err := configdocument.Absent(syntax, nativeconfig.DocumentLimits())
		if initial == nil {
			sourceDocument, err = configdocument.Parse(ctx, plan.desiredBefore, syntax, nativeconfig.DocumentLimits())
		}
		if err != nil {
			return plan, err
		}
		hostDocument, err := configdocument.Absent(syntax, nativeconfig.DocumentLimits())
		if plan.native.Exists() {
			hostDocument, err = configdocument.Parse(ctx, plan.native.Content(), syntax, nativeconfig.DocumentLimits())
		}
		if err != nil {
			return plan, err
		}
		var result captureconfig.Result
		if initial != nil {
			if len(input.Keys) == 0 {
				return plan, fmt.Errorf("initial capture requires --key or explicit document ownership")
			}
			result, err = captureconfig.Initial(ctx, hostDocument, sourceDocument, input.Keys, nativeconfig.DocumentLimits().MaximumBytes)
		} else {
			result, err = captureconfig.Update(ctx, sourceDocument, hostDocument, input.Keys, nativeconfig.DocumentLimits().MaximumBytes)
		}
		if err != nil {
			return plan, err
		}
		plan.sourceAfter, plan.selected = result.After().Bytes(), result.Selected()
		for _, key := range plan.selected {
			footprint, err := output.NewKeyFootprint(key)
			if err != nil {
				return plan, err
			}
			footprints = append(footprints, footprint)
		}
	}
	if !plan.retire && exists && input.SourcePath == "" && original.Mode().Perm()&0o222 == 0 {
		return plan, fmt.Errorf("capture source is not writable")
	}
	if !plan.retire {
		plan.references, err = captureSourceReferences(ctx, input.Paths, environment, plan.source.Path(), facts.Budget)
		if err != nil {
			return plan, err
		}
	}
	plan.narrowing, err = nativeconfig.QualifyCaptureOwnership(ctx, facts, plan.config, placement, footprints)
	if err != nil {
		return plan, err
	}
	plan.manifestAfter = bytes.Clone(plan.manifestBefore)
	if initial != nil {
		block, err := declarationcodec.RenderHostConfigBlock(plan.config)
		if err != nil {
			return plan, err
		}
		plan.manifestAfter = declaration.AppendDocumentBlock(manifestContent, block)
		if _, err := declarationmanifest.Decode(plan.manifestAfter); err != nil {
			return plan, err
		}
	} else if plan.retire || input.SourcePath != "" {
		plan.manifestAfter, err = captureManifestChange(plan.manifestBefore, plan.config, input.SourcePath, plan.retire)
		if err != nil {
			return plan, err
		}
	}
	if err := plan.qualifyCapturePaths(ctx); err != nil {
		return plan, err
	}
	return plan, nil
}

func captureManifestChange(before []byte, config hostconfig.Config, newSource string, retire bool) ([]byte, error) {
	blocks, err := declarationcodec.ScanHostConfigBlocks(before)
	if err != nil {
		return nil, err
	}
	for _, block := range blocks {
		if block.HostConfig.ID != config.ID().Name() {
			continue
		}
		change := declaration.NewDocumentRemoval(declaration.DocumentRange{Start: block.Start, End: block.End})
		if !retire {
			updated, err := captureSourceConfig(config, newSource)
			if err != nil {
				return nil, err
			}
			content, err := declarationcodec.RenderHostConfigBlock(updated)
			if err != nil {
				return nil, err
			}
			change = declaration.NewDocumentReplacement(declaration.DocumentRange{Start: block.Start, End: block.End}, []byte(content))
		}
		after, err := declaration.NewDocumentChangeSet(change).Apply(before)
		if err != nil {
			return nil, err
		}
		if _, err := declarationmanifest.Decode(after); err != nil {
			return nil, err
		}
		return after, nil
	}
	return nil, fmt.Errorf("capture declaration block is missing")
}

func (plan *HostConfigCapturePlan) qualifyCapturePaths(ctx context.Context) error {
	if err := plan.manifest.Validate(ctx); err != nil {
		return err
	}
	if err := plan.originalSource.Validate(ctx); err != nil {
		return err
	}
	if err := plan.source.Validate(ctx); err != nil {
		return err
	}
	paths := plan.input.Paths
	facts, _ := plan.observation.NativeInput()
	sourceKey, err := captureReferentKey(plan.source.Path(), facts.Budget)
	if err != nil {
		return err
	}
	manifestKey, err := captureReferentKey(plan.manifest.Path(), facts.Budget)
	if err != nil {
		return err
	}
	originalKey, err := captureReferentKey(plan.originalSource.Path(), facts.Budget)
	if err != nil {
		return err
	}
	if manifestKey == originalKey || manifestKey == sourceKey {
		return fmt.Errorf("%w: declaration and source must be separate artifacts", ErrHostConfigCaptureArtifactOverlap)
	}
	writes := []string{manifestKey}
	if !plan.retire {
		writes = append(writes, sourceKey)
	}
	marker, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return err
	}
	for _, forbidden := range []string{paths.LockfilePath, paths.StatefilePath, paths.OwnershipRegistryPath, paths.CarrierClaimRegistryPath, paths.SourceCacheDir, paths.RecoveryDir, marker} {
		canonical, err := captureReferentKey(forbidden, facts.Budget)
		if err != nil {
			return err
		}
		for _, write := range writes {
			if capturePathContains(canonical, write) {
				return fmt.Errorf("%w: write overlaps control or cache artifacts", ErrHostConfigCaptureArtifactOverlap)
			}
		}
	}
	for _, destination := range plan.hostOutputs {
		path, err := physicalhostpath.NewResolver(paths).Resolve(destination, facts.Budget)
		if err != nil {
			return err
		}
		hostKey, err := captureReferentKey(path, facts.Budget)
		if err != nil {
			return err
		}
		if slices.Contains(writes, hostKey) {
			return fmt.Errorf("%w: write aliases a declared host output", ErrHostConfigCaptureArtifactOverlap)
		}
	}
	for _, write := range writes {
		for _, claim := range facts.Registry.Claims() {
			if capturePathContains(claim.Address().Path(), write) || capturePathContains(write, claim.Address().Path()) {
				return fmt.Errorf("%w: write overlaps a managed host output", ErrHostConfigCaptureArtifactOverlap)
			}
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(write, physicalwork.MaximumPhysicalPathDepth, facts.Budget)
		if err != nil {
			return err
		}
		if physical, exact := observed.Exact(); exact {
			if err := facts.Registry.RequireWritable(physical); err != nil {
				return err
			}
		} else {
			missing, _ := observed.Provisional()
			scope, err := ownership.NewDocumentScope(nil, []pathauthority.Provisional{missing})
			if err != nil {
				return err
			}
			if err := scope.RequireProtectionClear(facts.Registry.PendingProtections()); err != nil {
				return err
			}
		}
	}
	return plan.barrier.Validate(ctx)
}
