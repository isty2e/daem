package adopt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	declarationtransaction "github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/recoverygate"
)

type HostConfigCaptureRecoveryInput struct {
	Paths      daempaths.Paths
	Name       string
	SourcePath string
}

type HostConfigCaptureRecovery struct {
	input      HostConfigCaptureRecoveryInput
	barrier    recoverygate.EffectAuthority
	manifest   declarationartifact.MutationTarget
	source     declarationartifact.MutationTarget
	evidence   declarationtransaction.SourceRecovery
	retire     bool
	references []HostConfigSourceReference
}

func (plan *HostConfigCaptureRecovery) SourcePath() string       { return plan.input.SourcePath }
func (plan *HostConfigCaptureRecovery) SourceReferent() string   { return plan.source.Path() }
func (plan *HostConfigCaptureRecovery) ManifestPath() string     { return plan.input.Paths.ManifestPath }
func (plan *HostConfigCaptureRecovery) RetiresDeclaration() bool { return plan.retire }

func (plan *HostConfigCaptureRecovery) Disposition() string {
	return string(plan.evidence.Disposition())
}

func (plan *HostConfigCaptureRecovery) KnownReferences() []HostConfigSourceReference {
	return slices.Clone(plan.references)
}

func (plan *HostConfigCaptureRecovery) Fingerprint() string {
	fields := [][]byte{[]byte("recovery"), []byte(plan.input.Name), []byte(plan.manifest.Path()), []byte(plan.input.SourcePath), []byte(plan.source.Path()), []byte(plan.evidence.Reference().MarkerRevision()), []byte(plan.Disposition())}
	for _, reference := range plan.references {
		fields = append(fields, []byte(reference.Resource), []byte(reference.Path))
	}
	return captureFingerprint(fields...)
}

// InspectHostConfigCaptureRecovery returns nil only for a clear barrier. Source
// paths come from the invocation or current declaration, never marker contents.
func InspectHostConfigCaptureRecovery(ctx context.Context, input HostConfigCaptureRecoveryInput) (*HostConfigCaptureRecovery, error) {
	if ctx == nil || input.Paths.ManifestPath == "" || input.Name == "" {
		return nil, fmt.Errorf("capture recovery requires selected paths and declaration name")
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, input.Paths)
	if err != nil {
		return nil, err
	}
	if err := barrier.ValidateFileSetRecovery(ctx); err != nil {
		return nil, err
	}
	if err := barrier.Validate(ctx); err == nil {
		return nil, nil
	}
	// The generic clear-barrier reader deliberately refuses generation 4. Only
	// the exact-target source adapter below may admit its reference evidence.
	plan := &HostConfigCaptureRecovery{input: input, barrier: barrier}
	plan.manifest, err = declarationartifact.CaptureMutationTarget(ctx, input.Paths.ManifestPath)
	if err != nil {
		return nil, err
	}
	if input.SourcePath == "" {
		current, exists, err := captureCurrentDeclaration(ctx, input.Paths.ManifestPath, input.Name)
		if err != nil {
			return nil, err
		}
		if exists {
			plan.input.SourcePath, err = localCaptureSourcePath(input.Paths.ManifestRoot, current.Source())
			if err != nil {
				return nil, err
			}
		}
	} else if !filepath.IsAbs(input.SourcePath) {
		plan.input.SourcePath = filepath.Join(input.Paths.ManifestRoot, input.SourcePath)
	}
	if plan.input.SourcePath != "" {
		plan.input.SourcePath = filepath.Clean(plan.input.SourcePath)
		plan.source, err = declarationartifact.CaptureMutationTarget(ctx, plan.input.SourcePath)
		if err != nil {
			return nil, err
		}
		plan.evidence, err = declarationtransaction.InspectSourceTransactionRecovery(ctx, input.Paths.StateDir, plan.manifest, plan.source)
	}
	if plan.input.SourcePath == "" || err != nil {
		// A retirement has exactly the declaration target, and never restores or
		// overwrites the old source. Its shape is validated below before approval.
		plan.evidence, err = declarationtransaction.InspectSourceRetirementRecovery(ctx, input.Paths.StateDir, plan.manifest)
		plan.retire = true
	}
	if err != nil {
		return nil, err
	}
	if err := plan.validateDeclarationTransition(ctx); err != nil {
		return nil, err
	}
	plan.references, err = plan.qualifyRecoveryPaths(ctx)
	if err != nil {
		return nil, err
	}
	if err := barrier.ValidateFileSetRecovery(ctx); err != nil {
		return nil, err
	}
	return plan, nil
}

func captureCurrentDeclaration(ctx context.Context, path, name string) (hostconfig.Config, bool, error) {
	content, err := declarationartifact.Read(ctx, path)
	if err != nil {
		return hostconfig.Config{}, false, err
	}
	return captureDeclaration(content, name)
}

func captureDeclaration(content []byte, name string) (hostconfig.Config, bool, error) {
	if len(content) == 0 {
		return hostconfig.Config{}, false, nil
	}
	environment, err := declarationmanifest.Decode(content)
	if err != nil {
		return hostconfig.Config{}, false, err
	}
	for _, config := range environment.HostConfigs() {
		if config.ID().Name() == name {
			return config, true, nil
		}
	}
	return hostconfig.Config{}, false, nil
}

func (plan *HostConfigCaptureRecovery) validateDeclarationTransition(ctx context.Context) error {
	reference := plan.evidence.Reference()
	before, beforeExists := reference.BeforeImage()
	after := reference.AfterImage()
	old, oldExists, err := captureDeclaration(before, plan.input.Name)
	if err != nil {
		return err
	}
	updated, updatedExists, err := captureDeclaration(after, plan.input.Name)
	if err != nil {
		return err
	}
	if plan.retire {
		if !oldExists || updatedExists || old.Ownership() != hostconfig.OwnershipDocument {
			return fmt.Errorf("pending transaction is not the selected whole-document retirement")
		}
		expected, err := captureManifestChange(before, old, "", true)
		if err != nil {
			return err
		}
		if !bytes.Equal(expected, after) {
			return fmt.Errorf("pending retirement changes other declarations")
		}
		oldSource, err := localCaptureSourcePath(plan.input.Paths.ManifestRoot, old.Source())
		if err != nil {
			return err
		}
		oldTarget, err := declarationartifact.CaptureMutationTarget(ctx, oldSource)
		if err != nil {
			return err
		}
		if oldTarget.Path() == plan.manifest.Path() {
			return ErrHostConfigCaptureArtifactOverlap
		}
		return nil
	}
	if !updatedExists {
		return fmt.Errorf("pending capture does not declare the selected id")
	}
	selected, err := localCaptureSourcePath(plan.input.Paths.ManifestRoot, updated.Source())
	if err != nil {
		return err
	}
	target, err := declarationartifact.CaptureMutationTarget(ctx, selected)
	if err != nil {
		return err
	}
	if target.Path() != plan.source.Path() || plan.manifest.Path() == plan.source.Path() {
		return fmt.Errorf("pending capture source differs from independently selected target")
	}
	if oldExists {
		if bytes.Equal(before, after) {
			return nil
		}
		expected, err := captureManifestChange(before, old, selected, false)
		if err != nil {
			return err
		}
		if !bytes.Equal(expected, after) {
			return fmt.Errorf("pending capture changes more than the selected source reference")
		}
		return nil
	}
	seed := before
	if !beforeExists {
		seed, err = declarationcodec.RenderImportManifest([]string{string(updated.Target())}, declarationcodec.ImportManifestBody{})
		if err != nil {
			return err
		}
	}
	block, err := declarationcodec.RenderHostConfigBlock(updated)
	if err != nil {
		return err
	}
	if !bytes.Equal(declaration.AppendDocumentBlock(seed, block), after) {
		return fmt.Errorf("pending capture changes more than the selected initial declaration")
	}
	return nil
}

func (plan *HostConfigCaptureRecovery) qualifyRecoveryPaths(ctx context.Context) ([]HostConfigSourceReference, error) {
	budget := recovery.NewPhysicalCaptureBudget(nil)
	var writes []string
	writablePaths := []string{plan.manifest.Path()}
	if !plan.retire {
		writablePaths = append(writablePaths, plan.source.Path())
	}
	for _, path := range writablePaths {
		key, err := captureReferentKey(path, budget)
		if err != nil {
			return nil, err
		}
		writes = append(writes, key)
	}
	paths := plan.input.Paths
	marker, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{paths.LockfilePath, paths.StatefilePath, paths.OwnershipRegistryPath, paths.CarrierClaimRegistryPath, paths.SourceCacheDir, paths.RecoveryDir, marker} {
		key, err := captureReferentKey(path, budget)
		if err != nil {
			return nil, err
		}
		for _, write := range writes {
			if capturePathContains(key, write) {
				return nil, ErrHostConfigCaptureArtifactOverlap
			}
		}
	}
	before, _ := plan.evidence.Reference().BeforeImage()
	var references []HostConfigSourceReference
	for _, content := range [][]byte{before, plan.evidence.Reference().AfterImage()} {
		if len(content) == 0 {
			continue
		}
		environment, err := declarationmanifest.Decode(content)
		if err != nil {
			return nil, err
		}
		for _, config := range environment.HostConfigs() {
			placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
			if err != nil {
				return nil, err
			}
			host, err := managedhostpath.Resolver(paths).Resolve(placement.Destination())
			if err != nil {
				return nil, err
			}
			key, err := captureReferentKey(host, budget)
			if err != nil {
				return nil, err
			}
			if slices.Contains(writes, key) {
				return nil, ErrHostConfigCaptureArtifactOverlap
			}
		}
		if plan.retire {
			continue
		}
		known, err := captureSourceReferences(ctx, paths, environment, plan.source.Path(), budget)
		if err != nil {
			return nil, err
		}
		for _, reference := range known {
			if !slices.Contains(references, reference) {
				references = append(references, reference)
			}
		}
	}
	return references, nil
}

// ExecuteHostConfigCaptureRecovery is terminal: it never builds or executes a
// fresh capture, even when recovery rolls back rather than finalizes.
func ExecuteHostConfigCaptureRecovery(ctx context.Context, plan *HostConfigCaptureRecovery, approvedFingerprint string) (disposition string, resultErr error) {
	if ctx == nil || plan == nil || approvedFingerprint != plan.Fingerprint() {
		return "", ErrHostConfigCaptureApproval
	}
	marker, err := fileset.FileSetAuthorityPath(plan.input.Paths.StateDir)
	if err != nil {
		return "", err
	}
	paths := []string{plan.input.Paths.ManifestPath, plan.manifest.Path(), marker}
	if !plan.retire {
		paths = append(paths, plan.input.SourcePath, plan.source.Path())
	}
	for _, reference := range plan.references {
		paths = append(paths, reference.Path)
	}
	domains := plan.barrier.Domains()
	for _, path := range paths {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect})
			if err != nil {
				return "", err
			}
			domains = append(domains, domain)
		}
	}
	store, err := mutation.NewStore(plan.input.Paths.DataDir)
	if err != nil {
		return "", err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, leases.Release()) }()
	if err := plan.barrier.ValidateFileSetRecovery(ctx); err != nil {
		return "", err
	}
	if err := plan.manifest.Validate(ctx); err != nil {
		return "", err
	}
	if !plan.retire {
		if err := plan.source.Validate(ctx); err != nil {
			return "", err
		}
	}
	if same, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return "", err
	} else if !same {
		return "", mutation.StalePlanError{}
	}
	if err := plan.validateDeclarationTransition(ctx); err != nil {
		return "", err
	}
	known, err := plan.qualifyRecoveryPaths(ctx)
	if err != nil {
		return "", err
	}
	if !slices.Equal(known, plan.references) {
		return "", mutation.StalePlanError{}
	}
	result, err := plan.evidence.Recover(ctx)
	return string(result), err
}
